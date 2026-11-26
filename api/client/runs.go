package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// CreateRun hands a task to a coding agent. With an idempotency key that was
// used before, it returns the run made then.
func (c *Client) CreateRun(ctx context.Context, req v1.RunRequest) (v1.Run, error) {
	return fetch[v1.Run](ctx, c, request{method: http.MethodPost, path: "/v1/runs", body: req})
}

// GetRun is where a run stands, with the end of the agent's output.
func (c *Client) GetRun(ctx context.Context, id string) (v1.Run, error) {
	return fetch[v1.Run](ctx, c, request{method: http.MethodGet, path: "/v1/runs/" + esc(id)})
}

// Runs lists runs, newest first; a non-empty externalRef keeps only that one's.
func (c *Client) Runs(ctx context.Context, externalRef string) ([]v1.Run, error) {
	var q url.Values
	if externalRef != "" {
		q = url.Values{"externalRef": {externalRef}}
	}
	out, err := fetch[v1.Runs](ctx, c, request{method: http.MethodGet, path: "/v1/runs", query: q})
	return out.Runs, err
}

// DeleteRun stops a run, and destroys its sandbox and the work on its volume.
func (c *Client) DeleteRun(ctx context.Context, id string) error {
	return c.json(ctx, request{method: http.MethodDelete, path: "/v1/runs/" + esc(id)}, nil)
}

// FollowRun calls fn with the run now and each time it changes, and returns
// once it has finished.
func (c *Client) FollowRun(ctx context.Context, id string, fn func(v1.Run) error) error {
	err := c.stream(ctx, request{method: http.MethodGet, path: "/v1/runs/" + url.PathEscape(id) + "/events"}, func(ev event) error {
		var r v1.Run
		ok, err := decodeEvent(ev, "run", &r)
		if err != nil || !ok {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
		if r.Finished() {
			return errEnd
		}
		return nil
	})
	if errors.Is(err, errEnd) {
		return nil
	}
	return err
}
