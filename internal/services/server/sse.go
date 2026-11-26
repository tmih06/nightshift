package server

import (
	"charm.land/log/v2"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// stream writes server-sent events.
type stream struct {
	w http.ResponseWriter
	f http.Flusher
}

// newStream starts an event stream, or says why it cannot.
func newStream(w http.ResponseWriter) (*stream, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("the connection cannot stream")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // proxies must not hold events back
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &stream{w: w, f: f}, nil
}

// event sends one event whose data is v as JSON.
func (s *stream) event(name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("event: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return fmt.Errorf("event: %w", err)
	}
	s.f.Flush()
	return nil
}

// fail ends the stream with the error as a Problem. The client may already
// be gone, which is nothing to report.
func (s *stream) fail(err error) {
	if err := s.event("error", problemOf(err)); err != nil {
		log.Debug("could not send the stream's error", "err", err)
	}
}

// keepAlive sends a comment, which clients ignore and proxies count as traffic.
func (s *stream) keepAlive() error {
	if _, err := fmt.Fprint(s.w, ": keep-alive\n\n"); err != nil {
		return fmt.Errorf("keep alive: %w", err)
	}
	s.f.Flush()
	return nil
}
