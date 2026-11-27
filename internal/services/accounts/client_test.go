package accounts

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is the slice of nightshift-auth's API the client uses.
type fakeAPI struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []string
	bodies []string
	chains map[string][]string
	reject string // the reason a sign-in is refused, when set
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer secret-key" {
		w.WriteHeader(http.StatusUnauthorized)
		f.say(w, `{"error":"missing or wrong key"}`)
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Error(err)
	}
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.EscapedPath())
	f.bodies = append(f.bodies, string(raw))
	f.mu.Unlock()
	switch {
	case r.URL.Path == "/v1/providers":
		f.say(w, `[{"id":"claude","name":"Claude Code","secret":"CLAUDE_ACCESS_TOKEN"},{"id":"codex","name":"Codex","secret":"CODEX_ACCESS_TOKEN"}]`)
	case r.URL.Path == "/v1/accounts":
		f.accounts(w)
	case r.URL.Path == "/v1/logins":
		w.WriteHeader(http.StatusCreated)
		f.say(w, `{"id":"abc","provider":"claude","method":{"id":"browser","label":"browser","kind":"browser"},"url":"https://claude.ai/oauth/authorize?state=abc"}`)
	case strings.HasPrefix(r.URL.Path, "/v1/logins/") && r.Method == http.MethodPost && f.reject != "":
		w.WriteHeader(http.StatusBadRequest)
		f.say(w, `{"error":"`+f.reject+`"}`)
	case strings.HasPrefix(r.URL.Path, "/v1/chains/"):
		var body struct {
			Accounts []string `json:"accounts"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			f.t.Error(err)
		}
		f.mu.Lock()
		f.chains[strings.TrimPrefix(r.URL.Path, "/v1/chains/")] = body.Accounts
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/v1/publish":
		f.say(w, `{"report":"claude: a@example.com (chain of 1)\n"}`)
	default:
		f.say(w, `{"account":"claude-a@example.com"}`)
	}
}

func (f *fakeAPI) accounts(w http.ResponseWriter) {
	f.mu.Lock()
	chains, err := json.Marshal(f.chains)
	f.mu.Unlock()
	if err != nil {
		f.t.Fatal(err)
	}
	f.say(w, `{"accounts":[
	 {"name":"claude-a@example.com","provider":"claude","email":"a@example.com","plan":"max","expires":"2030-01-01T00:00:00Z","refreshedAt":"2029-12-31T20:00:00Z","refreshes":3,"failures":1},
	 {"name":"claude-z","provider":"claude"},
	 {"name":"codex-b","provider":"codex","email":"b@example.com","disabled":true,"needsSignIn":true,"refreshError":"invalid_grant: revoked"}],
	 "chains":`+string(chains)+`,
	 "published":{"at":"2026-01-01T00:00:00Z","accounts":{"claude":{"name":"claude-a@example.com","label":"a@example.com","expires":"2026-01-01T06:00:00Z"}}}}`)
}

func (f *fakeAPI) say(w http.ResponseWriter, body string) { reply(f.t, w, body) }

func newTestClient(t *testing.T) (*Client, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{t: t, chains: map[string][]string{"claude": {"claude-a@example.com", "claude-z"}}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "secret-key", nil), api
}

func TestVaultParsesAccounts(t *testing.T) {
	c, _ := newTestClient(t)
	v, err := c.Vault(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Accounts) != 3 {
		t.Fatalf("got %d accounts, want 3", len(v.Accounts))
	}
	a, b := v.Accounts[0], v.Accounts[2]
	if a.Label() != "a@example.com" || a.Success != 3 || a.Failed != 1 || a.Expires.Year() != 2030 || a.StatusNote != "max" {
		t.Errorf("first account = %+v", a)
	}
	if !b.Disabled || !b.NeedsSignIn || !strings.Contains(b.RefreshError, "invalid_grant") {
		t.Errorf("third account = %+v", b)
	}
	if v.Published["claude"].Label != "a@example.com" || v.PublishedAt.IsZero() {
		t.Errorf("published = %+v at %v", v.Published, v.PublishedAt)
	}
}

func TestWrongKeyIsReported(t *testing.T) {
	c, _ := newTestClient(t)
	bad := NewClient(c.base, "nope", nil)
	if _, err := bad.Vault(t.Context()); err == nil || !strings.Contains(err.Error(), "wrong key") {
		t.Errorf("Vault with a wrong key = %v, want the server's message", err)
	}
}

func TestLoginRoundTrip(t *testing.T) {
	c, api := newTestClient(t)
	l, err := c.StartLogin(t.Context(), "claude", "")
	if err != nil || l.State != "abc" || !strings.HasPrefix(l.URL, "https://claude.ai/") || l.Method.Kind != KindBrowser {
		t.Fatalf("StartLogin = %+v, %v", l, err)
	}
	if _, err := c.Complete(t.Context(), l, "   "); err == nil {
		t.Error("an empty paste must be refused before anything is sent")
	}
	if p, err := c.Complete(t.Context(), l, " the-code#abc "); err != nil || !p.Done || p.Account != "claude-a@example.com" {
		t.Fatalf("Complete = %+v, %v", p, err)
	}
	if got := api.bodies[len(api.bodies)-1]; got != `{"input":"the-code#abc"}` {
		t.Errorf("Complete sent %s", got)
	}
}

func TestLoginFailureCarriesTheServersReason(t *testing.T) {
	c, api := newTestClient(t)
	api.reject = "account is on a free plan"
	_, err := c.Complete(t.Context(), Login{Provider: "claude", State: "abc"}, "code#abc")
	if err == nil || !strings.Contains(err.Error(), "free plan") {
		t.Errorf("Complete = %v, want the reason from nightshift-auth", err)
	}
}

func TestAccountActionsUseTheDocumentedEndpoints(t *testing.T) {
	c, api := newTestClient(t)
	ctx := t.Context()
	for _, err := range []error{
		c.SetDisabled(ctx, "claude-a@example.com", true),
		c.Refresh(ctx, "claude-a@example.com"),
		c.Remove(ctx, "claude a"),
		c.Cancel(ctx, Login{State: "abc"}),
		c.SetChain(ctx, "claude", []string{"claude-z"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"PATCH /v1/accounts/claude-a@example.com",
		"POST /v1/accounts/claude-a@example.com/refresh",
		"DELETE /v1/accounts/claude%20a",
		"DELETE /v1/logins/abc",
		"PUT /v1/chains/claude",
	}
	if strings.Join(api.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", api.calls, want)
	}
}
