package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Devin's sign-ins: a browser PKCE flow against app.devin.ai that yields a
// session token, sent to Devin's server as devin-session-token$<token>. The
// browser ends on a 127.0.0.1 address; pasting it finishes the sign-in. A
// Devin API key, a Windsurf key, or the CLI's credentials.toml can be pasted
// instead. None can be renewed. Ported from PiG's ai/oauth_devin.go (browser
// and CLI-credential logins) and ai/devin_proto.go.
const (
	devinAuthorizeURL = "https://app.devin.ai/auth/cli/continue"
	devinExchangeURL  = "https://api.devin.ai/auth/cli/token"
	devinRedirectURI  = "http://127.0.0.1:59653/callback"
	devinSession      = "devin-session-token$"
)

// devinKeyLine is the key line of the CLI's credentials.toml.
var devinKeyLine = regexp.MustCompile(`(?m)^\s*windsurf_api_key\s*=\s*"([^"]+)"`)

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
		Methods: []Method{
			{
				ID: methodBrowser, Label: "Devin account, in the browser", Kind: KindBrowser,
				Hint: "Approve, then paste the 127.0.0.1 address the browser ends on.",
			},
			{
				ID: methodToken, Label: "API key, or the CLI's credentials.toml", Kind: KindPaste,
				Hint: "Paste a Devin API key (apk_, cog_), a Windsurf key (sk-ws-), or " +
					"~/.local/share/devin/credentials.toml from a machine where devin is signed in.",
			},
		},
	}
}

func (d *devin) Begin(_ context.Context, id string) (Pending, error) {
	m, err := method(d, id)
	if err != nil {
		return Pending{}, err
	}
	if m.Kind == KindPaste {
		return Pending{Provider: d.Info().ID, Method: m}, nil
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
	q.Set(paramRedirect, devinRedirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set(paramState, state)
	q.Set("prompt", "select_account")
	return Pending{
		Provider: d.Info().ID, Method: m, URL: d.authorizeURL + "?" + q.Encode(),
		State: state, Verifier: verifier, RedirectURI: devinRedirectURI,
	}, nil
}

func (d *devin) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	input = strings.TrimSpace(input)
	if m := devinKeyLine.FindStringSubmatch(input); m != nil {
		input = m[1]
	}
	if isDevinKey(input) {
		return devinKey(input), nil
	}
	if p.Method.Kind == KindPaste {
		return Credential{}, errors.New("that is not a Devin key: paste a key starting apk_, cog_, sk-ws-, or devin-session-token$, or the whole credentials.toml")
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

// devinKey is a pasted key as a credential; a session token says who it is
// and when it lapses.
func devinKey(key string) Credential {
	session, ok := strings.CutPrefix(key, devinSession)
	if !ok {
		return Credential{Access: key}
	}
	claims := jwtClaims(session)
	return Credential{Access: key, Expires: jwtExpiry(session), Email: claim(claims, "email"), AccountID: claim(claims, "sub")}
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
