package client

import (
	"context"
	"net/url"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// Accounts reads every signed-in account, which one each agent uses, and what
// the egress proxy counted for each. It carries no token.
func (c *Client) Accounts(ctx context.Context) (v1.Accounts, error) {
	var out v1.Accounts
	return out, c.json(ctx, request{method: "GET", path: "/v1/accounts"}, &out)
}

// StartLogin begins signing an account in. Open the login's URL in a browser,
// approve, and hand the address the browser ends on to CompleteLogin.
func (c *Client) StartLogin(ctx context.Context, provider string) (v1.Login, error) {
	var out v1.Login
	return out, c.json(ctx, request{method: "POST", path: "/v1/accounts/logins", body: v1.LoginRequest{Provider: provider}}, &out)
}

// CompleteLogin finishes a sign-in with the address the browser ended on.
func (c *Client) CompleteLogin(ctx context.Context, l v1.Login, redirectURL string) error {
	body := v1.LoginCompletion{Provider: l.Provider, RedirectURL: redirectURL}
	return c.json(ctx, request{method: "POST", path: "/v1/accounts/logins/" + esc(l.ID) + "/complete", body: body}, nil)
}

// CancelLogin abandons a sign-in that was started.
func (c *Client) CancelLogin(ctx context.Context, l v1.Login) error {
	q := url.Values{"provider": {l.Provider}}
	return c.json(ctx, request{method: "DELETE", path: "/v1/accounts/logins/" + esc(l.ID), query: q}, nil)
}

// SetDisabled turns an account off or on without deleting it.
func (c *Client) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return c.json(ctx, request{method: "PATCH", path: "/v1/accounts/" + esc(name), body: v1.AccountPatch{Disabled: &disabled}}, nil)
}

// RemoveAccount deletes an account and its tokens.
func (c *Client) RemoveAccount(ctx context.Context, name string) error {
	return c.json(ctx, request{method: "DELETE", path: "/v1/accounts/" + esc(name)}, nil)
}

// RefreshAccount renews an account's access token now.
func (c *Client) RefreshAccount(ctx context.Context, name string) error {
	return c.json(ctx, request{method: "POST", path: "/v1/accounts/" + esc(name) + "/refresh"}, nil)
}

// SetActive picks the account a provider's new sandboxes get.
func (c *Client) SetActive(ctx context.Context, provider, name string) error {
	body := v1.ActiveAccount{Account: name}
	return c.json(ctx, request{method: "PUT", path: "/v1/accounts/active/" + esc(provider), body: body}, nil)
}

// Publish puts the chosen accounts into the cluster now, and returns what the
// publisher reported.
func (c *Client) Publish(ctx context.Context) (string, error) {
	var out v1.PublishReport
	return out.Report, c.json(ctx, request{method: "POST", path: "/v1/accounts/publish"}, &out)
}
