package server

import (
	"fmt"
	"net/http"
	"strconv"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func (s *Server) checkoutRoutes() []route {
	return []route{
		{method: http.MethodPost, pattern: "/v1/checkouts", handle: s.createCheckout},
		{method: http.MethodGet, pattern: "/v1/checkouts/{id}", handle: s.getCheckout},
		{method: http.MethodGet, pattern: "/v1/checkouts/{id}/events", handle: s.watchCheckout},
		{method: http.MethodGet, pattern: "/v1/volumes", handle: s.listVolumes},
		{method: http.MethodDelete, pattern: "/v1/volumes/{name}", handle: s.deleteVolume},
		{method: http.MethodDelete, pattern: "/v1/sandboxes/{name}", handle: s.deleteSandbox},
	}
}

func (s *Server) createCheckout(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Checkouts, "checkouts")
	if err != nil {
		return err
	}
	var req v1.CheckoutRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if why := req.Problem(); why != "" {
		return invalid(why)
	}
	co, err := c.Start(req)
	if err != nil {
		return fmt.Errorf("create checkout: %w", err)
	}
	writeJSON(w, http.StatusAccepted, co)
	return nil
}

func (s *Server) getCheckout(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Checkouts, "checkouts")
	if err != nil {
		return err
	}
	co, err := c.Get(r.PathValue("id"))
	if err != nil {
		return fmt.Errorf("get checkout: %w", err)
	}
	writeJSON(w, http.StatusOK, co)
	return nil
}

// watchCheckout streams a checkout's events until it ends. The stream is only
// opened once the checkout is known, so an unknown id is a plain 404.
func (s *Server) watchCheckout(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Checkouts, "checkouts")
	if err != nil {
		return err
	}
	id := r.PathValue("id")
	if _, err := c.Get(id); err != nil {
		return fmt.Errorf("watch checkout: %w", err)
	}
	out, err := newStream(w)
	if err != nil {
		return err
	}
	err = c.Follow(r.Context(), id, func(ev v1.CheckoutEvent) error { return out.event("checkout", ev) })
	if err != nil && r.Context().Err() == nil {
		out.fail(err)
	}
	return nil
}

func (s *Server) listVolumes(w http.ResponseWriter, r *http.Request) error {
	v, err := need(s.cfg.Volumes, "volumes")
	if err != nil {
		return err
	}
	vols, err := v.Volumes(r.Context())
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}
	if vols == nil {
		vols = []v1.Volume{}
	}
	writeJSON(w, http.StatusOK, v1.Volumes{Volumes: vols})
	return nil
}

func (s *Server) deleteVolume(w http.ResponseWriter, r *http.Request) error {
	v, err := need(s.cfg.Volumes, "volumes")
	if err != nil {
		return err
	}
	if err := v.DeleteVolume(r.Context(), r.PathValue("name")); err != nil {
		return fmt.Errorf("delete volume: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) deleteSandbox(w http.ResponseWriter, r *http.Request) error {
	v, err := need(s.cfg.Volumes, "volumes")
	if err != nil {
		return err
	}
	drop := false
	if raw := r.URL.Query().Get("deleteVolume"); raw != "" {
		if drop, err = strconv.ParseBool(raw); err != nil {
			return invalid("deleteVolume must be true or false")
		}
	}
	if err := v.DeleteSandbox(r.Context(), r.PathValue("name"), drop); err != nil {
		return fmt.Errorf("delete sandbox: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
