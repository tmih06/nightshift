package live

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/accounts"
)

// Accounts answers for the coding-agent accounts, over the accounts service.
type Accounts struct {
	svc accounts.Service
}

// wrapErr says what was being done when err happened; nil stays nil.
func wrapErr(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

func firstErr(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}

// accountState is the contract's name for an account's state.
func accountState(s accounts.State) string {
	switch s {
	case accounts.StateExpiring:
		return v1.AccountExpiring
	case accounts.StateFailing:
		return v1.AccountFailing
	case accounts.StateSignIn:
		return v1.AccountSignIn
	case accounts.StateDisabled:
		return v1.AccountDisabled
	case accounts.StateOK:
	}
	return v1.AccountOK
}

// Accounts reads every signed-in account, which one each agent uses, and what
// the egress proxy counted for each.
func (a *Accounts) Accounts(ctx context.Context) (v1.Accounts, error) {
	snap, err := a.svc.Fetch(ctx)
	if err != nil {
		return v1.Accounts{}, fmt.Errorf("accounts: %w", err)
	}
	out := v1.Accounts{
		ObservedAt:  snap.At.UTC(),
		Providers:   []v1.Provider{},
		Accounts:    make([]v1.Account, 0, len(snap.Accounts)),
		PublishedAt: snap.PublishedAt.UTC(),
	}
	for _, p := range accounts.Providers() {
		out.Providers = append(out.Providers, v1.Provider{ID: p.ID, Name: p.Name})
	}
	if snap.UsageErr != nil {
		out.UsageError = snap.UsageErr.Error()
	}
	if len(snap.Published) > 0 {
		out.Published = make(map[string]v1.Published, len(snap.Published))
		for id, p := range snap.Published {
			out.Published[id] = v1.Published{Account: p.Name, Label: p.Label, ExpiresAt: p.Expires.UTC()}
		}
	}
	now := time.Now()
	for i := range snap.Accounts {
		out.Accounts = append(out.Accounts, accountOf(&snap.Accounts[i], now))
	}
	return out, nil
}

func accountOf(a *accounts.Account, now time.Time) v1.Account {
	out := v1.Account{
		Name: a.Name, Provider: a.Provider, Email: a.Email,
		State: accountState(a.State(now)), Disabled: a.Disabled, Active: a.Active,
		ExpiresAt: a.Expires.UTC(), LastRefreshAt: a.LastRefresh.UTC(), NextRefreshAt: a.NextRefresh.UTC(),
		RefreshOK: a.Success, RefreshFailed: a.Failed, RefreshError: a.RefreshError,
		StatusNote: a.StatusNote, QuotaBlocked: a.QuotaBlocked, Cooldowns: a.Cooldowns,
	}
	if u := a.Usage; u != nil {
		out.Usage = &v1.AccountUsage{
			Requests: u.Requests, OK: u.OK, ClientError: u.ClientError,
			RateLimited: u.RateLimited, ServerError: u.ServerError,
			LastAt: u.Last.UTC(), Recent: u.Recent,
		}
	}
	return out
}

// StartLogin begins signing an account in.
func (a *Accounts) StartLogin(ctx context.Context, provider string) (v1.Login, error) {
	l, err := a.svc.StartLogin(ctx, provider)
	if err != nil {
		return v1.Login{}, fmt.Errorf("start login: %w", err)
	}
	return v1.Login{ID: l.State, Provider: l.Provider, URL: l.URL}, nil
}

// CompleteLogin finishes a sign-in with the address the browser ended on.
func (a *Accounts) CompleteLogin(ctx context.Context, id string, c v1.LoginCompletion) error {
	return wrapErr("complete the sign-in", a.svc.CompleteLogin(ctx, accounts.Login{Provider: c.Provider, State: id}, c.RedirectURL))
}

// CancelLogin abandons a sign-in.
func (a *Accounts) CancelLogin(ctx context.Context, id, provider string) error {
	return wrapErr("cancel the sign-in", a.svc.CancelLogin(ctx, accounts.Login{Provider: provider, State: id}))
}

// SetDisabled turns an account off or on.
func (a *Accounts) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return wrapErr("set the account on or off", a.svc.SetDisabled(ctx, name, disabled))
}

// Remove deletes an account.
func (a *Accounts) Remove(ctx context.Context, name string) error {
	return wrapErr("remove the account", a.svc.Remove(ctx, name))
}

// Refresh renews an account's token now.
func (a *Accounts) Refresh(ctx context.Context, name string) error {
	return wrapErr("refresh the account", a.svc.Refresh(ctx, name))
}

// SetActive picks the account a provider's new sandboxes get.
func (a *Accounts) SetActive(ctx context.Context, provider, name string) error {
	return wrapErr("choose the account", a.svc.SetActive(ctx, provider, name))
}

// Publish puts the chosen accounts into the cluster now.
func (a *Accounts) Publish(ctx context.Context) (string, error) {
	report, err := a.svc.PublishNow(ctx)
	return report, wrapErr("publish", err)
}
