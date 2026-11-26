package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is the slice of CLIProxyAPI's management API the client uses.
type fakeAPI struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []string
	statuses []string // successive /oauth/status answers
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer secret-key" {
		w.WriteHeader(http.StatusUnauthorized)
		f.say(w, `{"error":"invalid management key"}`)
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
	f.mu.Unlock()
	switch {
	case r.URL.Path == "/v8/management/credentials" && r.Method == http.MethodGet:
		f.say(w, `{"files":[
		 {"name":"claude-a.json","provider":"claude","email":"a@example.com","status":"active","access_token_expires_at":"2030-01-01T00:00:00Z","last_refresh":"2029-12-31T20:00:00Z","success":3,"failed":1,"cooldowns":[{}]},
		 {"name":"codex-b.json","provider":"codex","account":"b@example.com","disabled":true,"requires_sign_in":true,"refresh_error":{"code":"invalid_grant","message":"revoked"}}]}`)
	case r.URL.Path == "/v8/management/oauth/auth-url":
		f.say(w, `{"status":"ok","url":"https://claude.ai/oauth/authorize?state=abc","state":"abc"}`)
	case r.URL.Path == "/v8/management/oauth/callback":
		f.callback(w, r)
	case r.URL.Path == "/v8/management/oauth/status":
		f.oauthStatus(w)
	default:
		f.say(w, `{"status":"ok"}`)
	}
}

func (f *fakeAPI) say(w http.ResponseWriter, body string) { reply(f.t, w, body) }

func (f *fakeAPI) callback(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if body["provider"] != "claude" || !strings.Contains(body["redirect_url"], "code=xyz") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f.say(w, `{"status":"ok"}`)
}

func (f *fakeAPI) oauthStatus(w http.ResponseWriter) {
	f.mu.Lock()
	st := "ok"
	if len(f.statuses) > 0 {
		st, f.statuses = f.statuses[0], f.statuses[1:]
	}
	f.mu.Unlock()
	if st == "error" {
		f.say(w, `{"status":"error","error":"account is on a free plan"}`)
		return
	}
	f.say(w, `{"status":"`+st+`"}`)
}

func newTestClient(t *testing.T) (*Client, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{t: t}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "secret-key", nil), api
}

func TestListParsesAccounts(t *testing.T) {
	c, _ := newTestClient(t)
	accounts, err := c.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("got %d accounts, want 2", len(accounts))
	}
	a, b := accounts[0], accounts[1]
	if a.Label() != "a@example.com" || a.Success != 3 || a.Failed != 1 || a.Cooldowns != 1 || a.Expires.Year() != 2030 {
		t.Errorf("first account = %+v", a)
	}
	if b.Label() != "b@example.com" || !b.Disabled || !b.NeedsSignIn || !strings.Contains(b.RefreshError, "invalid_grant") {
		t.Errorf("second account = %+v (the account field must stand in for a missing email)", b)
	}
}

func TestWrongKeyIsReported(t *testing.T) {
	c, _ := newTestClient(t)
	bad := NewClient(c.base, "nope", nil)
	if _, err := bad.List(t.Context()); err == nil || !strings.Contains(err.Error(), "invalid management key") {
		t.Errorf("List with a wrong key = %v, want the server's message", err)
	}
}

func TestLoginRoundTrip(t *testing.T) {
	c, api := newTestClient(t)
	api.statuses = []string{"wait", "ok"}
	ctx := context.Background()
	l, err := c.StartLogin(ctx, "claude")
	if err != nil || l.State != "abc" || !strings.HasPrefix(l.URL, "https://claude.ai/") {
		t.Fatalf("StartLogin = %+v, %v", l, err)
	}
	if err := c.Complete(ctx, l, "not an address"); err == nil {
		t.Error("an address without code and state must be refused before anything is sent")
	}
	if err := c.Complete(ctx, l, " http://localhost:54545/callback?code=xyz&state=abc "); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestLoginFailureCarriesTheServersReason(t *testing.T) {
	c, api := newTestClient(t)
	api.statuses = []string{"error"}
	l := Login{Provider: "claude", State: "abc"}
	err := c.Complete(t.Context(), l, "http://localhost:54545/callback?code=xyz&state=abc")
	if err == nil || !strings.Contains(err.Error(), "free plan") {
		t.Errorf("Complete = %v, want the reason from CLIProxyAPI", err)
	}
}

func TestAccountActionsUseTheDocumentedEndpoints(t *testing.T) {
	c, api := newTestClient(t)
	ctx := t.Context()
	for _, err := range []error{
		c.SetDisabled(ctx, "claude-a.json", true),
		c.Refresh(ctx, "claude-a.json"),
		c.Remove(ctx, "claude a.json"),
		c.Cancel(ctx, Login{State: "abc"}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"PATCH /v8/management/credentials/status",
		"POST /v8/management/credentials/refresh",
		"DELETE /v8/management/credentials?name=claude+a.json",
		"DELETE /v8/management/oauth/session?state=abc",
	}
	if strings.Join(api.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", api.calls, want)
	}
}
