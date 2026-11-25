package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func (s *Server) fleetRoutes() []route {
	return []route{
		{method: "GET", pattern: "/v1/fleet", handle: s.getFleet},
		{method: "GET", pattern: "/v1/fleet/watch", handle: s.watchFleet},
		{method: "GET", pattern: "/v1/pods/{namespace}/{name}/logs", handle: s.podLogs},
	}
}

func parseInclude(r *http.Request) (Include, error) {
	var inc Include
	raw := r.URL.Query().Get("include")
	if raw == "" {
		return inc, nil
	}
	for _, part := range strings.Split(raw, ",") {
		switch strings.TrimSpace(part) {
		case "configs":
			inc.Configs = true
		case "live":
			inc.Live = true
		case "versions":
			inc.Versions = true
		default:
			return inc, invalid("include: unknown value " + strconv.Quote(part))
		}
	}
	return inc, nil
}

func (s *Server) getFleet(w http.ResponseWriter, r *http.Request) error {
	f, err := need(s.cfg.Fleet, "the fleet")
	if err != nil {
		return err
	}
	inc, err := parseInclude(r)
	if err != nil {
		return err
	}
	fleet, err := f.Fleet(r.Context(), inc)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, fleet)
	return nil
}

// watchFleet sends the fleet at once and again each time it changes. It looks
// for changes itself, so a frontend does not have to poll.
func (s *Server) watchFleet(w http.ResponseWriter, r *http.Request) error {
	f, err := need(s.cfg.Fleet, "the fleet")
	if err != nil {
		return err
	}
	inc, err := parseInclude(r)
	if err != nil {
		return err
	}
	out, err := newStream(w)
	if err != nil {
		return err
	}
	tick := time.NewTicker(s.cfg.WatchInterval)
	defer tick.Stop()
	var sent []byte
	quiet := 0
	for {
		fleet, err := f.Fleet(r.Context(), inc)
		if err != nil {
			ae := classify(err)
			_ = out.event("error", v1.Problem{Type: "about:blank", Title: titles[ae.code], Status: ae.status, Detail: ae.detail, Code: ae.code})
			return nil
		}
		fingerprint := fleetFingerprint(fleet)
		quiet++
		if !bytes.Equal(fingerprint, sent) {
			sent, quiet = fingerprint, 0
			if err := out.event("fleet", fleet); err != nil {
				return nil
			}
		} else if quiet%8 == 0 && out.keepAlive() != nil {
			return nil
		}
		select {
		case <-r.Context().Done():
			return nil
		case <-tick.C:
		}
	}
}

// fleetFingerprint is the fleet without the moment it was looked at.
func fleetFingerprint(f v1.Fleet) []byte {
	f.ObservedAt = time.Time{}
	b, err := json.Marshal(f)
	if err != nil {
		return nil
	}
	return b
}

const (
	defaultTail = 200
	maxTail     = 5000
)

func (s *Server) podLogs(w http.ResponseWriter, r *http.Request) error {
	f, err := need(s.cfg.Fleet, "the fleet")
	if err != nil {
		return err
	}
	tail, err := intParam(r, "tail", defaultTail, 1, maxTail)
	if err != nil {
		return err
	}
	ns, pod := r.PathValue("namespace"), r.PathValue("name")
	lines, err := f.PodLogs(r.Context(), ns, pod, tail)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, v1.Logs{Pod: pod, Lines: lines})
	return nil
}

// intParam reads an optional integer query parameter within [lo, hi].
func intParam(r *http.Request, name string, def, lo, hi int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return 0, invalid(name + " must be a whole number from " + strconv.Itoa(lo) + " to " + strconv.Itoa(hi))
	}
	return n, nil
}
