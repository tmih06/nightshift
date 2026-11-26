package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeKube answers the few kubectl calls Live makes. cpa-keys holds the
// management key "secret-key"; the ConfigMap names claude-b as active and
// records a publish. It returns the directory holding the call trace.
func fakeKube(t *testing.T) string {
	t.Helper()
	cm, err := json.Marshal(map[string]any{"data": map[string]string{
		"active": `{"claude":"claude-b.json"}`,
		"status": `{"published_at":"2026-01-01T00:00:00Z","accounts":{"claude":{"name":"claude-b.json","label":"b@example.com","expires":"2026-01-01T06:00:00Z"}}}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	dir := stubKubectl(t, `echo "$*" >> "$KUBE_TRACE"
case "$*" in
  *"get secret cpa-keys"*) printf 'c2VjcmV0LWtleQ==' ;;
  *"get configmap nightshift-accounts --ignore-not-found -o json"*) printf '%s' "$KUBE_CM" ;;
  *"get configmap nightshift-accounts --ignore-not-found -o name"*) printf 'configmap/nightshift-accounts' ;;
esac
`)
	t.Setenv("KUBE_TRACE", dir+"/trace")
	t.Setenv("KUBE_CM", string(cm))
	return dir
}

func TestFetchMarksTheActiveAccountAndAttachesUsage(t *testing.T) {
	api := &fakeAPI{t: t}
	cpa := httptest.NewServer(api)
	defer cpa.Close()
	var quotaRefreshed bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/usage":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","label":"a@example.com","requests":7,"ok":5,"rate_limited":2,"input_tokens":1000,"output_tokens":300,"cache_read_tokens":4000,"cache_write_tokens":200,"total_tokens":5500,"cache_hit_percent":76.9,"avg_latency_ms":420,"by_model":{"claude-sonnet-4-6":{"requests":7,"input":1000,"output":300,"cache_read":4000,"cache_write":200,"total":5500}},"recent":[0,3,4],"recent_tokens":[0,2000,3500]}]`)
		case "/pools":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","name":"claude-a.json","label":"a@example.com","priority":1,"active_now":true,"cooldown_until":"2026-01-01T12:05:00Z","cooldown_reason":"rate limited (429)","failovers":3}]`)
		case "/quota":
			reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","name":"claude-a.json","label":"a@example.com","plan":"max","source":"fetch","next_reset_at":"2026-01-01T15:00:00Z","windows":[{"name":"5h","label":"5-hour window","group":"Gemini Models","used_percent":42,"resets_at":"2026-01-01T15:00:00Z"}]}]`)
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
	live := &Live{CPA: Static(cpa.URL), Proxy: Static(proxy.URL)}
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
	// claude-b is chosen but does not exist in the fake CLIProxyAPI, so the
	// first enabled claude account, claude-a, is the active one.
	if !byName["claude-a.json"].Active || byName["claude-a.json"].Priority != 1 {
		t.Errorf("claude-a must be active #1 when the chosen account is gone: %+v", byName["claude-a.json"])
	}
	if !byName["claude-a.json"].ActiveNow || byName["claude-a.json"].Failovers != 3 || byName["claude-a.json"].CooldownReason != "rate limited (429)" {
		t.Errorf("pool status was not attached: %+v", byName["claude-a.json"])
	}
	u := byName["claude-a.json"].Usage
	if u == nil || u.Requests != 7 || u.RateLimited != 2 || len(u.Recent) != 3 || u.Input != 1000 || u.CacheRead != 4000 || u.Total != 5500 {
		t.Errorf("usage = %+v, want the proxy's request & token counts for a@example.com", u)
	}
	if q := byName["claude-a.json"].Quota; q == nil || q.Plan != "max" || len(q.Windows) != 1 || q.Windows[0].UsedPercent != 42 || q.Windows[0].Group != "Gemini Models" {
		t.Errorf("quota = %+v, want fetched quota windows", q)
	}
	if h := byName["claude-a.json"].History; len(h) != 1 || h[0] != (DayUsage{Date: "2025-12-31", Requests: 7, Total: 5500}) {
		t.Errorf("history = %+v, want the proxy's daily counts", h)
	}
	if snap.Analytics == nil || snap.Analytics.Total != 5500 || snap.Analytics.ByModel["claude-sonnet-4-6"].Requests != 7 {
		t.Errorf("analytics = %+v", snap.Analytics)
	}
	if err := live.RefreshQuotas(t.Context()); err != nil || !quotaRefreshed {
		t.Errorf("RefreshQuotas = %v (refreshed=%v)", err, quotaRefreshed)
	}
	if byName["codex-b.json"].Usage != nil {
		t.Error("an account the proxy never saw must have no usage")
	}
	if snap.Published["claude"].Label != "b@example.com" || snap.PublishedAt.IsZero() {
		t.Errorf("publish status = %+v at %v", snap.Published, snap.PublishedAt)
	}
}

func TestFetchKeepsAccountsWhenUsageIsUnavailable(t *testing.T) {
	cpa := httptest.NewServer(&fakeAPI{t: t})
	defer cpa.Close()
	fakeKube(t)
	live := &Live{CPA: Static(cpa.URL), Proxy: Static("http://127.0.0.1:1")}
	snap, err := live.Fetch(t.Context())
	if err != nil || len(snap.Accounts) != 2 {
		t.Fatalf("Fetch = %d accounts, %v; the accounts must survive a usage failure", len(snap.Accounts), err)
	}
	if snap.UsageErr == nil {
		t.Error("UsageErr must say why usage is missing")
	}
}

func TestSetActiveKeepsTheOtherAgentsChoices(t *testing.T) {
	dir := fakeKube(t)
	if err := (Kube{}).SetActive(t.Context(), "codex", "codex-b.json"); err != nil {
		t.Fatal(err)
	}
	trace := readFile(t, dir, "trace")
	if !strings.Contains(trace, "patch configmap nightshift-accounts --type=merge") {
		t.Fatalf("the ConfigMap was not patched:\n%s", trace)
	}
	for _, want := range []string{`claude\":\"claude-b.json`, `codex\":\"codex-b.json`} {
		if !strings.Contains(trace, want) {
			t.Errorf("the patch lacks %q:\n%s", want, trace)
		}
	}
}

func TestSetChainSavesOrderAndPrimaryAccount(t *testing.T) {
	dir := fakeKube(t)
	if err := (Kube{}).SetChain(t.Context(), "claude", []string{"claude-z.json", "claude-a.json", "claude-z.json"}); err != nil {
		t.Fatal(err)
	}
	trace := readFile(t, dir, "trace")
	for _, want := range []string{
		`claude\":\"claude-z.json`,
		`claude\":[\"claude-z.json\",\"claude-a.json\"]`,
	} {
		if !strings.Contains(trace, want) {
			t.Errorf("the patch lacks %q:\n%s", want, trace)
		}
	}
}

func TestManagementKeyIsDecoded(t *testing.T) {
	fakeKube(t)
	key, err := (Kube{}).ManagementKey(t.Context())
	if err != nil || key != "secret-key" {
		t.Errorf("ManagementKey = %q, %v", key, err)
	}
}
