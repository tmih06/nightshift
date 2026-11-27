package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Antigravity's sign-ins: Google OAuth with PKCE on Antigravity's own
// published desktop client, which its refresh must use too. The browser ends
// on a 127.0.0.1 address; pasting it finishes the sign-in. Google offers this
// client no device code, so the other way in is a login file pasted from a
// machine where agy is signed in. agy binds its own Cloud Code project, so
// none is bound here. Ported from PiG's ai/google_cloud_code_auth.go.
const (
	agyClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	agyClientSecret = "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"
	agyAuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	agyExchangeURL  = "https://oauth2.googleapis.com/token"
	agyUserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo?alt=json"
	agyRedirectURI  = "http://127.0.0.1:8085/oauth2callback"
)

// agyScopes are the scopes Antigravity's sign-in asks for.
const agyScopes = "https://www.googleapis.com/auth/cloud-platform " +
	"https://www.googleapis.com/auth/userinfo.email " +
	"https://www.googleapis.com/auth/userinfo.profile"

type antigravity struct {
	hc                               *http.Client
	authorizeURL, tokenURL, userInfo string
}

func newAntigravity(hc *http.Client) *antigravity {
	return &antigravity{hc: hc, authorizeURL: agyAuthorizeURL, tokenURL: agyExchangeURL, userInfo: agyUserInfoURL}
}

func (*antigravity) Info() Info {
	return Info{
		ID: "antigravity", Name: "Antigravity", Secret: "AGY_ACCESS_TOKEN",
		Pool: "agy-pool", LoginFile: ".gemini/antigravity-cli/antigravity-oauth-token",
		Hosts: []string{"cloudcode-pa.googleapis.com", "daily-cloudcode-pa.googleapis.com", "aicode.googleapis.com"},
		Methods: []Method{
			{
				ID: methodBrowser, Label: "Google account, in the browser", Kind: KindBrowser,
				Hint: "Approve, then paste the 127.0.0.1 address the browser ends on.",
			},
			{
				ID: methodFile, Label: "Login file from agy", Kind: KindPaste,
				Hint: "Paste ~/.gemini/antigravity-cli/antigravity-oauth-token from a machine where agy is signed in.",
			},
		},
	}
}

func (a *antigravity) Begin(_ context.Context, id string) (Pending, error) {
	m, err := method(a, id)
	if err != nil {
		return Pending{}, err
	}
	if m.Kind == KindPaste {
		return Pending{Provider: a.Info().ID, Method: m}, nil
	}
	verifier, challenge, err := pkce()
	if err != nil {
		return Pending{}, err
	}
	state, err := randomHex(16)
	if err != nil {
		return Pending{}, err
	}
	q := url.Values{}
	q.Set(paramClientID, agyClientID)
	q.Set(paramRedirect, agyRedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", agyScopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set(paramState, state)
	return Pending{
		Provider: a.Info().ID, Method: m, URL: a.authorizeURL + "?" + q.Encode(),
		State: state, Verifier: verifier, RedirectURI: agyRedirectURI,
	}, nil
}

func (a *antigravity) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	if p.Method.ID == methodFile {
		return a.finishFile(ctx, input)
	}
	code, state := codeAndState(input)
	if err := checkCode(code, state, p.State); err != nil {
		return Credential{}, err
	}
	form := url.Values{
		paramClientID: {agyClientID}, "client_secret": {agyClientSecret}, paramCode: {code},
		paramGrant: {grantCode}, paramRedirect: {p.RedirectURI}, paramVerifier: {p.Verifier},
	}
	cred, err := a.token(ctx, form, "")
	if err != nil {
		return Credential{}, fmt.Errorf("antigravity sign-in: %w", err)
	}
	a.lookupEmail(ctx, &cred)
	return cred, nil
}

// finishFile takes agy's pasted token file, renewing its token when it is
// about to lapse.
func (a *antigravity) finishFile(ctx context.Context, input string) (Credential, error) {
	f, ok := loginFile(input)
	if !ok {
		return Credential{}, errNotLoginFile
	}
	cred := Credential{Access: f.Access, Refresh: f.Refresh, Expires: f.Expires, Email: f.Email}
	if stale(cred) {
		renewed, err := a.Refresh(ctx, cred)
		if err != nil {
			return Credential{}, err
		}
		cred = renewed
	}
	if cred.Email == "" || cred.Email == SandboxEmail {
		cred.Email = ""
		a.lookupEmail(ctx, &cred)
	}
	return cred, nil
}

// lookupEmail asks Google who signed in; a failed lookup keeps the login.
func (a *antigravity) lookupEmail(ctx context.Context, cred *Credential) {
	var info struct {
		Email string `json:"email"`
	}
	if do(ctx, a.hc, call{method: http.MethodGet, url: a.userInfo, bearer: cred.Access}, &info) == nil {
		cred.Email = info.Email
	}
}

func (a *antigravity) Refresh(ctx context.Context, old Credential) (Credential, error) {
	if old.Refresh == "" {
		return Credential{}, ErrNoRefresh
	}
	form := url.Values{
		paramClientID: {agyClientID}, "client_secret": {agyClientSecret},
		paramRefreshToken: {old.Refresh}, paramGrant: {grantRefresh},
	}
	cred, err := a.token(ctx, form, old.Refresh)
	if err != nil {
		return Credential{}, fmt.Errorf("antigravity token refresh: %w", err)
	}
	cred.Email = old.Email
	return cred, nil
}

// token exchanges form; Google leaves the refresh token out of a refresh
// reply, so keep is the one to go on using.
func (a *antigravity) token(ctx context.Context, form url.Values, keep string) (Credential, error) {
	var tok tokenReply
	if err := do(ctx, a.hc, call{method: http.MethodPost, url: a.tokenURL, form: form}, &tok); err != nil {
		return Credential{}, err
	}
	if tok.AccessToken == "" {
		return Credential{}, fmt.Errorf("%s returned no access token", a.tokenURL)
	}
	return Credential{Access: tok.AccessToken, Refresh: firstNonEmpty(tok.RefreshToken, keep), Expires: expiresIn(tok.ExpiresIn)}, nil
}

func (*antigravity) SandboxLogin(_ Credential, placeholder string) ([]byte, error) {
	return marshal(map[string]any{
		"token": map[string]any{
			"access_token": placeholder, "token_type": "Bearer",
			"expiry": farFuture, "expires_in": 99999999,
		},
		"auth_method":    "consumer",
		"id_token_email": SandboxEmail,
	})
}

func (*antigravity) Companions(Credential) map[string]string { return nil }
