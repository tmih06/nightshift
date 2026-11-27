package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Codex's sign-ins: OAuth with PKCE through auth.openai.com, by browser (the
// browser ends on a localhost address that will not load in a cluster, and
// pasting it finishes the sign-in) or by device code (RFC 8628: enter a code
// on OpenAI's page and the sign-in finishes by itself), or a login file
// pasted from a machine where codex is signed in. Ported from PiG's
// ai/oauth_openai_codex.go.
const (
	codexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	codexExchangeURL  = "https://auth.openai.com/oauth/token"
	codexRedirectURI  = "http://localhost:1455/auth/callback"
	codexScope        = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	codexOriginator   = "codex_cli_rs"
	codexAuthClaim    = "https://api.openai.com/auth"
	// The device code endpoints, the page the code is entered on, and the
	// redirect its authorization code is bound to.
	codexDeviceCodeURL  = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDevicePollURL  = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDevicePage     = "https://auth.openai.com/codex/device"
	codexDeviceRedirect = "https://auth.openai.com/deviceauth/callback"
	// codexDeviceSlack is added to the interval OpenAI asks for, as Codex
	// does.
	codexDeviceSlack = 3 * time.Second
	// codexSandboxIDJWT is the id_token every sandbox gets: unsigned, and
	// naming only the sandbox identity and a pro plan.
	codexSandboxIDJWT    = "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJlbWFpbCI6Im5pZ2h0c2hpZnRAcHJveHkubG9jYWwiLCJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9wbGFuX3R5cGUiOiJwcm8ifX0.c2ln"
	codexAccountIDSecret = "CODEX_ACCOUNT_ID"
)

type codex struct {
	hc                                   *http.Client
	authorizeURL, tokenURL               string
	deviceCodeURL, devicePollURL, device string
}

func newCodex(hc *http.Client) *codex {
	return &codex{
		hc: hc, authorizeURL: codexAuthorizeURL, tokenURL: codexExchangeURL,
		deviceCodeURL: codexDeviceCodeURL, devicePollURL: codexDevicePollURL, device: codexDevicePage,
	}
}

func (*codex) Info() Info {
	return Info{
		ID: "codex", Name: "Codex", Secret: "CODEX_ACCESS_TOKEN", Pool: "codex-pool",
		LoginFile: ".codex/auth.json", Hosts: []string{"chatgpt.com"},
		Methods: []Method{
			{
				ID: methodBrowser, Label: "ChatGPT account, in the browser", Kind: KindBrowser,
				Hint: "Approve, then paste the localhost address the browser ends on.",
			},
			{
				ID: methodDevice, Label: "ChatGPT account, with a device code", Kind: KindDevice,
				Hint: "Enter the code on OpenAI's page. Device code sign-in must be on in ChatGPT's security settings.",
			},
			{
				ID: methodFile, Label: "Login file from the Codex CLI", Kind: KindPaste,
				Hint: "Paste ~/.codex/auth.json from a machine where codex is signed in.",
			},
		},
	}
}

func (c *codex) Begin(ctx context.Context, id string) (Pending, error) {
	m, err := method(c, id)
	if err != nil {
		return Pending{}, err
	}
	switch m.ID {
	case methodDevice:
		return c.beginDevice(ctx, m)
	case methodFile:
		return Pending{Provider: c.Info().ID, Method: m}, nil
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
		Provider: c.Info().ID, Method: m, URL: c.authorizeURL + "?" + q.Encode(),
		State: state, Verifier: verifier, RedirectURI: codexRedirectURI,
	}, nil
}

// beginDevice asks OpenAI for a code to enter on its device page.
func (c *codex) beginDevice(ctx context.Context, m Method) (Pending, error) {
	var reply struct {
		ID       string `json:"device_auth_id"`
		UserCode string `json:"user_code"`
		Interval any    `json:"interval"`
	}
	body := map[string]string{paramClientID: codexClientID}
	if err := do(ctx, c.hc, call{method: http.MethodPost, url: c.deviceCodeURL, json: body}, &reply); err != nil {
		return Pending{}, fmt.Errorf("codex device sign-in: %w", err)
	}
	if reply.ID == "" || reply.UserCode == "" {
		return Pending{}, errors.New("codex device sign-in: OpenAI returned no code")
	}
	return Pending{
		Provider: c.Info().ID, Method: m, URL: c.device, UserCode: reply.UserCode,
		Device: reply.ID, Interval: seconds(reply.Interval, 5*time.Second) + codexDeviceSlack,
	}, nil
}

func (c *codex) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	switch p.Method.ID {
	case methodDevice:
		return c.finishDevice(ctx, p)
	case methodFile:
		return c.finishFile(ctx, input)
	}
	code, state := codeAndState(input)
	if err := checkCode(code, state, p.State); err != nil {
		return Credential{}, err
	}
	return c.exchange(ctx, code, p.Verifier, p.RedirectURI)
}

// finishDevice asks whether the code was entered; ErrPending until it was.
func (c *codex) finishDevice(ctx context.Context, p Pending) (Credential, error) {
	status, raw, err := post(ctx, c.hc, c.devicePollURL, map[string]string{"device_auth_id": p.Device, "user_code": p.UserCode})
	if err != nil {
		return Credential{}, fmt.Errorf("codex device sign-in: %w", err)
	}
	if status == http.StatusForbidden || status == http.StatusNotFound || deviceWaiting(raw) {
		return Credential{}, ErrPending
	}
	if status < 200 || status >= 300 {
		return Credential{}, fmt.Errorf("codex device sign-in: OpenAI answered %d: %s", status, firstChars(string(raw), 200))
	}
	var tok struct {
		Code     string `json:"authorization_code"`
		Verifier string `json:"code_verifier"`
	}
	if json.Unmarshal(raw, &tok) != nil || tok.Code == "" || tok.Verifier == "" {
		return Credential{}, errors.New("codex device sign-in: OpenAI returned no authorization code")
	}
	return c.exchange(ctx, tok.Code, tok.Verifier, codexDeviceRedirect)
}

// finishFile takes a pasted auth.json, renewing its token when it is about
// to lapse.
func (c *codex) finishFile(ctx context.Context, input string) (Credential, error) {
	f, ok := loginFile(input)
	if !ok {
		return Credential{}, errNotLoginFile
	}
	auth := codexAuth(f.Access)
	cred := Credential{
		Access: f.Access, Refresh: f.Refresh, Expires: firstTime(jwtExpiry(f.Access), f.Expires),
		Email:     firstNonEmpty(claim(jwtClaims(f.IDToken), "email"), f.Email),
		AccountID: firstNonEmpty(claim(auth, "chatgpt_account_id"), f.AccountID),
		Plan:      claim(auth, "chatgpt_plan_type"),
	}
	if !stale(cred) {
		return cred, nil
	}
	return c.Refresh(ctx, cred)
}

// exchange trades an authorization code for tokens.
func (c *codex) exchange(ctx context.Context, code, verifier, redirect string) (Credential, error) {
	form := url.Values{
		paramGrant: {grantCode}, paramClientID: {codexClientID}, paramCode: {code},
		paramVerifier: {verifier}, paramRedirect: {redirect},
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
	auth := codexAuth(tok.AccessToken)
	cred := Credential{
		Access: tok.AccessToken, Refresh: tok.RefreshToken, Expires: expiresIn(tok.ExpiresIn),
		Email:     firstNonEmpty(claim(jwtClaims(tok.IDToken), "email"), old.Email),
		AccountID: firstNonEmpty(claim(auth, "chatgpt_account_id"), old.AccountID),
		Plan:      firstNonEmpty(claim(auth, "chatgpt_plan_type"), old.Plan),
	}
	return cred, nil
}

// codexAuth is the OpenAI claims of an access token, or nil.
func codexAuth(access string) map[string]any {
	auth, ok := jwtClaims(access)[codexAuthClaim].(map[string]any)
	if !ok {
		return nil
	}
	return auth
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
