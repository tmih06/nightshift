package server

import (
	"encoding/json"
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
		return nil, fmt.Errorf("the connection cannot stream")
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
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// keepAlive sends a comment, which clients ignore and proxies count as traffic.
func (s *stream) keepAlive() error {
	if _, err := fmt.Fprint(s.w, ": keep-alive\n\n"); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}
