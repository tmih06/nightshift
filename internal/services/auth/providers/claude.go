package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Claude Code's sign-in: OAuth with PKCE through claude.ai, finished by
// pasting the code Anthropic's page shows, so no localhost address is needed.
// Ported from PiG's ai/oauth_anthropic.go (copy-code login).
const (
	claudeClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeAuthorizeURL = "https://claude.ai/oauth/authorize"
	claudeExchangeURL  = "https://platform.claude.com/v1/oauth/token"
	claudeRedirectURI  = "https://platform.claude.com/oauth/code/callback"
	claudeProfileURL   = "https://api.anthropic.com/api/oauth/profile"
	claudeScopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

type claude struct {
	hc                              *http.Client
	authorizeURL, tokenURL, profile string
}

func newClaude(hc *http.Client) *claude {
	return &claude{hc: hc, authorizeURL: claudeAuthorizeURL, tokenURL: claudeExchangeURL, profile: claudeProfileURL}
}

func (*claude) Info() Info {
	return Info{
		ID: "claude", Name: "Claude Code", Secret: "CLAUDE_ACCESS_TOKEN",
		Pool: "claude-code-pool", LoginFile: ".claude/.credentials.json",
		Hosts: []string{"api.anthropic.com"},
	}
}

func (c *claude) Begin() (Pending, error) {
	verifier, challenge, err := pkce()
	if err != nil {
		return Pending{}, err
	}
	q := url.Values{}
	q.Set(paramCode, "true")
	q.Set(paramClientID, claudeClientID)
	q.Set("response_type", "code")
	q.Set(paramRedirect, claudeRedirectURI)
	q.Set("scope", claudeScopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set(paramState, verifier)
	return Pending{
		Provider: c.Info().ID, URL: c.authorizeURL + "?" + q.Encode(),
		State: verifier, Verifier: verifier, RedirectURI: claudeRedirectURI,
	}, nil
}

func (c *claude) Finish(ctx context.Context, p Pending, input string) (Credential, error) {
	code, state := codeAndState(input)
	if err := checkCode(code, state, p.State); err != nil {
		return Credential{}, err
	}
	if state == "" {
		state = p.State
	}
	cred, err := c.token(ctx, map[string]string{
		paramGrant: grantCode, paramClientID: claudeClientID, paramCode: code,
		paramState: state, paramRedirect: p.RedirectURI, paramVerifier: p.Verifier,
	})
	if err != nil {
		return Credential{}, fmt.Errorf("claude sign-in: %w", err)
	}
	c.describe(ctx, &cred)
	return cred, nil
}

func (c *claude) Refresh(ctx context.Context, old Credential) (Credential, error) {
	if old.Refresh == "" {
		return Credential{}, ErrNoRefresh
	}
	cred, err := c.token(ctx, map[string]string{
		paramGrant: grantRefresh, paramClientID: claudeClientID, paramRefreshToken: old.Refresh,
	})
	if err != nil {
		return Credential{}, fmt.Errorf("claude token refresh: %w", err)
	}
	cred.Email, cred.AccountID, cred.Plan, cred.Tier = old.Email, old.AccountID, old.Plan, old.Tier
	return cred, nil
}

func (c *claude) token(ctx context.Context, body map[string]string) (Credential, error) {
	var tok tokenReply
	if err := do(ctx, c.hc, call{method: http.MethodPost, url: c.tokenURL, json: body}, &tok); err != nil {
		return Credential{}, err
	}
	if tok.AccessToken == "" {
		return Credential{}, fmt.Errorf("%s returned no access token", c.tokenURL)
	}
	return Credential{Access: tok.AccessToken, Refresh: tok.RefreshToken, Expires: expiresIn(tok.ExpiresIn)}, nil
}

// describe fills in who signed in and their plan, when Anthropic says; a
// failed lookup keeps the login.
func (c *claude) describe(ctx context.Context, cred *Credential) {
	var prof struct {
		Account struct {
			UUID         string `json:"uuid"`
			Email        string `json:"email"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			Type string `json:"organization_type"`
			Tier string `json:"rate_limit_tier"`
		} `json:"organization"`
	}
	headers := map[string]string{"anthropic-beta": "oauth-2025-04-20"}
	if do(ctx, c.hc, call{method: http.MethodGet, url: c.profile, bearer: cred.Access, headers: headers}, &prof) != nil {
		return
	}
	cred.Email = firstNonEmpty(prof.Account.Email, prof.Account.EmailAddress)
	cred.AccountID = prof.Account.UUID
	cred.Plan = strings.TrimPrefix(prof.Organization.Type, "claude_")
	cred.Tier = prof.Organization.Tier
}

func (*claude) SandboxLogin(c Credential, placeholder string) ([]byte, error) {
	plan := firstNonEmpty(c.Plan, "pro")
	tier := firstNonEmpty(c.Tier, "default_claude_ai")
	return marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken":      placeholder,
		"expiresAt":        int64(1999999999999),
		"scopes":           strings.Fields(claudeScopes),
		"subscriptionType": plan,
		"rateLimitTier":    tier,
	}})
}

func (*claude) Companions(Credential) map[string]string { return nil }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
