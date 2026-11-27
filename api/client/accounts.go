package client

import (
	"context"
	"net/http"
	"net/url"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// Accounts reads every signed-in account, which one each agent uses, and what
// the egress proxy counted for each. It carries no token.
func (c *Client) Accounts(ctx context.Context) (v1.Accounts, error) {
	return fetch[v1.Accounts](ctx, c, request{method: http.MethodGet, path: "/v1/accounts"})
}

// StartLogin begins signing an account in to provider by one of its methods,
// the first when method is empty. What to do next depends on the login's
// Method.Kind (see v1.LoginBrowser, v1.LoginDevice, v1.LoginPaste).
func (c *Client) StartLogin(ctx context.Context, provider, method string) (v1.Login, error) {
	return fetch[v1.Login](ctx, c, request{method: http.MethodPost, path: "/v1/accounts/logins", body: v1.LoginRequest{Provider: provider, Method: method}})
}

// CompleteLogin finishes a sign-in with what the person pasted, or asks after
// a device sign-in when input is empty; it is not Done until approved.
func (c *Client) CompleteLogin(ctx context.Context, l v1.Login, input string) (v1.LoginProgress, error) {
	body := v1.LoginCompletion{Provider: l.Provider, Input: input}
	return fetch[v1.LoginProgress](ctx, c, request{method: http.MethodPost, path: "/v1/accounts/logins/" + esc(l.ID) + "/complete", body: body})
}

// CancelLogin abandons a sign-in that was started.
func (c *Client) CancelLogin(ctx context.Context, l v1.Login) error {
	q := url.Values{"provider": {l.Provider}}
	return c.json(ctx, request{method: http.MethodDelete, path: "/v1/accounts/logins/" + esc(l.ID), query: q}, nil)
}

// SetDisabled turns an account off or on without deleting it.
func (c *Client) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return c.json(ctx, request{method: http.MethodPatch, path: "/v1/accounts/" + esc(name), body: v1.AccountPatch{Disabled: &disabled}}, nil)
}

// RemoveAccount deletes an account and its tokens.
func (c *Client) RemoveAccount(ctx context.Context, name string) error {
	return c.json(ctx, request{method: http.MethodDelete, path: "/v1/accounts/" + esc(name)}, nil)
}

// RefreshAccount renews an account's access token now.
func (c *Client) RefreshAccount(ctx context.Context, name string) error {
	return c.json(ctx, request{method: http.MethodPost, path: "/v1/accounts/" + esc(name) + "/refresh"}, nil)
}

// SetActive picks the primary account at the head of a provider's chain.
func (c *Client) SetActive(ctx context.Context, provider, name string) error {
	body := v1.ActiveAccount{Account: name}
	return c.json(ctx, request{method: http.MethodPut, path: "/v1/accounts/active/" + esc(provider), body: body}, nil)
}

// SetChain sets the ordered fallback chain for a provider's accounts.
func (c *Client) SetChain(ctx context.Context, provider string, names []string) error {
	body := v1.AccountChain{Accounts: names}
	return c.json(ctx, request{method: http.MethodPut, path: "/v1/accounts/chain/" + esc(provider), body: body}, nil)
}

// Publish puts the chosen accounts into the cluster now, and returns what the
// publisher reported.
func (c *Client) Publish(ctx context.Context) (string, error) {
	out, err := fetch[v1.PublishReport](ctx, c, request{method: http.MethodPost, path: "/v1/accounts/publish"})
	return out.Report, err
}

// RefreshQuotas asks the egress proxy to actively fetch live provider quotas
// for all loaded accounts now.
func (c *Client) RefreshQuotas(ctx context.Context) error {
	return c.json(ctx, request{method: http.MethodPost, path: "/v1/accounts/refresh-quotas"}, nil)
}
