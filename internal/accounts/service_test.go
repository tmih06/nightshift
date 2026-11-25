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
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usage" {
			http.NotFound(w, r)
			return
		}
		reply(t, w, `[{"binding":"CLAUDE_ACCESS_TOKEN","label":"a@example.com","requests":7,"ok":5,"rate_limited":2,"recent":[0,3,4]}]`)
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
	if !byName["claude-a.json"].Active {
		t.Errorf("claude-a must be active when the chosen account is gone: %+v", byName["claude-a.json"])
	}
	u := byName["claude-a.json"].Usage
	if u == nil || u.Requests != 7 || u.RateLimited != 2 || len(u.Recent) != 3 {
		t.Errorf("usage = %+v, want the proxy's counts for a@example.com", u)
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

func TestManagementKeyIsDecoded(t *testing.T) {
	fakeKube(t)
	key, err := (Kube{}).ManagementKey(t.Context())
	if err != nil || key != "secret-key" {
		t.Errorf("ManagementKey = %q, %v", key, err)
	}
}
