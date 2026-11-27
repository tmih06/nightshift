package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"charm.land/log/v2"
)

// Client speaks nightshift-auth's API (cmd/nightshift-auth): the service that
// signs accounts in, keeps their tokens, and publishes them.
type Client struct {
	base string
	key  string
	http *http.Client
}

// NewClient returns a client for the service at base, authenticated by its
// key. A nil httpClient gets one with a ninety second timeout: finishing a
// sign-in waits on the provider and then on a publish.
func NewClient(base, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), key: key, http: httpClient}
}

// Login is a sign-in in progress: open URL, approve, and hand what the
// browser ends on (or the code the page shows) to Complete.
type Login struct {
	Provider string
	URL      string
	State    string // the sign-in's id
}

// call is one API request.
type call struct {
	method string
	path   string
	body   any
}

func (c *Client) request(ctx context.Context, rq call) (*http.Request, error) {
	var rd io.Reader
	if rq.body != nil {
		raw, err := json.Marshal(rq.body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, rq.method, c.base+rq.path, rd)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if rq.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// closeBody closes a response body, noting a failure in the debug log.
func closeBody(rc io.Closer) {
	if err := rc.Close(); err != nil {
		log.Debug("accounts: close response body", "err", err)
	}
}

// failure turns an error response into an error carrying the server's reason.
func failure(path string, status int, raw []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		return errors.New(e.Error)
	}
	return fmt.Errorf("%s: HTTP %d", path, status)
}

func (c *Client) do(ctx context.Context, rq call, out any) error {
	req, err := c.request(ctx, rq)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", rq.method, rq.path, err)
	}
	defer closeBody(resp.Body)
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read %s: %w", rq.path, err)
	}
	if resp.StatusCode >= 300 {
		return failure(rq.path, resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse %s: %w", rq.path, err)
	}
	return nil
}

type wireAccount struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Email        string    `json:"email"`
	Plan         string    `json:"plan"`
	Disabled     bool      `json:"disabled"`
	NeedsSignIn  bool      `json:"needsSignIn"`
	Expires      time.Time `json:"expires"`
	RefreshedAt  time.Time `json:"refreshedAt"`
	NextRefresh  time.Time `json:"nextRefresh"`
	RefreshError string    `json:"refreshError"`
	Refreshes    int       `json:"refreshes"`
	Failures     int       `json:"failures"`
}

func (w *wireAccount) account() Account {
	return Account{
		Name: w.Name, Provider: w.Provider, Email: w.Email, Disabled: w.Disabled,
		Expires: w.Expires, LastRefresh: w.RefreshedAt, NextRefresh: w.NextRefresh,
		Success: w.Refreshes, Failed: w.Failures, RefreshError: w.RefreshError,
		NeedsSignIn: w.NeedsSignIn, StatusNote: w.Plan,
	}
}

// Vault is what nightshift-auth holds, without a token in it.
type Vault struct {
	Accounts    []Account
	Chains      map[string][]string
	PublishedAt time.Time
	Published   map[string]Published
}

// Providers lists the agents the service signs in, in the tab's order.
func (c *Client) Providers(ctx context.Context) ([]Provider, error) {
	var out []Provider
	if err := c.do(ctx, call{method: http.MethodGet, path: "/v1/providers"}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Vault reads every account, each agent's chain, and the last publish.
func (c *Client) Vault(ctx context.Context) (Vault, error) {
	var resp struct {
		Accounts  []wireAccount       `json:"accounts"`
		Chains    map[string][]string `json:"chains"`
		Published struct {
			At       time.Time            `json:"at"`
			Accounts map[string]Published `json:"accounts"`
		} `json:"published"`
	}
	if err := c.do(ctx, call{method: http.MethodGet, path: "/v1/accounts"}, &resp); err != nil {
		return Vault{}, err
	}
	v := Vault{Chains: resp.Chains, PublishedAt: resp.Published.At, Published: resp.Published.Accounts}
	for i := range resp.Accounts {
		v.Accounts = append(v.Accounts, resp.Accounts[i].account())
	}
	return v, nil
}

// StartLogin begins signing an account in to provider.
func (c *Client) StartLogin(ctx context.Context, provider string) (Login, error) {
	var resp struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/logins", body: map[string]string{"provider": provider}}, &resp); err != nil {
		return Login{}, err
	}
	if resp.URL == "" || resp.ID == "" {
		return Login{}, errors.New("nightshift-auth returned no sign-in link")
	}
	return Login{Provider: provider, URL: resp.URL, State: resp.ID}, nil
}

// Complete finishes a sign-in with what the browser ended on, or the code
// the provider's page showed, and returns once the account is saved and
// published. It reports why when the sign-in fails.
func (c *Client) Complete(ctx context.Context, l Login, input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return errors.New("paste the whole address the browser ended on, or the code the page showed")
	}
	return c.do(ctx, call{method: http.MethodPost, path: "/v1/logins/" + url.PathEscape(l.State), body: map[string]string{"input": input}}, nil)
}

// Cancel abandons a sign-in that was started but not completed.
func (c *Client) Cancel(ctx context.Context, l Login) error {
	return c.do(ctx, call{method: http.MethodDelete, path: "/v1/logins/" + url.PathEscape(l.State)}, nil)
}

// SetDisabled turns an account off or on without deleting it.
func (c *Client) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return c.do(ctx, call{method: http.MethodPatch, path: "/v1/accounts/" + url.PathEscape(name), body: map[string]bool{"disabled": disabled}}, nil)
}

// Remove deletes an account and its tokens.
func (c *Client) Remove(ctx context.Context, name string) error {
	return c.do(ctx, call{method: http.MethodDelete, path: "/v1/accounts/" + url.PathEscape(name)}, nil)
}

// Refresh renews an account's access token now.
func (c *Client) Refresh(ctx context.Context, name string) error {
	return c.do(ctx, call{method: http.MethodPost, path: "/v1/accounts/" + url.PathEscape(name) + "/refresh"}, nil)
}

// SetChain sets the order provider's accounts are tried in.
func (c *Client) SetChain(ctx context.Context, provider string, names []string) error {
	return c.do(ctx, call{method: http.MethodPut, path: "/v1/chains/" + url.PathEscape(provider), body: map[string][]string{"accounts": names}}, nil)
}

// Publish puts the accounts into the cluster now and returns the report.
func (c *Client) Publish(ctx context.Context) (string, error) {
	var resp struct {
		Report string `json:"report"`
	}
	if err := c.do(ctx, call{method: http.MethodPost, path: "/v1/publish"}, &resp); err != nil {
		return "", err
	}
	return resp.Report, nil
}
