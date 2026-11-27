// Package auth is nightshift-auth: it signs coding-agent accounts in, keeps
// their tokens fresh, and publishes them to the cluster. Real tokens go only
// to the egress proxy (egress-secrets); sandboxes get logins holding
// placeholders (agent-creds); and each agent's warm pool is kept only while it
// has an account. It is the only process that spends a refresh token.
//
// The agents are internal/services/auth/providers; adding one changes
// nothing here.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/log/v2"

	"github.com/tmih06/nightshift/internal/services/auth/providers"
)

// Errors the API maps to statuses.
var (
	ErrUnknownProvider = errors.New("unknown agent")
	ErrUnknownAccount  = errors.New("no such account")
	ErrUnknownLogin    = errors.New("no such sign-in; it may have timed out, so start again")
	ErrLoginBusy       = errors.New("this sign-in is already being finished")
)

// retryAfter is how long a failed renewal waits before the next try.
const retryAfter = 5 * time.Minute

// Config is how a Service runs.
type Config struct {
	Namespaces Namespaces
	// RefreshAhead renews a token this long before it lapses.
	RefreshAhead time.Duration
	// PublishEvery publishes this often even when nothing changed, so a
	// warm pool or secret changed by hand is put back.
	PublishEvery time.Duration
	// LoginTTL is how long a started sign-in waits to be finished.
	LoginTTL time.Duration
}

func (c *Config) defaults() {
	if c.RefreshAhead == 0 {
		c.RefreshAhead = 10 * time.Minute
	}
	if c.PublishEvery == 0 {
		c.PublishEvery = 5 * time.Minute
	}
	if c.LoginTTL == 0 {
		c.LoginTTL = 15 * time.Minute
	}
}

type pendingLogin struct {
	providers.Pending
	started time.Time
	// busy is set while a Finish runs, so two answers to one sign-in cannot
	// both spend its code.
	busy bool
}

// Service owns the vault. Every change goes through one lock, so no two
// renewals of an account can race and spend its refresh token twice.
type Service struct {
	mu        sync.Mutex
	store     *Store
	state     State
	providers []providers.Provider
	cluster   Cluster
	cfg       Config
	logins    map[string]pendingLogin
	dirty     bool
}

// New loads the vault and serves list's agents.
func New(store *Store, list []providers.Provider, cluster Cluster, cfg Config) (*Service, error) {
	cfg.defaults()
	st, err := store.Load()
	if err != nil {
		return nil, err
	}
	return &Service{store: store, state: st, providers: list, cluster: cluster, cfg: cfg, logins: map[string]pendingLogin{}, dirty: true}, nil
}

// Providers lists the agents served.
func (s *Service) Providers() []providers.Info {
	out := make([]providers.Info, 0, len(s.providers))
	for _, p := range s.providers {
		out = append(out, p.Info())
	}
	return out
}

// View is the vault without a token in it.
type View struct {
	Accounts  []Account
	Chains    map[string][]string
	Published Published
}

// View reads the accounts with every token blanked.
func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := View{Chains: map[string][]string{}, Published: s.state.Published}
	for i := range s.state.Accounts {
		a := s.state.Accounts[i]
		a.Credential.Access, a.Credential.Refresh = "", ""
		v.Accounts = append(v.Accounts, a)
	}
	for _, p := range s.providers {
		id := p.Info().ID
		if chain := Chain(s.state.Accounts, id, s.state.Chains[id]); len(chain) > 0 {
			v.Chains[id] = chain
		}
	}
	return v
}

// StartLogin begins signing an account in to provider by method, its usual
// method when method is empty.
func (s *Service) StartLogin(ctx context.Context, provider, method string) (providers.Pending, string, error) {
	p, ok := providers.ByID(s.providers, provider)
	if !ok {
		return providers.Pending{}, "", fmt.Errorf("%w %q", ErrUnknownProvider, provider)
	}
	pend, err := p.Begin(ctx, method)
	if err != nil {
		return providers.Pending{}, "", fmt.Errorf("start the %s sign-in: %w", provider, err)
	}
	id, err := newID()
	if err != nil {
		return providers.Pending{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k := range s.logins {
		if now.Sub(s.logins[k].started) > s.cfg.LoginTTL {
			delete(s.logins, k)
		}
	}
	s.logins[id] = pendingLogin{Pending: pend, started: now}
	return pend, id, nil
}

// CompleteLogin finishes sign-in id with what the person pasted, saves the
// account, and publishes. Signing the same account in again replaces its
// tokens. A device sign-in takes no input and answers providers.ErrPending
// until it is approved. A failed answer leaves the sign-in open to try again.
func (s *Service) CompleteLogin(ctx context.Context, id, input string) (string, error) {
	l, err := s.claimLogin(id)
	if err != nil {
		return "", err
	}
	p, ok := providers.ByID(s.providers, l.Provider)
	if !ok {
		s.releaseLogin(id, false)
		return "", fmt.Errorf("%w %q", ErrUnknownProvider, l.Provider)
	}
	cred, err := p.Finish(ctx, l.Pending, input)
	s.releaseLogin(id, err == nil)
	if errors.Is(err, providers.ErrPending) {
		return "", providers.ErrPending
	}
	if err != nil {
		return "", fmt.Errorf("finish the sign-in: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.upsert(l.Provider, &cred)
	if err := s.saveLocked(); err != nil {
		return "", err
	}
	s.publishLocked(ctx)
	return name, nil
}

// claimLogin marks sign-in id busy and returns it.
func (s *Service) claimLogin(id string) (pendingLogin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.logins[id]
	switch {
	case !ok || time.Since(l.started) > s.cfg.LoginTTL:
		delete(s.logins, id)
		return pendingLogin{}, ErrUnknownLogin
	case l.busy:
		return pendingLogin{}, ErrLoginBusy
	}
	l.busy = true
	s.logins[id] = l
	return l, nil
}

// releaseLogin ends a Finish of sign-in id: done drops the sign-in, and
// otherwise it is open again.
func (s *Service) releaseLogin(id string, done bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.logins[id]
	switch {
	case !ok:
	case done:
		delete(s.logins, id)
	default:
		l.busy = false
		s.logins[id] = l
	}
}

// upsert saves cred as provider's account named after who signed in.
func (s *Service) upsert(provider string, cred *providers.Credential) string {
	who := firstOf(cred.Email, cred.AccountID)
	for i := range s.state.Accounts {
		a := &s.state.Accounts[i]
		if a.Provider == provider && who != "" && firstOf(a.Credential.Email, a.Credential.AccountID) == who {
			a.Credential, a.NeedsSignIn, a.RefreshError, a.RefreshedAt = *cred, false, "", time.Now()
			return a.Name
		}
	}
	name := provider + "-" + slug(who)
	if who == "" || s.find(name) != nil {
		suffix, err := newID()
		if err != nil {
			suffix = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		name = provider + "-" + firstOf(slug(who), "account") + "-" + suffix[:6]
	}
	s.state.Accounts = append(s.state.Accounts, Account{Name: name, Provider: provider, Credential: *cred, AddedAt: time.Now(), RefreshedAt: time.Now()})
	return name
}

// CancelLogin abandons sign-in id.
func (s *Service) CancelLogin(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.logins, id)
}

// SetDisabled turns an account off or on, and publishes.
func (s *Service) SetDisabled(ctx context.Context, name string, disabled bool) error {
	return s.change(ctx, func() error {
		a := s.find(name)
		if a == nil {
			return fmt.Errorf("%w %q", ErrUnknownAccount, name)
		}
		a.Disabled = disabled
		return nil
	})
}

// Remove deletes an account and its tokens, and publishes.
func (s *Service) Remove(ctx context.Context, name string) error {
	return s.change(ctx, func() error {
		for i := range s.state.Accounts {
			if s.state.Accounts[i].Name == name {
				s.state.Accounts = append(s.state.Accounts[:i], s.state.Accounts[i+1:]...)
				return nil
			}
		}
		return fmt.Errorf("%w %q", ErrUnknownAccount, name)
	})
}

// SetChain sets the order provider's accounts are tried in, and publishes.
func (s *Service) SetChain(ctx context.Context, provider string, names []string) error {
	if _, ok := providers.ByID(s.providers, provider); !ok {
		return fmt.Errorf("%w %q", ErrUnknownProvider, provider)
	}
	return s.change(ctx, func() error {
		clean := make([]string, 0, len(names))
		for _, n := range names {
			if a := s.find(n); a != nil && a.Provider == provider {
				clean = append(clean, n)
			}
		}
		if len(clean) == 0 {
			return errors.New("the chain must name at least one of the agent's accounts")
		}
		s.state.Chains[provider] = clean
		return nil
	})
}

// change applies fn under the lock, saves, and publishes.
func (s *Service) change(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(); err != nil {
		return err
	}
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.publishLocked(ctx)
	return nil
}

// Refresh renews an account's token now, and publishes.
func (s *Service) Refresh(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.find(name)
	if a == nil {
		return fmt.Errorf("%w %q", ErrUnknownAccount, name)
	}
	err := s.renewLocked(ctx, a)
	if serr := s.saveLocked(); serr != nil {
		return serr
	}
	s.publishLocked(ctx)
	return err
}

// renewLocked renews one account, recording how it went.
func (s *Service) renewLocked(ctx context.Context, a *Account) error {
	p, ok := providers.ByID(s.providers, a.Provider)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownProvider, a.Provider)
	}
	a.TriedAt = time.Now()
	cred, err := p.Refresh(ctx, a.Credential)
	if errors.Is(err, providers.ErrNoRefresh) {
		a.NeedsSignIn, a.RefreshError = true, err.Error()
		s.dirty = true
		return fmt.Errorf("renew %s: %w", a.Name, err)
	}
	if err != nil {
		a.Failures++
		a.RefreshError = err.Error()
		return fmt.Errorf("renew %s: %w", a.Name, err)
	}
	if cred.Access != a.Credential.Access {
		s.dirty = true
	}
	a.Credential, a.RefreshedAt, a.RefreshError = cred, time.Now(), ""
	a.Refreshes++
	return nil
}

// Publish puts the accounts into the cluster now and reports what it did.
func (s *Service) Publish(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishNowLocked(ctx)
}

func (s *Service) publishNowLocked(ctx context.Context) (string, error) {
	p, err := buildPlan(&s.state, s.providers, time.Now())
	if err != nil {
		return "", err
	}
	report, err := apply(ctx, s.cluster, s.cfg.Namespaces, &p)
	if err != nil {
		return report, fmt.Errorf("publish: %w", err)
	}
	s.state.Published = p.status
	s.dirty = false
	return report, s.saveLocked()
}

// publishLocked publishes, leaving a failure for the next pass to retry.
func (s *Service) publishLocked(ctx context.Context) {
	s.dirty = true
	if _, err := s.publishNowLocked(ctx); err != nil {
		log.Warn("nightshift-auth: publish failed, retrying on the next pass", "err", err)
	}
}

// Run renews tokens before they lapse and publishes, until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		s.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// pass is one turn of Run.
func (s *Service) pass(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.state.Accounts {
		a := &s.state.Accounts[i]
		if !s.dueLocked(a) {
			continue
		}
		if err := s.renewLocked(ctx, a); err != nil {
			log.Warn("nightshift-auth: renewal failed", "account", a.Name, "err", err)
		}
	}
	if err := s.saveLocked(); err != nil {
		log.Error("nightshift-auth: save the vault", "err", err)
	}
	if s.dirty || time.Since(s.state.Published.At) >= s.cfg.PublishEvery {
		if _, err := s.publishNowLocked(ctx); err != nil {
			log.Warn("nightshift-auth: publish failed, retrying on the next pass", "err", err)
		}
	}
}

// dueLocked is whether the pass should renew a now.
func (s *Service) dueLocked(a *Account) bool {
	switch {
	case a.Disabled, a.NeedsSignIn, a.Credential.Expires.IsZero():
		return false
	case a.Credential.Expires.After(time.Now().Add(s.cfg.RefreshAhead)):
		return false
	default:
		return a.RefreshError == "" || time.Since(a.TriedAt) >= retryAfter
	}
}

func (s *Service) saveLocked() error { return s.store.Save(&s.state) }

func (s *Service) find(name string) *Account {
	for i := range s.state.Accounts {
		if s.state.Accounts[i].Name == name {
			return &s.state.Accounts[i]
		}
	}
	return nil
}

func newID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("make an id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

var unsafeName = regexp.MustCompile(`[^a-z0-9@._-]+`)

// slug is s as part of an account name.
func slug(s string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
