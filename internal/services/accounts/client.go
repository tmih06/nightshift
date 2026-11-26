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

// nameKey is the parameter that names a credential file in the API.
const nameKey = "name"

// Client speaks CLIProxyAPI's v8 management API.
type Client struct {
	base string
	key  string
	http *http.Client
}

// NewClient returns a client for the instance at base, authenticated by the
// management key. A nil httpClient gets one with a thirty second timeout.
func NewClient(base, key string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(base, "/"), key: key, http: httpClient}
}

// Login is a sign-in in progress: open URL, approve, and hand the address the
// browser ends on to Complete.
type Login struct {
	Provider string
	URL      string
	State    string
}

// call is one management API request.
type call struct {
	method string
	path   string
	query  url.Values
	body   any
}

func (c *Client) request(ctx context.Context, rq call) (*http.Request, error) {
	u := c.base + "/v8/management" + rq.path
	if len(rq.query) > 0 {
		u += "?" + rq.query.Encode()
	}
	var rd io.Reader
	if rq.body != nil {
		raw, err := json.Marshal(rq.body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, rq.method, u, rd)
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
		return fmt.Errorf("%s: %s", path, e.Error)
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

type wireCredential struct {
	Name           string     `json:"name"`
	Provider       string     `json:"provider"`
	Email          string     `json:"email"`
	Account        string     `json:"account"`
	Disabled       bool       `json:"disabled"`
	Status         string     `json:"status"`
	StatusMessage  string     `json:"status_message"`
	Expires        string     `json:"access_token_expires_at"`
	LastRefresh    string     `json:"last_refresh"`
	NextRefresh    string     `json:"next_refresh_after"`
	Success        int        `json:"success"`
	Failed         int        `json:"failed"`
	RequiresSignIn bool       `json:"requires_sign_in"`
	RenewalState   string     `json:"renewal_state"`
	QuotaBlocked   bool       `json:"quota_blocked"`
	Cooldowns      []any      `json:"cooldowns"`
	RefreshError   *wireError `json:"refresh_error"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (w wireCredential) account() Account {
	a := Account{
		Name:         w.Name,
		Provider:     w.Provider,
		Email:        w.Email,
		Disabled:     w.Disabled,
		Expires:      parseTime(w.Expires),
		LastRefresh:  parseTime(w.LastRefresh),
		NextRefresh:  parseTime(w.NextRefresh),
		Success:      w.Success,
		Failed:       w.Failed,
		RenewalState: w.RenewalState,
		StatusNote:   w.StatusMessage,
		QuotaBlocked: w.QuotaBlocked,
		Cooldowns:    len(w.Cooldowns),
		NeedsSignIn:  w.RequiresSignIn,
	}
	if a.Email == "" {
		a.Email = w.Account
	}
	if w.RefreshError != nil {
		a.RefreshError = strings.TrimSpace(w.RefreshError.Code + " " + w.RefreshError.Message)
	}
	return a
}

// List returns every signed-in account.
func (c *Client) List(ctx context.Context) ([]Account, error) {
	var resp struct {
		Files []wireCredential `json:"files"`
	}
	if err := c.do(ctx, call{method: http.MethodGet, path: "/credentials"}, &resp); err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(resp.Files))
	for i := range resp.Files {
		out = append(out, resp.Files[i].account())
	}
	return out, nil
}

// StartLogin begins signing an account in to provider.
func (c *Client) StartLogin(ctx context.Context, provider string) (Login, error) {
	var resp struct {
		Status string `json:"status"`
		URL    string `json:"url"`
		State  string `json:"state"`
	}
	rq := call{method: http.MethodGet, path: "/oauth/auth-url", query: url.Values{"provider": {provider}}}
	if err := c.do(ctx, rq, &resp); err != nil {
		return Login{}, err
	}
	if resp.URL == "" || resp.State == "" {
		return Login{}, errors.New("CLIProxyAPI returned no sign-in link")
	}
	return Login{Provider: provider, URL: resp.URL, State: resp.State}, nil
}

// Complete hands CLIProxyAPI the address the browser ended on after approval
// (a localhost address that will not load) and waits for the account to be
// saved. It reports the error CLIProxyAPI gives when the sign-in fails.
func (c *Client) Complete(ctx context.Context, l Login, redirect string) error {
	redirect = strings.TrimSpace(redirect)
	u, err := url.Parse(redirect)
	if err != nil || u.Query().Get("code") == "" || u.Query().Get("state") == "" {
		return errors.New("paste the whole address the browser ended on, which has code= and state= in it")
	}
	body := map[string]string{"provider": l.Provider, "redirect_url": redirect}
	if err := c.do(ctx, call{method: http.MethodPost, path: "/oauth/callback", body: body}, nil); err != nil {
		return err
	}
	for i := 0; i < 60; i++ {
		var st struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if err := c.do(ctx, call{method: http.MethodGet, path: "/oauth/status", query: url.Values{"state": {l.State}}}, &st); err != nil {
			return err
		}
		switch st.Status {
		case "ok":
			return nil
		case "error":
			if st.Error == "" {
				st.Error = "sign-in failed"
			}
			return errors.New(st.Error)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for sign-in: %w", ctx.Err())
		case <-time.After(time.Second):
		}
	}
	return errors.New("sign-in did not finish within a minute")
}

// Cancel abandons a sign-in that was started but not completed.
func (c *Client) Cancel(ctx context.Context, l Login) error {
	return c.do(ctx, call{method: http.MethodDelete, path: "/oauth/session", query: url.Values{"state": {l.State}}}, nil)
}

// SetDisabled turns an account off or on without deleting it.
func (c *Client) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return c.do(ctx, call{method: http.MethodPatch, path: "/credentials/status", body: map[string]any{nameKey: name, "disabled": disabled}}, nil)
}

// Remove deletes an account and its tokens.
func (c *Client) Remove(ctx context.Context, name string) error {
	return c.do(ctx, call{method: http.MethodDelete, path: "/credentials", query: url.Values{nameKey: {name}}}, nil)
}

// Refresh renews an account's access token now.
func (c *Client) Refresh(ctx context.Context, name string) error {
	return c.do(ctx, call{method: http.MethodPost, path: "/credentials/refresh", body: map[string]string{nameKey: name}}, nil)
}
