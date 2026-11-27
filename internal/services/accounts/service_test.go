package accounts

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeKube answers the kubectl call Live makes: nightshift-auth-key holds the
// key "secret-key". It returns the directory holding the call trace.
func fakeKube(t *testing.T) string {
	t.Helper()
	dir := stubKubectl(t, `echo "$*" >> "$KUBE_TRACE"
case "$*" in
  *"get secret nightshift-auth-key"*) printf 'c2VjcmV0LWtleQ==' ;;
esac
`)
	t.Setenv("KUBE_TRACE", dir+"/trace")
	return dir
}

func TestFetchMarksTheActiveAccountAndAttachesUsage(t *testing.T) {
	c, _ := newTestClient(t)
	var quotaRefreshed bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/usage":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","label":"a@example.com","requests":7,"ok":5,"rate_limited":2,"input_tokens":1000,"output_tokens":300,"cache_read_tokens":4000,"cache_write_tokens":200,"total_tokens":5500,"cache_hit_percent":76.9,"avg_latency_ms":420,"by_model":{"claude-sonnet-4-6":{"requests":7,"input":1000,"output":300,"cache_read":4000,"cache_write":200,"total":5500}},"recent":[0,3,4],"recent_tokens":[0,2000,3500]}]`)
		case "/pools":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","name":"claude-a@example.com","label":"a@example.com","priority":1,"active_now":true,"cooldown_until":"2026-01-01T12:05:00Z","cooldown_reason":"rate limited (429)","failovers":3}]`)
		case "/quota":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","name":"claude-a@example.com","label":"a@example.com","plan":"max","source":"fetch","next_reset_at":"2026-01-01T15:00:00Z","windows":[{"name":"5h","label":"5-hour window","group":"Gemini Models","used_percent":42,"resets_at":"2026-01-01T15:00:00Z"}]}]`)
		case "/history":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","label":"a@example.com","days":[{"date":"2025-12-31","requests":7,"total":5500}]}]`)
		case "/quota/refresh":
			quotaRefreshed = true
			reply(t, w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxy.Close()

	fakeKube(t)
	live := &Live{Auth: Static(c.base), Proxy: Static(proxy.URL)}
	snap, err := live.Fetch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snap.UsageErr != nil {
		t.Errorf("UsageErr = %v", snap.UsageErr)
	}
	byName := map[string]Account{}
	for _, a := range snap.Accounts {
		byName[a.Name] = a
	}
	if !byName["claude-a@example.com"].Active || byName["claude-a@example.com"].Priority != 1 || byName["claude-z"].Priority != 2 || byName["claude-z"].Active {
		t.Errorf("the chain's order must rank the accounts: %+v, %+v", byName["claude-a@example.com"], byName["claude-z"])
	}
	if !byName["claude-a@example.com"].ActiveNow || byName["claude-a@example.com"].Failovers != 3 || byName["claude-a@example.com"].CooldownReason != "rate limited (429)" {
		t.Errorf("pool status was not attached: %+v", byName["claude-a@example.com"])
	}
	u := byName["claude-a@example.com"].Usage
	if u == nil || u.Requests != 7 || u.RateLimited != 2 || len(u.Recent) != 3 || u.Input != 1000 || u.CacheRead != 4000 || u.Total != 5500 {
		t.Errorf("usage = %+v, want the proxy's request & token counts for a@example.com", u)
	}
	if q := byName["claude-a@example.com"].Quota; q == nil || q.Plan != "max" || len(q.Windows) != 1 || q.Windows[0].UsedPercent != 42 || q.Windows[0].Group != "Gemini Models" {
		t.Errorf("quota = %+v, want fetched quota windows", q)
	}
	if h := byName["claude-a@example.com"].History; len(h) != 1 || h[0] != (DayUsage{Date: "2025-12-31", Requests: 7, Total: 5500}) {
		t.Errorf("history = %+v, want the proxy's daily counts", h)
	}
	if snap.Analytics == nil || snap.Analytics.Total != 5500 || snap.Analytics.ByModel["claude-sonnet-4-6"].Requests != 7 {
		t.Errorf("analytics = %+v", snap.Analytics)
	}
	if err := live.RefreshQuotas(t.Context()); err != nil || !quotaRefreshed {
		t.Errorf("RefreshQuotas = %v (refreshed=%v)", err, quotaRefreshed)
	}
	if byName["codex-b"].Usage != nil {
		t.Error("an account the proxy never saw must have no usage")
	}
	if len(snap.Providers) != 2 || snap.Providers[0].Secret != "CLAUDE_ACCESS_TOKEN" {
		t.Errorf("providers = %+v, want nightshift-auth's list", snap.Providers)
	}
	if snap.Published["claude"].Label != "a@example.com" || snap.PublishedAt.IsZero() {
		t.Errorf("publish status = %+v at %v", snap.Published, snap.PublishedAt)
	}
}

func TestFetchKeepsAccountsWhenUsageIsUnavailable(t *testing.T) {
	c, _ := newTestClient(t)
	fakeKube(t)
	live := &Live{Auth: Static(c.base), Proxy: Static("http://127.0.0.1:1")}
	snap, err := live.Fetch(t.Context())
	if err != nil || len(snap.Accounts) != 3 {
		t.Fatalf("Fetch = %d accounts, %v; the accounts must survive a usage failure", len(snap.Accounts), err)
	}
	if snap.UsageErr == nil {
		t.Error("UsageErr must say why usage is missing")
	}
}

func TestSetActiveMovesTheAccountToTheHeadOfItsChain(t *testing.T) {
	c, api := newTestClient(t)
	fakeKube(t)
	live := &Live{Auth: Static(c.base), Proxy: Static("http://127.0.0.1:1")}
	if err := live.SetActive(t.Context(), "claude", "claude-z"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(api.chains["claude"], ","); got != "claude-z,claude-a@example.com" {
		t.Errorf("claude chain = %s", got)
	}
}

func TestAuthKeyIsDecoded(t *testing.T) {
	fakeKube(t)
	key, err := (Kube{}).AuthKey(t.Context())
	if err != nil || key != "secret-key" {
		t.Errorf("AuthKey = %q, %v", key, err)
	}
}
