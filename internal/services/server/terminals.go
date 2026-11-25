package server

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

const (
	maxScreenSize = 1000
	maxScreenWait = 30000
	maxKeys       = 4096
)

var viewerID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func (s *Server) terminalRoutes() []route {
	const base = "/v1/sandboxes/{name}/terminals"
	return []route{
		{method: http.MethodGet, pattern: base, handle: s.listTerminals},
		{method: http.MethodPost, pattern: base, handle: s.createTerminal},
		{method: http.MethodDelete, pattern: base + "/{address}", handle: s.deleteTerminal},
		{method: http.MethodGet, pattern: base + "/{address}/screen", handle: s.getScreen},
		{method: http.MethodPost, pattern: base + "/{address}/input", handle: s.sendInput},
	}
}

func (s *Server) listTerminals(w http.ResponseWriter, r *http.Request) error {
	t, err := need(s.cfg.Terminals, "terminals")
	if err != nil {
		return err
	}
	terms, err := t.Terminals(r.Context(), r.PathValue("name"))
	if err != nil {
		return fmt.Errorf("list terminals: %w", err)
	}
	if terms == nil {
		terms = []v1.Terminal{}
	}
	writeJSON(w, http.StatusOK, v1.Terminals{Terminals: terms})
	return nil
}

func (s *Server) createTerminal(w http.ResponseWriter, r *http.Request) error {
	t, err := need(s.cfg.Terminals, "terminals")
	if err != nil {
		return err
	}
	var req v1.TerminalRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			return err
		}
	}
	term, err := t.OpenTerminal(r.Context(), r.PathValue("name"), req)
	if err != nil {
		return fmt.Errorf("create terminal: %w", err)
	}
	writeJSON(w, http.StatusCreated, term)
	return nil
}

func (s *Server) deleteTerminal(w http.ResponseWriter, r *http.Request) error {
	t, err := need(s.cfg.Terminals, "terminals")
	if err != nil {
		return err
	}
	if err := t.CloseTerminal(r.Context(), r.PathValue("name"), r.PathValue("address")); err != nil {
		return fmt.Errorf("delete terminal: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// screenQuery reads and checks a screen request's parameters.
func screenQuery(r *http.Request) (ScreenQuery, error) {
	var q ScreenQuery
	var err error
	if q.Cols, err = intParam(r, "cols", 0, 1, maxScreenSize); err != nil || q.Cols == 0 {
		return q, invalid("cols is required, from 1 to " + strconv.Itoa(maxScreenSize))
	}
	if q.Rows, err = intParam(r, "rows", 0, 1, maxScreenSize); err != nil || q.Rows == 0 {
		return q, invalid("rows is required, from 1 to " + strconv.Itoa(maxScreenSize))
	}
	wait, err := intParam(r, "wait", 0, 0, maxScreenWait)
	if err != nil {
		return q, err
	}
	q.Wait = time.Duration(wait) * time.Millisecond
	if raw := r.URL.Query().Get("resize"); raw != "" {
		if q.Resize, err = strconv.ParseBool(raw); err != nil {
			return q, invalid("resize must be true or false")
		}
	}
	q.Viewer = r.URL.Query().Get("viewer")
	if q.Viewer != "" && !viewerID.MatchString(q.Viewer) {
		return q, invalid("viewer must be up to 64 letters, digits, dots, dashes, or underscores")
	}
	return q, nil
}

func (s *Server) getScreen(w http.ResponseWriter, r *http.Request) error {
	t, err := need(s.cfg.Terminals, "terminals")
	if err != nil {
		return err
	}
	q, err := screenQuery(r)
	if err != nil {
		return err
	}
	screen, err := t.Screen(r.Context(), r.PathValue("name"), r.PathValue("address"), q)
	if err != nil {
		return fmt.Errorf("get screen: %w", err)
	}
	writeJSON(w, http.StatusOK, screen)
	return nil
}

func (s *Server) sendInput(w http.ResponseWriter, r *http.Request) error {
	t, err := need(s.cfg.Terminals, "terminals")
	if err != nil {
		return err
	}
	var in v1.Input
	if err := decode(r, &in); err != nil {
		return err
	}
	if len(in.Keys) > maxKeys {
		return invalid("too many keys in one request")
	}
	if err := t.Input(r.Context(), r.PathValue("name"), r.PathValue("address"), in.Keys); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
