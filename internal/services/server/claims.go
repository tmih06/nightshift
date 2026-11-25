package server

import (
	"net/http"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

const maxClaimWait = 120

func (s *Server) claimRoutes() []route {
	return []route{
		{method: "POST", pattern: "/v1/claims", handle: s.createClaim},
		{method: "GET", pattern: "/v1/claims/{name}", handle: s.getClaim},
		{method: "DELETE", pattern: "/v1/claims/{name}", handle: s.deleteClaim},
	}
}

func (s *Server) createClaim(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Claims, "claims")
	if err != nil {
		return err
	}
	var req v1.ClaimRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Pool == "" {
		return invalid("pool is required")
	}
	claim, err := c.Claim(r.Context(), req.Pool)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, claim)
	return nil
}

func (s *Server) getClaim(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Claims, "claims")
	if err != nil {
		return err
	}
	wait, err := intParam(r, "wait", 0, 0, maxClaimWait)
	if err != nil {
		return err
	}
	claim, err := c.Claimed(r.Context(), r.PathValue("name"), time.Duration(wait)*time.Second)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, claim)
	return nil
}

func (s *Server) deleteClaim(w http.ResponseWriter, r *http.Request) error {
	c, err := need(s.cfg.Claims, "claims")
	if err != nil {
		return err
	}
	if err := c.DeleteClaim(r.Context(), r.PathValue("name")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
