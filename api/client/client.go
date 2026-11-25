// Package client is the Go client of the Nightshift services API. It is what
// the terminal UI uses, and it is meant for any other Go frontend: it depends
// on nothing but the standard library and the contract's types in api/v1.
//
//	c, _ := client.New("http://127.0.0.1:7777", token)
//	fleet, err := c.Fleet(ctx, v1.IncludeLive)
//
// Every method takes a context, which ends the request. A call that the API
// answers with an error returns an *Error carrying the contract's Problem;
// IsNotFound and the other Is functions say which.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// maxResponse bounds what a client reads of one response, so that a server
// gone wrong cannot fill its memory. The largest honest body is a fleet with
// every manifest.
const maxResponse = 64 << 20

// Client talks to one Nightshift server.
type Client struct {
	base   *url.URL
	token  string
	http   *http.Client
	viewer string
}

// Option changes how a Client is made.
type Option func(*Client)

// WithHTTPClient uses h for requests, for a proxy, a CA, or a test. It should
// have no overall Timeout: streams and long polls outlive any.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithViewer names this client's view of terminals. Two frontends with
// different names can watch one terminal each at its own size. The default is
// a name that is unique to the Client.
func WithViewer(id string) Option { return func(c *Client) { c.viewer = id } }

// New returns a Client for the server at base, such as http://127.0.0.1:7777.
func New(base, token string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not a server address like http://127.0.0.1:7777", base)
	}
	c := &Client{base: u, token: token, http: &http.Client{}, viewer: "c" + strconv.FormatInt(time.Now().UnixNano(), 36)}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// URL is the server's address.
func (c *Client) URL() string { return c.base.String() }

// Error is an error the API answered with.
type Error struct {
	v1.Problem
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (%d %s)", e.Problem.Error(), e.Status, e.Code)
}

func has(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// IsNotFound is whether err says something does not exist.
func IsNotFound(err error) bool { return has(err, v1.CodeNotFound) }

// IsUnauthorized is whether err says the token is missing or wrong.
func IsUnauthorized(err error) bool { return has(err, v1.CodeUnauthorized) }

// IsUnavailable is whether err says the cluster, or a service in it, cannot be
// reached, or that the server does not offer the part asked for.
func IsUnavailable(err error) bool { return has(err, v1.CodeUnavailable) }

// IsConflict is whether err says the resource's state forbids the request.
func IsConflict(err error) bool { return has(err, v1.CodeConflict) }

// request is one API call.
type request struct {
	method string
	path   string
	query  url.Values
	body   any
	accept string
}

func (c *Client) do(ctx context.Context, rq request) (*http.Response, error) {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + rq.path
	u.RawPath = ""
	if len(rq.query) > 0 {
		u.RawQuery = rq.query.Encode()
	}
	var body io.Reader
	if rq.body != nil {
		raw, err := json.Marshal(rq.body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, rq.method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if rq.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if rq.accept != "" {
		req.Header.Set("Accept", rq.accept)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		return nil, problemOf(resp)
	}
	return resp, nil
}

// problemOf reads the error the API answered with. A body that is not a
// Problem, from a proxy in the way, still becomes an Error.
func problemOf(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var p v1.Problem
	if err := json.Unmarshal(raw, &p); err != nil || p.Code == "" {
		p = v1.Problem{Title: http.StatusText(resp.StatusCode), Status: resp.StatusCode, Detail: strings.TrimSpace(string(raw)), Code: codeOf(resp.StatusCode)}
	}
	return &Error{Problem: p}
}

// codeOf is the contract's code for a status that came without a Problem.
func codeOf(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return v1.CodeUnauthorized
	case http.StatusNotFound:
		return v1.CodeNotFound
	case http.StatusConflict:
		return v1.CodeConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return v1.CodeInvalid
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusNotImplemented:
		return v1.CodeUnavailable
	case http.StatusGatewayTimeout:
		return v1.CodeTimeout
	}
	return v1.CodeInternal
}

// json calls the API and decodes the answer into out, when there is one.
func (c *Client) json(ctx context.Context, rq request, out any) error {
	resp, err := c.do(ctx, rq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(out); err != nil {
		return fmt.Errorf("read the answer to %s %s: %w", rq.method, rq.path, err)
	}
	return nil
}

func esc(s string) string { return url.PathEscape(s) }

// Info says who is answering and what it can do.
func (c *Client) Info(ctx context.Context) (v1.Info, error) {
	var out v1.Info
	return out, c.json(ctx, request{method: "GET", path: "/v1"}, &out)
}

// Healthy is whether the server answers its liveness check.
func (c *Client) Healthy(ctx context.Context) error {
	resp, err := c.do(ctx, request{method: "GET", path: "/v1/healthz"})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func includeQuery(include []string) url.Values {
	if len(include) == 0 {
		return nil
	}
	return url.Values{"include": {strings.Join(include, ",")}}
}

// Fleet reads the sandbox fleet. include is any of v1.IncludeConfigs,
// v1.IncludeLive, and v1.IncludeVersions.
func (c *Client) Fleet(ctx context.Context, include ...string) (v1.Fleet, error) {
	var out v1.Fleet
	return out, c.json(ctx, request{method: "GET", path: "/v1/fleet", query: includeQuery(include)}, &out)
}

// PodLogs reads the last tail lines of a pod's log.
func (c *Client) PodLogs(ctx context.Context, namespace, pod string, tail int) ([]string, error) {
	var out v1.Logs
	q := url.Values{"tail": {strconv.Itoa(tail)}}
	err := c.json(ctx, request{method: "GET", path: "/v1/pods/" + esc(namespace) + "/" + esc(pod) + "/logs", query: q}, &out)
	return out.Lines, err
}

// Claim claims a sandbox from a warm pool. The claim may not have a sandbox
// yet; WaitClaim waits for one.
func (c *Client) Claim(ctx context.Context, pool string) (v1.Claim, error) {
	var out v1.Claim
	return out, c.json(ctx, request{method: "POST", path: "/v1/claims", body: v1.ClaimRequest{Pool: pool}}, &out)
}

// WaitClaim reads a claim, waiting up to wait for it to have a sandbox. The
// claim comes back without one when the wait runs out.
func (c *Client) WaitClaim(ctx context.Context, name string, wait time.Duration) (v1.Claim, error) {
	var out v1.Claim
	q := url.Values{"wait": {strconv.Itoa(int(wait.Seconds()))}}
	return out, c.json(ctx, request{method: "GET", path: "/v1/claims/" + esc(name), query: q}, &out)
}

// DeleteClaim releases a claim; its sandbox goes with it.
func (c *Client) DeleteClaim(ctx context.Context, name string) error {
	return c.json(ctx, request{method: "DELETE", path: "/v1/claims/" + esc(name)}, nil)
}

// Checkout starts putting a target on a volume of its own and a sandbox on it.
func (c *Client) Checkout(ctx context.Context, req v1.CheckoutRequest) (v1.Checkout, error) {
	var out v1.Checkout
	return out, c.json(ctx, request{method: "POST", path: "/v1/checkouts", body: req}, &out)
}

// GetCheckout is where a checkout stands.
func (c *Client) GetCheckout(ctx context.Context, id string) (v1.Checkout, error) {
	var out v1.Checkout
	return out, c.json(ctx, request{method: "GET", path: "/v1/checkouts/" + esc(id)}, &out)
}

// Volumes lists the volumes that keep checked-out work.
func (c *Client) Volumes(ctx context.Context) ([]v1.Volume, error) {
	var out v1.Volumes
	return out.Volumes, c.json(ctx, request{method: "GET", path: "/v1/volumes"}, &out)
}

// DeleteVolume deletes a volume and the clone on it.
func (c *Client) DeleteVolume(ctx context.Context, name string) error {
	return c.json(ctx, request{method: "DELETE", path: "/v1/volumes/" + esc(name)}, nil)
}

// DeleteSandbox ends a sandbox a checkout started, and its volume too when
// deleteVolume is set.
func (c *Client) DeleteSandbox(ctx context.Context, name string, deleteVolume bool) error {
	q := url.Values{"deleteVolume": {strconv.FormatBool(deleteVolume)}}
	return c.json(ctx, request{method: "DELETE", path: "/v1/sandboxes/" + esc(name), query: q}, nil)
}
