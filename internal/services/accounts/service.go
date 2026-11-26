package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/log/v2"
)

// Service is what the Accounts tab needs from the outside world, so the tab
// can be tested without a cluster.
type Service interface {
	Fetch(ctx context.Context) (Snapshot, error)
	StartLogin(ctx context.Context, provider string) (Login, error)
	CompleteLogin(ctx context.Context, l Login, redirect string) error
	CancelLogin(ctx context.Context, l Login) error
	SetDisabled(ctx context.Context, name string, disabled bool) error
	Remove(ctx context.Context, name string) error
	Refresh(ctx context.Context, name string) error
	SetActive(ctx context.Context, provider, name string) error
	SetChain(ctx context.Context, provider string, names []string) error
	PublishNow(ctx context.Context) (string, error)
	RefreshQuotas(ctx context.Context) error
	Close()
}

// Live is the Service backed by a cluster: CLIProxyAPI behind a port-forward,
// the egress proxy's admin port behind another, and kubectl for the rest.
type Live struct {
	CPA   Endpoint
	Proxy Endpoint
	Kube  Kube
}

// NewLive returns the Service for the cluster kubectl points at.
func NewLive() *Live {
	return &Live{
		CPA:   &Forward{Namespace: "nightshift", Target: "svc/cpa", Port: 8317},
		Proxy: &Forward{Namespace: "nightshift-egress", Target: "deploy/egress-proxy", Port: 9090},
	}
}

// Close ends the port-forwards.
func (l *Live) Close() {
	l.CPA.Close()
	l.Proxy.Close()
}

func (l *Live) client(ctx context.Context) (*Client, error) {
	base, err := l.CPA.URL(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach CLIProxyAPI: %w", err)
	}
	key, err := l.Kube.ManagementKey(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(base, key, nil), nil
}

// Fetch gathers the accounts, which one is active, and the proxy's usage. A
// failure to read usage does not hide the accounts.
func (l *Live) Fetch(ctx context.Context) (Snapshot, error) {
	c, err := l.client(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	accounts, err := c.List(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := l.Kube.ReadAccounts(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{
		At:          time.Now(),
		Accounts:    accounts,
		Chains:      map[string][]string{},
		PublishedAt: data.PublishedAt,
		Published:   data.Published,
	}
	for _, p := range Providers() {
		chain := Chain(accounts, p.ID, data.Chains[p.ID], data.Active[p.ID])
		if len(chain) == 0 {
			continue
		}
		snap.Chains[p.ID] = chain
		rank := make(map[string]int, len(chain))
		for idx, name := range chain {
			rank[name] = idx + 1
		}
		for i := range snap.Accounts {
			if snap.Accounts[i].Provider != p.ID {
				continue
			}
			if pos, ok := rank[snap.Accounts[i].Name]; ok {
				snap.Accounts[i].Priority = pos
				snap.Accounts[i].Active = pos == 1
			}
		}
	}
	snap.Analytics, snap.UsageErr = l.attachUsage(ctx, snap.Accounts)
	return snap, nil
}

func (l *Live) attachUsage(ctx context.Context, accounts []Account) (*Analytics, error) {
	base, err := l.Proxy.URL(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach the egress proxy: %w", err)
	}
	usage, err := fetchUsage(ctx, base)
	if err != nil {
		return nil, err
	}
	pools, perr := fetchPools(ctx, base)
	if perr != nil {
		log.Debug("accounts: fetch proxy pools", "err", perr)
	}
	quotas, qerr := fetchQuotas(ctx, base)
	if qerr != nil {
		log.Debug("accounts: fetch proxy quotas", "err", qerr)
	}
	history, herr := fetchHistory(ctx, base)
	if herr != nil {
		log.Debug("accounts: fetch proxy history", "err", herr)
	}
	for i := range accounts {
		p, ok := ProviderByID(accounts[i].Provider)
		if !ok {
			continue
		}
		applyAccountProxyData(&accounts[i], p, usage, pools, quotas)
		applyAccountHistory(&accounts[i], p, history)
	}
	return summarizeAnalytics(usage), nil
}

func applyAccountProxyData(acct *Account, p Provider, usage []wireUsage, pools []wirePoolStatus, quotas []wireQuota) {
	lbl := acct.Label()
	applyAccountUsage(acct, p.Secret, lbl, usage)
	applyAccountPool(acct, p.Secret, lbl, pools)
	applyAccountQuota(acct, p.Secret, lbl, quotas)
}

func applyAccountUsage(acct *Account, secret, lbl string, usage []wireUsage) {
	for i := range usage {
		if usage[i].Binding == secret && usage[i].Label == lbl {
			acct.Usage = usage[i].usage()
		}
	}
}

func applyAccountPool(acct *Account, secret, lbl string, pools []wirePoolStatus) {
	for i := range pools {
		ps := &pools[i]
		if ps.Binding == secret && matchesProxyAccount(ps.Name, ps.Label, acct.Name, secret, lbl) {
			acct.ActiveNow = ps.ActiveNow
			acct.CooldownUntil = ps.CooldownUntil
			acct.CooldownReason = ps.CooldownReason
			acct.Failovers = ps.Failovers
		}
	}
}

func applyAccountQuota(acct *Account, secret, lbl string, quotas []wireQuota) {
	for i := range quotas {
		q := &quotas[i]
		if q.Binding == secret && matchesProxyAccount(q.Name, q.Label, acct.Name, secret, lbl) {
			acct.Quota = q.quota()
		}
	}
}

func matchesProxyAccount(proxyName, proxyLabel, acctName, secret, acctLabel string) bool {
	return proxyName == acctName || (proxyName == secret && proxyLabel == acctLabel)
}

// StartLogin begins signing an account in.
func (l *Live) StartLogin(ctx context.Context, provider string) (Login, error) {
	c, err := l.client(ctx)
	if err != nil {
		return Login{}, err
	}
	return c.StartLogin(ctx, provider)
}

// CompleteLogin finishes a sign-in with the address the browser ended on.
func (l *Live) CompleteLogin(ctx context.Context, lg Login, redirect string) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.Complete(ctx, lg, redirect)
}

// CancelLogin abandons a sign-in.
func (l *Live) CancelLogin(ctx context.Context, lg Login) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.Cancel(ctx, lg)
}

// SetDisabled turns an account off or on.
func (l *Live) SetDisabled(ctx context.Context, name string, disabled bool) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.SetDisabled(ctx, name, disabled)
}

// Remove deletes an account.
func (l *Live) Remove(ctx context.Context, name string) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.Remove(ctx, name)
}

// Refresh renews an account's token now.
func (l *Live) Refresh(ctx context.Context, name string) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.Refresh(ctx, name)
}

// SetActive chooses which account leads an agent's fallback chain.
func (l *Live) SetActive(ctx context.Context, provider, name string) error {
	if _, ok := ProviderByID(provider); !ok {
		return errors.New("unknown agent " + provider)
	}
	return l.Kube.SetActive(ctx, provider, name)
}

// SetChain sets the ordered fallback chain for an agent's accounts.
func (l *Live) SetChain(ctx context.Context, provider string, names []string) error {
	if _, ok := ProviderByID(provider); !ok {
		return errors.New("unknown agent " + provider)
	}
	return l.Kube.SetChain(ctx, provider, names)
}

// PublishNow has the refresher publish immediately.
func (l *Live) PublishNow(ctx context.Context) (string, error) { return l.Kube.PublishNow(ctx) }

// RefreshQuotas asks the egress proxy to actively fetch provider quotas now.
func (l *Live) RefreshQuotas(ctx context.Context) error {
	base, err := l.Proxy.URL(ctx)
	if err != nil {
		return fmt.Errorf("reach the egress proxy: %w", err)
	}
	return triggerQuotaRefresh(ctx, base)
}
