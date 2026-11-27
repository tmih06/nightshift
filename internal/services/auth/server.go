package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"charm.land/log/v2"
)

// The wire types of nightshift-auth's API. Only nightshiftd calls it.

// ProviderJSON is one agent.
type ProviderJSON struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

// AccountJSON is one account, without its tokens.
type AccountJSON struct {
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Email        string    `json:"email,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	Disabled     bool      `json:"disabled,omitempty"`
	NeedsSignIn  bool      `json:"needsSignIn,omitempty"`
	Expires      time.Time `json:"expires,omitzero"`
	RefreshedAt  time.Time `json:"refreshedAt,omitzero"`
	NextRefresh  time.Time `json:"nextRefresh,omitzero"`
	RefreshError string    `json:"refreshError,omitempty"`
	Refreshes    int       `json:"refreshes,omitempty"`
	Failures     int       `json:"failures,omitempty"`
}

// AccountsJSON is GET /v1/accounts.
type AccountsJSON struct {
	Accounts  []AccountJSON       `json:"accounts"`
	Chains    map[string][]string `json:"chains,omitempty"`
	Published Published           `json:"published"`
}

// LoginJSON is a started sign-in.
type LoginJSON struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

// Handler serves the API, every route but /healthz behind key.
func (s *Service) Handler(key string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/providers", s.handleProviders)
	mux.HandleFunc("GET /v1/accounts", s.handleAccounts)
	mux.HandleFunc("POST /v1/logins", s.handleStartLogin)
	mux.HandleFunc("POST /v1/logins/{id}", s.handleCompleteLogin)
	mux.HandleFunc("DELETE /v1/logins/{id}", s.handleCancelLogin)
	mux.HandleFunc("PATCH /v1/accounts/{name}", s.handleSetDisabled)
	mux.HandleFunc("DELETE /v1/accounts/{name}", s.handleRemove)
	mux.HandleFunc("POST /v1/accounts/{name}/refresh", s.handleRefresh)
	mux.HandleFunc("PUT /v1/chains/{provider}", s.handleSetChain)
	mux.HandleFunc("POST /v1/publish", s.handlePublish)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("missing or wrong key"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) handleProviders(w http.ResponseWriter, _ *http.Request) {
	list := s.Providers()
	out := make([]ProviderJSON, 0, len(list))
	for _, p := range list {
		out = append(out, ProviderJSON{ID: p.ID, Name: p.Name, Secret: p.Secret})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) handleAccounts(w http.ResponseWriter, _ *http.Request) {
	v := s.View()
	out := AccountsJSON{Accounts: []AccountJSON{}, Chains: v.Chains, Published: v.Published}
	for i := range v.Accounts {
		a := &v.Accounts[i]
		out.Accounts = append(out.Accounts, AccountJSON{
			Name: a.Name, Provider: a.Provider, Email: a.Credential.Email, Plan: a.Credential.Plan,
			Disabled: a.Disabled, NeedsSignIn: a.NeedsSignIn, Expires: a.Credential.Expires,
			RefreshedAt: a.RefreshedAt, NextRefresh: s.nextRefresh(a), RefreshError: a.RefreshError,
			Refreshes: a.Refreshes, Failures: a.Failures,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// nextRefresh is when the loop will renew a, or zero when it will not.
func (s *Service) nextRefresh(a *Account) time.Time {
	if a.Disabled || a.NeedsSignIn || a.Credential.Expires.IsZero() {
		return time.Time{}
	}
	return a.Credential.Expires.Add(-s.cfg.RefreshAhead)
}

func (s *Service) handleStartLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	pend, id, err := s.StartLogin(req.Provider)
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, LoginJSON{ID: id, Provider: pend.Provider, URL: pend.URL})
}

func (s *Service) handleCompleteLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input string `json:"input"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	name, err := s.CompleteLogin(r.Context(), r.PathValue("id"), req.Input)
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"account": name})
}

func (s *Service) handleCancelLogin(w http.ResponseWriter, r *http.Request) {
	s.CancelLogin(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleSetDisabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	done(w, s.SetDisabled(r.Context(), r.PathValue("name"), req.Disabled))
}

func (s *Service) handleRemove(w http.ResponseWriter, r *http.Request) {
	done(w, s.Remove(r.Context(), r.PathValue("name")))
}

func (s *Service) handleRefresh(w http.ResponseWriter, r *http.Request) {
	done(w, s.Refresh(r.Context(), r.PathValue("name")))
}

func (s *Service) handleSetChain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Accounts []string `json:"accounts"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	done(w, s.SetChain(r.Context(), r.PathValue("provider"), req.Accounts))
}

func (s *Service) handlePublish(w http.ResponseWriter, r *http.Request) {
	report, err := s.Publish(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"report": report})
}

// statusOf is the HTTP status for a Service error.
func statusOf(err error) int {
	switch {
	case errors.Is(err, ErrUnknownProvider), errors.Is(err, ErrUnknownAccount), errors.Is(err, ErrUnknownLogin):
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}

func done(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, statusOf(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("the request body is not JSON"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debug("nightshift-auth: write reply", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
