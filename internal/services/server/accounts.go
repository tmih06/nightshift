package server

import (
	"fmt"
	"net/http"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func (s *Server) accountRoutes() []route {
	return []route{
		{method: http.MethodGet, pattern: "/v1/accounts", handle: s.listAccounts},
		{method: http.MethodPost, pattern: "/v1/accounts/logins", handle: s.startLogin},
		{method: http.MethodPost, pattern: "/v1/accounts/logins/{id}/complete", handle: s.completeLogin},
		{method: http.MethodDelete, pattern: "/v1/accounts/logins/{id}", handle: s.cancelLogin},
		{method: http.MethodPost, pattern: "/v1/accounts/publish", handle: s.publishAccounts},
		{method: http.MethodPost, pattern: "/v1/accounts/refresh-quotas", handle: s.refreshQuotas},
		{method: http.MethodPut, pattern: "/v1/accounts/active/{provider}", handle: s.setActiveAccount},
		{method: http.MethodPut, pattern: "/v1/accounts/chain/{provider}", handle: s.setAccountChain},
		{method: http.MethodPatch, pattern: "/v1/accounts/{name}", handle: s.patchAccount},
		{method: http.MethodDelete, pattern: "/v1/accounts/{name}", handle: s.deleteAccount},
		{method: http.MethodPost, pattern: "/v1/accounts/{name}/refresh", handle: s.refreshAccount},
	}
}

// acct runs an accounts action that answers with no body.
func (s *Server) acct(w http.ResponseWriter, act func(Accounts) error) error {
	a, err := need(s.cfg.Accounts, "accounts")
	if err != nil {
		return err
	}
	if err := act(a); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) error {
	a, err := need(s.cfg.Accounts, "accounts")
	if err != nil {
		return err
	}
	snap, err := a.Accounts(r.Context())
	if err != nil {
		return fmt.Errorf("list accounts: %w", err)
	}
	writeJSON(w, http.StatusOK, snap)
	return nil
}

func (s *Server) startLogin(w http.ResponseWriter, r *http.Request) error {
	a, err := need(s.cfg.Accounts, "accounts")
	if err != nil {
		return err
	}
	var req v1.LoginRequest
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Provider == "" {
		return invalid("provider is required")
	}
	login, err := a.StartLogin(r.Context(), req.Provider)
	if err != nil {
		return fmt.Errorf("start login: %w", err)
	}
	writeJSON(w, http.StatusCreated, login)
	return nil
}

func (s *Server) completeLogin(w http.ResponseWriter, r *http.Request) error {
	var c v1.LoginCompletion
	if err := decode(r, &c); err != nil {
		return err
	}
	if c.Provider == "" || c.RedirectURL == "" {
		return invalid("provider and redirectUrl are required")
	}
	return s.acct(w, func(a Accounts) error { return a.CompleteLogin(r.Context(), r.PathValue("id"), c) })
}

func (s *Server) cancelLogin(w http.ResponseWriter, r *http.Request) error {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		return invalid("provider is required")
	}
	return s.acct(w, func(a Accounts) error { return a.CancelLogin(r.Context(), r.PathValue("id"), provider) })
}

func (s *Server) publishAccounts(w http.ResponseWriter, r *http.Request) error {
	a, err := need(s.cfg.Accounts, "accounts")
	if err != nil {
		return err
	}
	report, err := a.Publish(r.Context())
	if err != nil {
		return fmt.Errorf("publish accounts: %w", err)
	}
	writeJSON(w, http.StatusOK, v1.PublishReport{Report: report})
	return nil
}

func (s *Server) refreshQuotas(w http.ResponseWriter, r *http.Request) error {
	return s.acct(w, func(a Accounts) error { return a.RefreshQuotas(r.Context()) })
}

func (s *Server) setActiveAccount(w http.ResponseWriter, r *http.Request) error {
	var req v1.ActiveAccount
	if err := decode(r, &req); err != nil {
		return err
	}
	if req.Account == "" {
		return invalid("account is required")
	}
	return s.acct(w, func(a Accounts) error { return a.SetActive(r.Context(), r.PathValue("provider"), req.Account) })
}

func (s *Server) setAccountChain(w http.ResponseWriter, r *http.Request) error {
	var req v1.AccountChain
	if err := decode(r, &req); err != nil {
		return err
	}
	if len(req.Accounts) == 0 {
		return invalid("accounts is required")
	}
	return s.acct(w, func(a Accounts) error { return a.SetChain(r.Context(), r.PathValue("provider"), req.Accounts) })
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) error {
	var p v1.AccountPatch
	if err := decode(r, &p); err != nil {
		return err
	}
	if p.Disabled == nil {
		return invalid("nothing to change: send disabled")
	}
	return s.acct(w, func(a Accounts) error { return a.SetDisabled(r.Context(), r.PathValue("name"), *p.Disabled) })
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	return s.acct(w, func(a Accounts) error { return a.Remove(r.Context(), r.PathValue("name")) })
}

func (s *Server) refreshAccount(w http.ResponseWriter, r *http.Request) error {
	return s.acct(w, func(a Accounts) error { return a.Refresh(r.Context(), r.PathValue("name")) })
}
