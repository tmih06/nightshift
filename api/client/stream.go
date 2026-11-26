package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// maxEvent bounds one event's data. A fleet with every manifest is the largest.
const maxEvent = 64 << 20

// event is one server-sent event.
type event struct {
	name string
	data []byte
}

// stream reads the server-sent events of a call, handing each to fn, until the
// stream ends, fn fails, or ctx ends.
func (c *Client) stream(ctx context.Context, rq request, fn func(event) error) (err error) {
	rq.accept = "text/event-stream"
	resp, err := c.do(ctx, rq)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeErr(resp.Body)) }()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), maxEvent)
	var ev event
	for sc.Scan() {
		if err := ev.feed(sc.Text(), fn); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read the stream: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stream stopped: %w", err)
	}
	return nil
}

// feed takes one line of a stream: a field of the event being built, or the
// blank line that ends it, which hands the event to fn.
func (ev *event) feed(line string, fn func(event) error) error {
	switch {
	case line == "":
		done := *ev
		*ev = event{}
		if done.name != "" || len(done.data) > 0 {
			return fn(done)
		}
	case strings.HasPrefix(line, ":"):
		// a comment: the server keeping the connection alive
	case strings.HasPrefix(line, "event:"):
		ev.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	case strings.HasPrefix(line, "data:"):
		ev.data = append(ev.data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")...)
	}
	return nil
}

// errEnd ends a stream from inside its handler without being an error.
var errEnd = errors.New("end of stream")

// decodeEvent turns an event into v, or into the error an "error" event carries.
func decodeEvent(ev event, want string, v any) (bool, error) {
	switch ev.name {
	case "error":
		var p v1.Problem
		if err := json.Unmarshal(ev.data, &p); err != nil {
			return false, fmt.Errorf("the server sent an error event that is not a Problem: %w", err)
		}
		return false, &Error{Problem: p}
	case want:
		if err := json.Unmarshal(ev.data, v); err != nil {
			return false, fmt.Errorf("read a %s event: %w", want, err)
		}
		return true, nil
	}
	return false, nil // an event this client does not know: ignored, as the contract says
}

// WatchFleet calls fn with the fleet now and each time it changes, until ctx
// ends, fn fails, or the connection drops. It returns nil when the server ends
// the stream, so a caller that wants to keep watching connects again.
func (c *Client) WatchFleet(ctx context.Context, fn func(v1.Fleet) error, include ...string) error {
	err := c.stream(ctx, request{method: http.MethodGet, path: "/v1/fleet/watch", query: includeQuery(include)}, func(ev event) error {
		var f v1.Fleet
		ok, err := decodeEvent(ev, "fleet", &f)
		if err != nil || !ok {
			return err
		}
		return fn(f)
	})
	if errors.Is(err, errEnd) {
		return nil
	}
	return err
}

// FollowCheckout calls fn with a checkout's events, and returns once the last
// one, which has Done or Error set, has been delivered. A client that joins a
// checkout late is caught up first.
func (c *Client) FollowCheckout(ctx context.Context, id string, fn func(v1.CheckoutEvent) error) error {
	err := c.stream(ctx, request{method: http.MethodGet, path: "/v1/checkouts/" + url.PathEscape(id) + "/events"}, func(ev event) error {
		var e v1.CheckoutEvent
		ok, err := decodeEvent(ev, "checkout", &e)
		if err != nil || !ok {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
		if e.Done || e.Error != "" {
			return errEnd
		}
		return nil
	})
	if errors.Is(err, errEnd) {
		return nil
	}
	return err
}
