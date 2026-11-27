package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Devin's sign-in: a browser PKCE flow against app.devin.ai that yields a
// session token, sent to Devin's server as devin-session-token$<token>. The
// browser ends on a 127.0.0.1 address; pasting it finishes the sign-in. A
// Devin API key pasted instead is kept as it is. Neither can be renewed.
// Ported from PiG's ai/oauth_devin.go and ai/devin_proto.go.
const (
	devinAuthorizeURL = "https://app.devin.ai/auth/cli/continue"
	devinExchangeURL  = "https://api.devin.ai/auth/cli/token"
	devinRedirectURI  = "http://127.0.0.1:59653/callback"
	devinSession      = "devin-session-token$"
)

type devin struct {
	hc                     *http.Client
	authorizeURL, tokenURL string
}

func newDevin(hc *http.Client) *devin {
	return &devin{hc: hc, authorizeURL: devinAuthorizeURL, tokenURL: devinExchangeURL}
}

func (*devin) Info() Info {
	return Info{
		ID: "devin", Name: "Devin", Secret: "DEVIN_ACCESS_TOKEN",
		Pool: "devin-pool", LoginFile: ".local/share/devin/credentials.toml",
		Hosts: []string{"server.codeium.com"},
	}
}

func (d *devin) Begin() (Pending, error) {
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
	q.Set(paramRedirect, devinRedirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set(paramState, state)
	q.Set("prompt", "select_account")
	return Pending{
		Provider: d.Info().ID, URL: d.authorizeURL + "?" + q.Encode(),
		State: state, Verifier: verifier, RedirectURI: devinRedirectURI,
	}, nil
}

func (d *devin) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	input = strings.TrimSpace(input)
	if isDevinKey(input) {
		return Credential{Access: input}, nil
	}
	code, state := codeAndState(input)
	if err := checkCode(code, state, p.State); err != nil {
		return Credential{}, err
	}
	var reply struct {
		Token string `json:"token"`
	}
	body := map[string]string{paramCode: code, paramVerifier: p.Verifier}
	if err := do(ctx, d.hc, call{method: http.MethodPost, url: d.tokenURL, json: body}, &reply); err != nil {
		return Credential{}, fmt.Errorf("devin sign-in: %w", err)
	}
	if reply.Token == "" {
		return Credential{}, fmt.Errorf("devin sign-in: %s returned no token", d.tokenURL)
	}
	claims := jwtClaims(reply.Token)
	return Credential{
		Access:    devinSession + reply.Token,
		Expires:   jwtExpiry(reply.Token),
		Email:     claim(claims, "email"),
		AccountID: claim(claims, "sub"),
	}, nil
}

// isDevinKey is whether input is a key rather than a redirect or a code: a
// session token, a Windsurf key, or a Devin API key.
func isDevinKey(input string) bool {
	for _, prefix := range []string{devinSession, "sk-ws-", "apk_", "cog_"} {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}
	return false
}

// Refresh keeps a session token until it lapses: it has no refresh grant.
func (*devin) Refresh(_ context.Context, old Credential) (Credential, error) {
	if !old.Expires.IsZero() && time.Now().After(old.Expires) {
		return Credential{}, ErrNoRefresh
	}
	return old, nil
}

func (*devin) SandboxLogin(_ Credential, placeholder string) ([]byte, error) {
	return []byte(fmt.Sprintf("windsurf_api_key = %q\n", placeholder)), nil
}

func (*devin) Companions(Credential) map[string]string { return nil }
