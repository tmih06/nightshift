package server

import (
	"fmt"
	"net/http"
	"time"

	"charm.land/log/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// runKeepAlive is how often a quiet stream says it is still open: an agent can
// work for an hour without a change of state.
const runKeepAlive = 15 * time.Second

func (s *Server) runRoutes() []route {
	return []route{
		{method: http.MethodPost, pattern: "/v1/runs", handle: s.createRun},
		{method: http.MethodGet, pattern: "/v1/runs", handle: s.listRuns},
		{method: http.MethodGet, pattern: "/v1/runs/{id}", handle: s.getRun},
		{method: http.MethodDelete, pattern: "/v1/runs/{id}", handle: s.deleteRun},
		{method: http.MethodGet, pattern: "/v1/runs/{id}/events", handle: s.watchRun},
	}
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) error {
	runs, err := need(s.cfg.Runs, "runs")
	if err != nil {
		return err
	}
	var req v1.RunRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if why := req.Problem(); why != "" {
		return invalid(why)
	}
	run, err := runs.Create(req)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	writeJSON(w, http.StatusAccepted, run)
	return nil
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) error {
	runs, err := need(s.cfg.Runs, "runs")
	if err != nil {
		return err
	}
	list, err := runs.List(r.URL.Query().Get("externalRef"))
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	if list == nil {
		list = []v1.Run{}
	}
	writeJSON(w, http.StatusOK, v1.Runs{Runs: list})
	return nil
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) error {
	runs, err := need(s.cfg.Runs, "runs")
	if err != nil {
		return err
	}
	run, _, err := runs.Watch(r.PathValue("id"))
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}
	writeJSON(w, http.StatusOK, run)
	return nil
}

func (s *Server) deleteRun(w http.ResponseWriter, r *http.Request) error {
	runs, err := need(s.cfg.Runs, "runs")
	if err != nil {
		return err
	}
	if err := runs.Delete(r.PathValue("id")); err != nil {
		return fmt.Errorf("delete run: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// watchRun streams a run until it finishes. The stream is only opened once the
// run is known, so an unknown id is a plain 404.
func (s *Server) watchRun(w http.ResponseWriter, r *http.Request) error {
	runs, err := need(s.cfg.Runs, "runs")
	if err != nil {
		return err
	}
	run, changed, err := runs.Watch(r.PathValue("id"))
	if err != nil {
		return fmt.Errorf("watch run: %w", err)
	}
	out, err := newStream(w)
	if err != nil {
		return err
	}
	followRun(r, out, runs, run, changed)
	return nil
}

// followRun sends the run until it finishes or the client leaves.
func followRun(r *http.Request, out *stream, runs Runs, run v1.Run, changed <-chan struct{}) {
	id := run.ID
	for {
		if err := out.event("run", run); err != nil {
			log.Debug("the run's follower left", "run", id, "err", err)
			return
		}
		if run.Finished() || !awaitChange(r, out, changed) {
			return
		}
		var err error
		if run, changed, err = runs.Watch(id); err != nil {
			out.fail(err)
			return
		}
	}
}

// awaitChange keeps the stream open until changed closes, and says whether it
// did before the client left.
func awaitChange(r *http.Request, out *stream, changed <-chan struct{}) bool {
	tick := time.NewTicker(runKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-changed:
			return true
		case <-r.Context().Done():
			return false
		case <-tick.C:
			if err := out.keepAlive(); err != nil {
				return false
			}
		}
	}
}
