package checkout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// keepFinished is how long a finished checkout stays readable.
const keepFinished = 30 * time.Minute

// ErrNotFound is the error for a checkout that does not exist, or has been
// forgotten.
var ErrNotFound = errors.New("no such checkout")

// ErrNoRun is ErrNotFound for a run.
var ErrNoRun = errors.New("no such run")

// Manager runs checkouts in the background and lets any number of frontends
// follow them. It owns every goroutine it starts: Close waits for them.
type Manager struct {
	ops Ops

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu   sync.Mutex
	runs map[string]*run
}

// run is one checkout in flight or just finished. It keeps every event that
// ends a step or the run, and only the latest of the rest: a clone reports
// every line git prints, and a frontend that joins late needs where things
// stand and what finished, not each line.
type run struct {
	info       v1.Checkout
	milestones []v1.CheckoutEvent
	latest     v1.CheckoutEvent
	seq        uint64        // events so far
	changed    chan struct{} // closed and replaced on every event
	ended      time.Time
}

func milestone(ev v1.CheckoutEvent) bool { return ev.Finished || ev.Done || ev.Error != "" }

// NewManager returns a Manager that acts through ops.
func NewManager(ops Ops) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ops: ops, ctx: ctx, cancel: cancel, runs: map[string]*run{}}
}

// Start begins a checkout. When the same target is already being checked out,
// it returns that one instead of racing it for the volume.
func (m *Manager) Start(req v1.CheckoutRequest) (v1.Checkout, error) {
	if why := req.Problem(); why != "" {
		return v1.Checkout{}, &InvalidError{Reason: why}
	}
	template, err := m.ops.PoolTemplate(req.Pool)
	if err != nil {
		return v1.Checkout{}, fmt.Errorf("pool %s: %w", req.Pool, err)
	}
	spec := SpecOf(req)
	vol := spec.Volume()
	// A volume holds one sandbox at a time, so a checkout of its target goes
	// back to the one working on it.
	if open, err := m.ops.SandboxOn(vol); err != nil {
		return v1.Checkout{}, err
	} else if open != "" {
		return m.reopened(req, vol, open), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return v1.Checkout{}, errors.New("the server is shutting down")
	}
	m.prune(time.Now())
	for _, r := range m.runs {
		if r.info.Volume == vol && r.info.State == v1.CheckoutRunning {
			return r.info, nil
		}
	}
	r := &run{
		info: v1.Checkout{
			ID: newID(), Target: req.Target, Pool: req.Pool, Volume: vol,
			State: v1.CheckoutRunning, StartedAt: time.Now().UTC(),
		},
		changed: make(chan struct{}),
	}
	m.runs[r.info.ID] = r
	m.wg.Go(func() {
		Run(m.ops, spec, template, func(ev v1.CheckoutEvent) { m.record(r, ev) })
	})
	return r.info, nil
}

// reopened is a checkout that is done before it begins: the target is already
// open in a sandbox. It is remembered like any other, so it can be followed.
func (m *Manager) reopened(req v1.CheckoutRequest, volume, sandbox string) v1.Checkout {
	now := time.Now().UTC()
	last := v1.CheckoutEvent{Step: v1.StepShell, Sandbox: sandbox, Done: true, Detail: "already open in " + sandbox, At: now}
	r := &run{
		info: v1.Checkout{
			ID: newID(), Target: req.Target, Pool: req.Pool, Volume: volume, State: v1.CheckoutDone,
			Sandbox: sandbox, StartedAt: now, Last: &last,
		},
		milestones: []v1.CheckoutEvent{last}, latest: last, seq: 1, changed: make(chan struct{}), ended: time.Now(),
	}
	m.mu.Lock()
	m.runs[r.info.ID] = r
	m.mu.Unlock()
	return r.info
}

// InvalidError is a request the pipeline cannot take.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

// ConflictError is a request that what exists already forbids.
type ConflictError struct{ Reason string }

func (e *ConflictError) Error() string { return e.Reason }

// record adds an event to a run and wakes everyone following it.
func (m *Manager) record(r *run, ev v1.CheckoutEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.seq++
	r.latest = ev
	if milestone(ev) {
		r.milestones = append(r.milestones, ev)
	}
	last := ev
	r.info.Last = &last
	if ev.Sandbox != "" {
		r.info.Sandbox = ev.Sandbox
	}
	switch {
	case ev.Done:
		r.info.State, r.ended = v1.CheckoutDone, time.Now()
	case ev.Error != "":
		r.info.State, r.ended = v1.CheckoutFailed, time.Now()
	}
	close(r.changed)
	r.changed = make(chan struct{})
}

// Get is where a checkout stands.
func (m *Manager) Get(id string) (v1.Checkout, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return v1.Checkout{}, ErrNotFound
	}
	return r.info, nil
}

// Follow calls fn with the checkout's events, and returns once the last one is
// delivered, fn fails, or ctx ends. Every event that finished a step arrives,
// in order, and so does the latest of the others whenever there is a newer one
// than fn has seen; a frontend that joins late is caught up, so its display
// comes out the same.
func (m *Manager) Follow(ctx context.Context, id string, fn func(v1.CheckoutEvent) error) error {
	var (
		next int    // milestones delivered
		seen uint64 // events accounted for
	)
	for {
		m.mu.Lock()
		r, ok := m.runs[id]
		if !ok {
			m.mu.Unlock()
			return ErrNotFound
		}
		batch := append([]v1.CheckoutEvent(nil), r.milestones[next:]...)
		next = len(r.milestones)
		if r.seq != seen && r.seq > 0 && !milestone(r.latest) {
			batch = append(batch, r.latest)
		}
		seen = r.seq
		changed, finished := r.changed, r.info.State != v1.CheckoutRunning
		m.mu.Unlock()

		for _, ev := range batch {
			if err := fn(ev); err != nil {
				return err
			}
		}
		if finished {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return fmt.Errorf("stopped following %s: %w", id, ctx.Err())
		case <-m.ctx.Done():
			return errors.New("the server is shutting down")
		}
	}
}

// prune forgets finished checkouts that have been readable long enough. The
// caller holds mu.
func (m *Manager) prune(now time.Time) {
	for id, r := range m.runs {
		if !r.ended.IsZero() && now.Sub(r.ended) > keepFinished {
			delete(m.runs, id)
		}
	}
}

// Close stops taking checkouts and waits for the ones running, until ctx ends.
// A checkout cut off by ctx carries on in the cluster; it only stops being
// reported.
func (m *Manager) Close(ctx context.Context) {
	m.cancel()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("co-%d", time.Now().UnixNano())
	}
	return "co-" + hex.EncodeToString(b)
}
