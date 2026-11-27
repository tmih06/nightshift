package accounts

import (
	"context"
	"fmt"
	"time"

	"charm.land/log/v2"
)

// Service is what the Accounts tab needs from the outside world, so the tab
// can be tested without a cluster.
type Service interface {
	Fetch(ctx context.Context) (Snapshot, error)
	StartLogin(ctx context.Context, provider, method string) (Login, error)
	CompleteLogin(ctx context.Context, l Login, input string) (Progress, error)
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

// Live is the Service backed by a cluster: nightshift-auth and the egress
// proxy's admin port, each behind a port-forward unless reached directly, and
// kubectl for nightshift-auth's key.
type Live struct {
	Auth  Endpoint
	Proxy Endpoint
	Kube  Kube
}

// NewLive returns the Service for the cluster kubectl points at.
func NewLive() *Live {
	return &Live{
		Auth:  &Forward{Namespace: "nightshift", Target: "svc/nightshift-auth", Port: 8318},
		Proxy: &Forward{Namespace: "nightshift-egress", Target: "deploy/egress-proxy", Port: 9090},
	}
}

// Close ends the port-forwards.
func (l *Live) Close() {
	l.Auth.Close()
	l.Proxy.Close()
}

func (l *Live) client(ctx context.Context) (*Client, error) {
	base, err := l.Auth.URL(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach nightshift-auth: %w", err)
	}
	key, err := l.Kube.AuthKey(ctx)
	if err != nil {
		return nil, err
	}
	return NewClient(base, key, nil), nil
}

// Fetch gathers the accounts, each agent's chain, and the proxy's usage. A
// failure to read usage does not hide the accounts.
func (l *Live) Fetch(ctx context.Context) (Snapshot, error) {
	c, err := l.client(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	provs, err := c.Providers(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	v, err := c.Vault(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{
		At:          time.Now(),
		Providers:   provs,
		Accounts:    v.Accounts,
		Chains:      v.Chains,
		PublishedAt: v.PublishedAt,
		Published:   v.Published,
	}
	rankAccounts(snap.Accounts, snap.Chains)
	snap.Analytics, snap.UsageErr = l.attachUsage(ctx, provs, snap.Accounts)
	return snap, nil
}

// rankAccounts marks each account's place in its agent's chain.
func rankAccounts(accounts []Account, chains map[string][]string) {
	for i := range accounts {
		a := &accounts[i]
		for idx, name := range chains[a.Provider] {
			if name == a.Name {
				a.Priority, a.Active = idx+1, idx == 0
			}
		}
	}
}

func (l *Live) attachUsage(ctx context.Context, provs []Provider, accounts []Account) (*Analytics, error) {
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
		p, ok := providerByID(provs, accounts[i].Provider)
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

// StartLogin begins signing an account in by method.
func (l *Live) StartLogin(ctx context.Context, provider, method string) (Login, error) {
	c, err := l.client(ctx)
	if err != nil {
		return Login{}, err
	}
	return c.StartLogin(ctx, provider, method)
}

// CompleteLogin finishes a sign-in with what the person pasted, or asks after
// a device sign-in.
func (l *Live) CompleteLogin(ctx context.Context, lg Login, input string) (Progress, error) {
	c, err := l.client(ctx)
	if err != nil {
		return Progress{}, err
	}
	return c.Complete(ctx, lg, input)
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

// SetActive moves name to the head of its agent's fallback chain.
func (l *Live) SetActive(ctx context.Context, provider, name string) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	v, err := c.Vault(ctx)
	if err != nil {
		return err
	}
	chain := []string{name}
	for _, n := range v.Chains[provider] {
		if n != name {
			chain = append(chain, n)
		}
	}
	return c.SetChain(ctx, provider, chain)
}

// SetChain sets the ordered fallback chain for an agent's accounts.
func (l *Live) SetChain(ctx context.Context, provider string, names []string) error {
	c, err := l.client(ctx)
	if err != nil {
		return err
	}
	return c.SetChain(ctx, provider, names)
}

// PublishNow has nightshift-auth publish immediately.
func (l *Live) PublishNow(ctx context.Context) (string, error) {
	c, err := l.client(ctx)
	if err != nil {
		return "", err
	}
	return c.Publish(ctx)
}

// RefreshQuotas asks the egress proxy to actively fetch provider quotas now.
func (l *Live) RefreshQuotas(ctx context.Context) error {
	base, err := l.Proxy.URL(ctx)
	if err != nil {
		return fmt.Errorf("reach the egress proxy: %w", err)
	}
	return triggerQuotaRefresh(ctx, base)
}
