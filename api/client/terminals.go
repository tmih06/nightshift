package client

import (
	"context"
	"net/url"
	"strconv"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func terminalsPath(sandbox string) string { return "/v1/sandboxes/" + esc(sandbox) + "/terminals" }

// Terminals lists the live terminals of a claimed sandbox.
func (c *Client) Terminals(ctx context.Context, sandbox string) ([]v1.Terminal, error) {
	var out v1.Terminals
	return out.Terminals, c.json(ctx, request{method: "GET", path: terminalsPath(sandbox)}, &out)
}

// OpenTerminal opens a terminal. With an empty request on a sandbox that has
// none yet, it starts the sandbox's session and returns its first terminal.
func (c *Client) OpenTerminal(ctx context.Context, sandbox string, req v1.TerminalRequest) (v1.Terminal, error) {
	var out v1.Terminal
	return out, c.json(ctx, request{method: "POST", path: terminalsPath(sandbox), body: req}, &out)
}

// CloseTerminal stops a terminal.
func (c *Client) CloseTerminal(ctx context.Context, sandbox, address string) error {
	return c.json(ctx, request{method: "DELETE", path: terminalsPath(sandbox) + "/" + esc(address)}, nil)
}

// Screen waits up to wait for a terminal to change, and returns what it shows.
// Changed is false when nothing changed in time. resize says the size is new,
// which repaints everything. ctx should outlast wait.
func (c *Client) Screen(ctx context.Context, sandbox, address string, cols, rows int, resize bool, wait time.Duration) (v1.Screen, error) {
	q := url.Values{
		"cols": {strconv.Itoa(cols)}, "rows": {strconv.Itoa(rows)},
		"resize": {strconv.FormatBool(resize)},
		"wait":   {strconv.FormatInt(wait.Milliseconds(), 10)},
		"viewer": {c.viewer},
	}
	var out v1.Screen
	return out, c.json(ctx, request{method: "GET", path: terminalsPath(sandbox) + "/" + esc(address) + "/screen", query: q}, &out)
}

// Input presses keys in a terminal, in order.
func (c *Client) Input(ctx context.Context, sandbox, address string, keys []v1.Key) error {
	body := v1.Input{Keys: keys}
	return c.json(ctx, request{method: "POST", path: terminalsPath(sandbox) + "/" + esc(address) + "/input", body: body}, nil)
}
