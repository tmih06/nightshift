package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func terminalsPath(sandbox string) string { return "/v1/sandboxes/" + esc(sandbox) + "/terminals" }

// Terminals lists the live terminals of a claimed sandbox.
func (c *Client) Terminals(ctx context.Context, sandbox string) ([]v1.Terminal, error) {
	out, err := fetch[v1.Terminals](ctx, c, request{method: http.MethodGet, path: terminalsPath(sandbox)})
	return out.Terminals, err
}

// OpenTerminal opens a terminal. With an empty request on a sandbox that has
// none yet, it starts the sandbox's session and returns its first terminal.
func (c *Client) OpenTerminal(ctx context.Context, sandbox string, req v1.TerminalRequest) (v1.Terminal, error) {
	return fetch[v1.Terminal](ctx, c, request{method: http.MethodPost, path: terminalsPath(sandbox), body: req})
}

// CloseTerminal stops a terminal.
func (c *Client) CloseTerminal(ctx context.Context, sandbox, address string) error {
	return c.json(ctx, request{method: http.MethodDelete, path: terminalsPath(sandbox) + "/" + esc(address)}, nil)
}

// ScreenRequest says which terminal's screen to read, at what size, and how
// long to wait for it to change.
type ScreenRequest struct {
	Sandbox string
	Address string
	Cols    int
	Rows    int
	// Resize says the size is new, which repaints everything.
	Resize bool
	// Wait is how long to wait for a change before answering with none.
	Wait time.Duration
}

// Screen waits up to r.Wait for a terminal to change, and returns what it
// shows. Changed is false when nothing changed in time. ctx should outlast
// the wait.
func (c *Client) Screen(ctx context.Context, r ScreenRequest) (v1.Screen, error) {
	q := url.Values{
		"cols": {strconv.Itoa(r.Cols)}, "rows": {strconv.Itoa(r.Rows)},
		"resize": {strconv.FormatBool(r.Resize)},
		"wait":   {strconv.FormatInt(r.Wait.Milliseconds(), 10)},
		"viewer": {c.viewer},
	}
	return fetch[v1.Screen](ctx, c, request{method: http.MethodGet, path: terminalsPath(r.Sandbox) + "/" + esc(r.Address) + "/screen", query: q})
}

// Input presses keys in a terminal, in order.
func (c *Client) Input(ctx context.Context, sandbox, address string, keys []v1.Key) error {
	body := v1.Input{Keys: keys}
	return c.json(ctx, request{method: http.MethodPost, path: terminalsPath(sandbox) + "/" + esc(address) + "/input", body: body}, nil)
}
