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
	if len(snap.Chains) > 0 {
		out.Chains = make(map[string][]string, len(snap.Chains))
		for id, ch := range snap.Chains {
			out.Chains[id] = append([]string(nil), ch...)
		}
	}
	if snap.Analytics != nil {
		out.Analytics = analyticsOf(snap.Analytics)
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
		Priority: a.Priority, ActiveNow: a.ActiveNow,
		CooldownUntil: a.CooldownUntil.UTC(), CooldownReason: a.CooldownReason, Failovers: a.Failovers,
		ExpiresAt: a.Expires.UTC(), LastRefreshAt: a.LastRefresh.UTC(), NextRefreshAt: a.NextRefresh.UTC(),
		RefreshOK: a.Success, RefreshFailed: a.Failed, RefreshError: a.RefreshError,
		StatusNote: a.StatusNote, QuotaBlocked: a.QuotaBlocked, Cooldowns: a.Cooldowns,
	}
	if u := a.Usage; u != nil {
		out.Usage = usageOf(u)
	}
	for _, d := range a.History {
		out.History = append(out.History, v1.DayUsage(d))
	}
	if q := a.Quota; q != nil {
		out.Quota = quotaOf(q)
	}
	return out
}

func usageOf(u *accounts.Usage) *v1.AccountUsage {
	out := &v1.AccountUsage{
		Requests: u.Requests, OK: u.OK, ClientError: u.ClientError,
		RateLimited: u.RateLimited, ServerError: u.ServerError,
		Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
		CacheWrite: u.CacheWrite, Reasoning: u.Reasoning, Total: u.Total,
		CacheHitPercent: u.CacheHitPercent, AvgLatencyMS: u.AvgLatencyMS,
		LastAt: u.Last.UTC(), Recent: u.Recent, RecentVolume: u.RecentVolume,
	}
	out.ByModel = modelsOf(u.ByModel)
	return out
}

func quotaOf(q *accounts.Quota) *v1.AccountQuota {
	out := &v1.AccountQuota{
		Plan:           q.Plan,
		CreditsBalance: q.CreditsBalance,
		Source:         q.Source,
		FetchedAt:      q.FetchedAt.UTC(),
		NextResetAt:    q.NextResetAt.UTC(),
		Error:          q.Error,
		ModelsError:    q.ModelsError,
	}
	for _, m := range q.Models {
		out.Models = append(out.Models, v1.AccountModel(m))
	}
	if len(q.Windows) > 0 {
		out.Windows = make([]v1.QuotaWindow, len(q.Windows))
		for i, w := range q.Windows {
			out.Windows[i] = v1.QuotaWindow{
				Name: w.Name, Label: w.Label, Group: w.Group, UsedPercent: w.UsedPercent,
				Limit: w.Limit, Used: w.Used, Remaining: w.Remaining,
				ResetsAt: w.ResetsAt.UTC(), WindowMinutes: w.WindowMinutes, Status: w.Status,
			}
		}
	}
	return out
}

func analyticsOf(a *accounts.Analytics) *v1.AnalyticsSummary {
	return &v1.AnalyticsSummary{
		Requests: a.Requests, OK: a.OK, ClientError: a.ClientError,
		RateLimited: a.RateLimited, ServerError: a.ServerError,
		Input: a.Input, Output: a.Output, CacheRead: a.CacheRead,
		CacheWrite: a.CacheWrite, Reasoning: a.Reasoning, Total: a.Total,
		CacheHitPercent: a.CacheHitPercent, AvgLatencyMS: a.AvgLatencyMS,
		ByModel: modelsOf(a.ByModel),
	}
}

func modelsOf(src map[string]accounts.ModelUsage) map[string]v1.ModelUsage {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]v1.ModelUsage, len(src))
	for k, v := range src {
		out[k] = v1.ModelUsage(v)
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

// SetActive picks the primary account at the head of a provider's chain.
func (a *Accounts) SetActive(ctx context.Context, provider, name string) error {
	return wrapErr("choose the account", a.svc.SetActive(ctx, provider, name))
}

// SetChain sets the ordered fallback chain for a provider's accounts.
func (a *Accounts) SetChain(ctx context.Context, provider string, names []string) error {
	return wrapErr("set the fallback chain", a.svc.SetChain(ctx, provider, names))
}

// Publish puts the chosen accounts into the cluster now.
func (a *Accounts) Publish(ctx context.Context) (string, error) {
	report, err := a.svc.PublishNow(ctx)
	return report, wrapErr("publish", err)
}

// RefreshQuotas triggers an active provider quota fetch on the egress proxy.
func (a *Accounts) RefreshQuotas(ctx context.Context) error {
	return wrapErr("refresh quotas", a.svc.RefreshQuotas(ctx))
}
