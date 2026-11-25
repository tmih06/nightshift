// Package live is the services' backend: it answers the server's questions from
// the cluster, through kubectl, and the coding-agent accounts' own services.
// It is the one place where the contract's types meet the cluster's.
package live

import (
	"context"
	"time"

	"github.com/tmih06/nightshift/internal/services/accounts"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

// Backend stands behind every interface of the server.
type Backend struct {
	Fleet     *Fleet
	Claims    *Claims
	Volumes   *Volumes
	Terminals *Terminals
	// Accounts is nil when the backend has no account service.
	Accounts  *Accounts
	Checkouts *checkout.Manager
}

// Options say how the backend reaches what it needs.
type Options struct {
	// Accounts is where the account service is reached; nil leaves accounts
	// out. Locally it is port-forwards, in a cluster it is the Services'
	// addresses.
	Accounts accounts.Service
}

// New returns a Backend for the cluster kubectl points at.
func New(opts Options) *Backend {
	fleet := NewFleet()
	b := &Backend{
		Fleet:     fleet,
		Claims:    &Claims{},
		Volumes:   &Volumes{fleet: fleet},
		Terminals: NewTerminals(fleet),
		Checkouts: checkout.NewManager(checkout.Live()),
	}
	if opts.Accounts != nil {
		b.Accounts = &Accounts{svc: opts.Accounts}
	}
	return b
}

// Close stops what the backend runs, waiting for it until ctx ends.
func (b *Backend) Close(ctx context.Context) {
	b.Checkouts.Close(ctx)
	b.Terminals.Close()
	if b.Accounts != nil {
		b.Accounts.svc.Close()
	}
}

// call runs fn, which cannot be cancelled, and gives up waiting for it when
// ctx ends. fn still finishes in its goroutine, bounded by kubectl's own
// timeouts.
func call[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// pause waits d, or less if ctx ends first, and says whether ctx is still live.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
