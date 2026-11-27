package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Codex's sign-in: OAuth with PKCE through auth.openai.com. The browser ends
// on a localhost address that will not load in a cluster; pasting it
// finishes the sign-in. Ported from PiG's ai/oauth_openai_codex.go.
const (
	codexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexExchangeURL  = "https://auth.openai.com/oauth/token"
	codexRedirectURI  = "http://localhost:1455/auth/callback"
	codexScope        = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	codexOriginator   = "codex_cli_rs"
	codexAuthClaim    = "https://api.openai.com/auth"
	// codexSandboxIDJWT is the id_token every sandbox gets: unsigned, and
	// naming only the sandbox identity and a pro plan.
	codexSandboxIDJWT    = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJlbWFpbCI6Im5pZ2h0c2hpZnRAcHJveHkubG9jYWwiLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9wbGFuX3R5cGUiOiJwcm8ifX0.c2ln"
	codexAccountIDSecret = "CODEX_ACCOUNT_ID"
)

type codex struct {
	hc                     *http.Client
	authorizeURL, tokenURL string
}

func newCodex(hc *http.Client) *codex {
	return &codex{hc: hc, authorizeURL: codexAuthorizeURL, tokenURL: codexExchangeURL}
}

func (*codex) Info() Info {
	return Info{
		ID: "codex", Name: "Codex", Secret: "CODEX_ACCESS_TOKEN", Pool: "codex-pool",
		LoginFile: ".codex/auth.json", Hosts: []string{"chatgpt.com"},
	}
}

func (c *codex) Begin() (Pending, error) {
	verifier, challenge, err := pkce()
	if err != nil {
		return Pending{}, err
	}
	state, err := randomHex(16)
	if err != nil {
		return Pending{}, err
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set(paramClientID, codexClientID)
	q.Set(paramRedirect, codexRedirectURI)
	q.Set("scope", codexScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set(paramState, state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", codexOriginator)
	return Pending{
		Provider: c.Info().ID, URL: c.authorizeURL + "?" + q.Encode(),
		State: state, Verifier: verifier, RedirectURI: codexRedirectURI,
	}, nil
}

func (c *codex) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	code, state := codeAndState(input)
	if err := checkCode(code, state, p.State); err != nil {
		return Credential{}, err
	}
	form := url.Values{
		paramGrant: {grantCode}, paramClientID: {codexClientID}, paramCode: {code},
		paramVerifier: {p.Verifier}, paramRedirect: {p.RedirectURI},
	}
	cred, err := c.token(ctx, form, Credential{})
	if err != nil {
		return Credential{}, fmt.Errorf("codex sign-in: %w", err)
	}
	return cred, nil
}

func (c *codex) Refresh(ctx context.Context, old Credential) (Credential, error) {
	if old.Refresh == "" {
		return Credential{}, ErrNoRefresh
	}
	form := url.Values{paramGrant: {grantRefresh}, paramClientID: {codexClientID}, paramRefreshToken: {old.Refresh}}
	cred, err := c.token(ctx, form, old)
	if err != nil {
		return Credential{}, fmt.Errorf("codex token refresh: %w", err)
	}
	return cred, nil
}

// token exchanges form for tokens, keeping what old knew of the account when
// the new tokens do not say.
func (c *codex) token(ctx context.Context, form url.Values, old Credential) (Credential, error) {
	var tok tokenReply
	if err := do(ctx, c.hc, call{method: http.MethodPost, url: c.tokenURL, form: form}, &tok); err != nil {
		return Credential{}, err
	}
	if tok.AccessToken == "" || tok.RefreshToken == "" {
		return Credential{}, fmt.Errorf("%s returned no tokens", c.tokenURL)
	}
	auth, ok := jwtClaims(tok.AccessToken)[codexAuthClaim].(map[string]any)
	if !ok {
		auth = nil
	}
	cred := Credential{
		Access: tok.AccessToken, Refresh: tok.RefreshToken, Expires: expiresIn(tok.ExpiresIn),
		Email:     firstNonEmpty(claim(jwtClaims(tok.IDToken), "email"), old.Email),
		AccountID: firstNonEmpty(claim(auth, "chatgpt_account_id"), old.AccountID),
		Plan:      firstNonEmpty(claim(auth, "chatgpt_plan_type"), old.Plan),
	}
	return cred, nil
}

func (*codex) SandboxLogin(c Credential, placeholder string) ([]byte, error) {
	var accountID any
	if c.AccountID != "" {
		accountID = "nightshift:secret:" + codexAccountIDSecret
	}
	return marshal(map[string]any{
		"auth_mode":      "chatgpt",
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token":      codexSandboxIDJWT,
			"access_token":  placeholder,
			"refresh_token": "nightshift:secret:CODEX_REFRESH_TOKEN_UNUSABLE",
			"account_id":    accountID,
		},
		"last_refresh": farFuture,
	})
}

func (*codex) Companions(c Credential) map[string]string {
	if c.AccountID == "" {
		return nil
	}
	return map[string]string{codexAccountIDSecret: c.AccountID}
}
