package providers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// jwt is an unsigned token carrying claims.
func jwt(t *testing.T, claims map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".sig"
}

// oauthServer answers a token endpoint with reply and records each request's
// form or JSON body.
type oauthServer struct {
	*httptest.Server
	got []map[string]string
}

func newOAuthServer(t *testing.T, reply func(body map[string]string) any) *oauthServer {
	t.Helper()
	s := &oauthServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Error(err)
			}
		} else if q, err := url.ParseQuery(string(raw)); err == nil {
			for k := range q {
				body[k] = q.Get(k)
			}
		}
		body["path"] = r.URL.Path
		body["auth"] = r.Header.Get("Authorization")
		s.got = append(s.got, body)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(reply(body)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// redirect is the address a browser ends on after approving p.
func redirect(p Pending, code string) string {
	return p.RedirectURI + "?code=" + code + "&state=" + p.State
}

func TestEveryAgentStartsASignInWithPKCE(t *testing.T) {
	for _, p := range All(nil) {
		pend, err := p.Begin()
		if err != nil {
			t.Fatalf("%s: %v", p.Info().ID, err)
		}
		u, err := url.Parse(pend.URL)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" || q.Get("state") != pend.State || pend.Verifier == "" {
			t.Errorf("%s: sign-in link %s lacks PKCE or state", p.Info().ID, pend.URL)
		}
		if q.Get("redirect_uri") != pend.RedirectURI {
			t.Errorf("%s: redirect_uri %q, want %q", p.Info().ID, q.Get("redirect_uri"), pend.RedirectURI)
		}
	}
}

func TestAPasteFromAnotherSignInIsRefused(t *testing.T) {
	for _, p := range All(nil) {
		pend, err := p.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Finish(t.Context(), pend, pend.RedirectURI+"?code=abc&state=someone-else"); err == nil {
			t.Errorf("%s accepted another sign-in's state", p.Info().ID)
		}
		if _, err := p.Finish(t.Context(), pend, ""); err == nil {
			t.Errorf("%s accepted an empty paste", p.Info().ID)
		}
	}
}

func TestCodexSignInReadsTheAccountFromItsTokens(t *testing.T) {
	access := jwt(t, map[string]any{codexAuthClaim: map[string]any{"chatgpt_account_id": "acct-1", "chatgpt_plan_type": "pro"}})
	idToken := jwt(t, map[string]any{"email": "a@example.com"})
	srv := newOAuthServer(t, func(map[string]string) any {
		return map[string]any{"access_token": access, "refresh_token": "r1", "id_token": idToken, "expires_in": 3600}
	})
	c := &codex{hc: srv.Client(), authorizeURL: codexAuthorizeURL, tokenURL: srv.URL + "/oauth/token"}
	pend, err := c.Begin()
	if err != nil {
		t.Fatal(err)
	}
	cred, err := c.Finish(t.Context(), pend, redirect(pend, "the-code"))
	if err != nil {
		t.Fatal(err)
	}
	if cred.Email != "a@example.com" || cred.AccountID != "acct-1" || cred.Refresh != "r1" || cred.Expires.Before(time.Now()) {
		t.Errorf("credential = %+v", cred)
	}
	got := srv.got[0]
	if got["code"] != "the-code" || got["code_verifier"] != pend.Verifier || got["grant_type"] != "authorization_code" {
		t.Errorf("exchange sent %v", got)
	}
	if c.Companions(cred)[codexAccountIDSecret] != "acct-1" {
		t.Errorf("companions = %v", c.Companions(cred))
	}
}

// Google leaves the refresh token out of a refresh reply; the old one must be
// kept, or the next renewal has nothing to send.
func TestAntigravityRefreshKeepsItsRefreshToken(t *testing.T) {
	srv := newOAuthServer(t, func(map[string]string) any {
		return map[string]any{"access_token": "new-access", "expires_in": 3599}
	})
	a := &antigravity{hc: srv.Client(), tokenURL: srv.URL}
	cred, err := a.Refresh(t.Context(), Credential{Access: "old", Refresh: "keep-me", Email: "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if cred.Access != "new-access" || cred.Refresh != "keep-me" || cred.Email != "a@example.com" {
		t.Errorf("credential = %+v", cred)
	}
	if got := srv.got[0]; got["client_id"] != agyClientID || got["refresh_token"] != "keep-me" {
		t.Errorf("refresh sent %v: Antigravity's grants refresh only on its own client", got)
	}
}

func TestClaudeSignInTakesTheCodeAnthropicShows(t *testing.T) {
	srv := newOAuthServer(t, func(body map[string]string) any {
		if body["path"] == "/profile" {
			return map[string]any{"account": map[string]any{"uuid": "u-1", "email": "c@example.com"}, "organization": map[string]any{"organization_type": "claude_max", "rate_limit_tier": "default_claude_max_20x"}}
		}
		return map[string]any{"access_token": "fake-access-1", "refresh_token": "fake-refresh-1", "expires_in": 28800}
	})
	c := &claude{hc: srv.Client(), authorizeURL: claudeAuthorizeURL, tokenURL: srv.URL + "/token", profile: srv.URL + "/profile"}
	pend, err := c.Begin()
	if err != nil {
		t.Fatal(err)
	}
	cred, err := c.Finish(t.Context(), pend, "the-code#"+pend.State)
	if err != nil {
		t.Fatal(err)
	}
	if cred.Email != "c@example.com" || cred.Plan != "max" || cred.Tier != "default_claude_max_20x" {
		t.Errorf("credential = %+v", cred)
	}
	if got := srv.got[0]; got["code"] != "the-code" || got["redirect_uri"] != claudeRedirectURI || got["state"] != pend.State {
		t.Errorf("exchange sent %v", got)
	}
	if srv.got[1]["auth"] != "Bearer fake-access-1" {
		t.Errorf("profile asked with %q", srv.got[1]["auth"])
	}
}

func TestDevinSessionTokenTravelsWithItsPrefix(t *testing.T) {
	session := jwt(t, map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "email": "d@example.com"})
	srv := newOAuthServer(t, func(map[string]string) any { return map[string]any{"token": session} })
	d := &devin{hc: srv.Client(), authorizeURL: devinAuthorizeURL, tokenURL: srv.URL}
	pend, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	cred, err := d.Finish(t.Context(), pend, redirect(pend, "c"))
	if err != nil {
		t.Fatal(err)
	}
	if cred.Access != devinSession+session || cred.Email != "d@example.com" || cred.Expires.IsZero() {
		t.Errorf("credential = %+v", cred)
	}
	key, err := d.Finish(t.Context(), pend, "  sk-ws-abc  ")
	if err != nil || key.Access != "sk-ws-abc" {
		t.Errorf("a pasted key = %+v, %v", key, err)
	}
	if _, err := d.Refresh(t.Context(), Credential{Access: "x", Expires: time.Now().Add(-time.Minute)}); !errors.Is(err, ErrNoRefresh) {
		t.Errorf("an expired session renews: %v", err)
	}
}

// A sandbox login holds the placeholder and names no account: no email, no
// account id, no refresh token (rule: build each sandbox login from a named
// list of fields).
func TestSandboxLoginsNameNoAccount(t *testing.T) {
	cred := Credential{
		Access: "REAL-ACCESS", Refresh: "REAL-REFRESH", Email: "someone@example.com",
		AccountID: "REAL-ACCOUNT-ID", Plan: "pro", Expires: time.Now().Add(time.Hour),
	}
	for _, p := range All(nil) {
		info := p.Info()
		ph := "nightshift:secret:" + info.Secret
		login, err := p.SandboxLogin(cred, ph)
		if err != nil {
			t.Fatalf("%s: %v", info.ID, err)
		}
		s := string(login)
		if !strings.Contains(s, ph) {
			t.Errorf("%s: login %s lacks its placeholder", info.ID, s)
		}
		for _, leak := range []string{"REAL-ACCESS", "REAL-REFRESH", "someone@example.com", "REAL-ACCOUNT-ID"} {
			if strings.Contains(s, leak) {
				t.Errorf("%s: login %s carries %q", info.ID, s, leak)
			}
		}
		if info.ID == "" || info.Name == "" || info.Secret == "" || info.Pool == "" || info.LoginFile == "" {
			t.Errorf("incomplete info %+v", info)
		}
	}
}
