// Package runs hands a task to a coding agent and follows it to its result:
// it clones the repository onto a volume, starts a sandbox on it, runs the
// agent on the prompt, and records how it ended. It knows no issue tracker.
package runs

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/log/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

const (
	// setups and looks bound the kubectl processes runs start at once, which
	// the services' memory limit has to hold.
	setups = 3
	looks  = 4

	pollEvery  = 5 * time.Second
	retryEvery = 10 * time.Second
)

var errClosed = errors.New("the server is shutting down")

// record is what a run's volume remembers.
type record struct {
	Run v1.Run `json:"run"`
	// Digest tells a repeated request from a different one under the same key.
	Digest string `json:"digest"`
}

// entry is one run the manager holds.
type entry struct {
	rec     record        // guarded by Manager.mu
	changed chan struct{} // closed and replaced on every change; Manager.mu

	// ctx ends when the run is deleted or the manager closes.
	ctx    context.Context
	cancel context.CancelFunc

	// mu keeps records written in the order the changes were made, and a
	// deletion from missing a record or sandbox on its way.
	mu sync.Mutex
}

// Manager holds every run, drives the new ones, and watches the working ones.
// It owns every goroutine it starts: Close waits for them.
type Manager struct {
	ops  Ops
	poll time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	setup  chan struct{}
	look   chan struct{}

	// admit serializes loading and creating; loaded is set once, under it.
	admit  sync.Mutex
	loaded atomic.Bool

	mu   sync.Mutex
	runs map[string]*entry
}

// NewManager returns a Manager that acts through ops. It reads nothing until
// first asked, or until Resume.
func NewManager(ops Ops) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ops: ops, poll: pollEvery, ctx: ctx, cancel: cancel,
		setup: make(chan struct{}, setups), look: make(chan struct{}, looks),
		runs: map[string]*entry{},
	}
}

// Resume reads the runs the cluster remembers and takes up the working ones,
// trying until it can. Without it that happens at the first request.
func (m *Manager) Resume() {
	m.wg.Go(func() {
		for {
			err := m.load()
			if err == nil || errors.Is(err, errClosed) {
				return
			}
			log.Warn("could not read the runs yet", "err", err)
			if !pause(m.ctx, retryEvery) {
				return
			}
		}
	})
}

// load reads the records once. It takes no lock once they are read, so a
// creation in progress holds up nobody who only looks.
func (m *Manager) load() error {
	if m.loaded.Load() && m.ctx.Err() == nil {
		return nil
	}
	m.admit.Lock()
	defer m.admit.Unlock()
	return m.loadLocked()
}

func (m *Manager) loadLocked() error {
	if m.ctx.Err() != nil {
		return errClosed
	}
	if m.loaded.Load() {
		return nil
	}
	records, err := m.ops.Records()
	if err != nil {
		return fmt.Errorf("read the runs: %w", err)
	}
	var cutOff []*entry
	for name, raw := range records {
		var rec record
		if err := json.Unmarshal(raw, &rec); err != nil || rec.Run.ID != name {
			log.Warn("skipping a run record that cannot be read", "volume", name, "err", err)
			continue
		}
		switch e := m.hold(rec); rec.Run.State {
		case v1.RunDone, v1.RunFailed:
		case v1.RunWorking:
			m.wg.Go(func() { m.watch(e) })
		default:
			cutOff = append(cutOff, e)
		}
	}
	m.failCutOff(cutOff)
	m.loaded.Store(true)
	return nil
}

// failCutOff fails the runs a restart cut off before their agent started, which
// cannot go on as their prompt was only held in memory. One at a time: each
// writes its record with a kubectl of its own.
func (m *Manager) failCutOff(cutOff []*entry) {
	if len(cutOff) == 0 {
		return
	}
	m.wg.Go(func() {
		for _, e := range cutOff {
			m.fail(e, "the server restarted before the agent started; delete the run and create it again")
		}
	})
}

func (m *Manager) hold(rec record) *entry {
	ctx, cancel := context.WithCancel(m.ctx)
	e := &entry{rec: rec, changed: make(chan struct{}), ctx: ctx, cancel: cancel}
	m.mu.Lock()
	m.runs[rec.Run.ID] = e
	m.mu.Unlock()
	return e
}

// Create starts a run. A request whose idempotency key was used before returns
// that run, if it asks for the same thing.
func (m *Manager) Create(req v1.RunRequest) (v1.Run, error) {
	if why := req.Problem(); why != "" {
		return v1.Run{}, &checkout.InvalidError{Reason: why}
	}
	m.admit.Lock()
	defer m.admit.Unlock()
	if err := m.loadLocked(); err != nil {
		return v1.Run{}, err
	}
	rec, err := newRecord(req)
	if err != nil {
		return v1.Run{}, err
	}
	if run, found, err := m.existing(rec); found {
		return run, err
	}
	t, err := m.taskFor(req)
	if err != nil {
		return v1.Run{}, err
	}
	return m.admitRun(rec, t)
}

// taskFor finds the template and agent command req's pool runs it with.
func (m *Manager) taskFor(req v1.RunRequest) (task, error) {
	template, err := m.ops.PoolTemplate(req.Pool)
	if err != nil {
		return task{}, fmt.Errorf("pool %s: %w", req.Pool, err)
	}
	command, ok := checkout.TaskCommand(checkout.ForPool(req.Pool, template, "").Command, req.Model)
	if !ok {
		return task{}, &checkout.InvalidError{Reason: "pool " + req.Pool + " has no coding agent that can run a task"}
	}
	return task{template: template, command: command, prompt: req.Prompt}, nil
}

// admitRun makes the run's volume and starts driving it.
func (m *Manager) admitRun(rec record, t task) (v1.Run, error) {
	raw, err := json.Marshal(rec)
	if err != nil {
		return v1.Run{}, fmt.Errorf("record the run: %w", err)
	}
	id, source := rec.Run.ID, rec.Run.Source
	created, err := m.ops.CreateRunVolume(id, source.RepoPath(), branchOf(id, source), raw)
	if err != nil {
		return v1.Run{}, fmt.Errorf("keep the run: %w", err)
	}
	if !created {
		return v1.Run{}, &checkout.ConflictError{Reason: "run " + id + " is still being deleted; try again"}
	}
	e := m.hold(rec)
	m.wg.Go(func() { m.drive(e, t) })
	return rec.Run, nil
}

func (m *Manager) existing(rec record) (v1.Run, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.runs[rec.Run.ID]
	if !ok {
		return v1.Run{}, false, nil
	}
	if e.rec.Digest != rec.Digest {
		return v1.Run{}, true, &checkout.ConflictError{Reason: "this idempotencyKey belongs to a different request"}
	}
	return e.rec.Run, true, nil
}

// newRecord is a queued run for req. With a key, its ID comes from the key, so
// the cluster itself refuses a second volume for it.
func newRecord(req v1.RunRequest) (record, error) {
	var sum [8]byte
	if req.IdempotencyKey != "" {
		full := sha256.Sum256([]byte(req.IdempotencyKey))
		copy(sum[:], full[:])
	} else {
		rand.Read(sum[:])
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return record{}, fmt.Errorf("record the request: %w", err)
	}
	digest := sha256.Sum256(raw)
	now := time.Now().UTC()
	return record{
		Digest: hex.EncodeToString(digest[:]),
		Run: v1.Run{
			ID: "run-" + hex.EncodeToString(sum[:]), State: v1.RunQueued, Source: req.Source,
			Pool: req.Pool, Model: req.Model, ExternalRef: req.ExternalRef,
			TimeoutSeconds: cmp.Or(req.TimeoutSeconds, v1.DefaultRunSeconds),
			CreatedAt:      now, UpdatedAt: now,
		},
	}, nil
}

// Watch is where a run stands, and a channel that closes when that changes.
func (m *Manager) Watch(id string) (v1.Run, <-chan struct{}, error) {
	if err := m.load(); err != nil {
		return v1.Run{}, nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.runs[id]
	if !ok {
		return v1.Run{}, nil, checkout.ErrNoRun
	}
	return e.rec.Run, e.changed, nil
}

// List is every run, newest first, without their output. A non-empty
// externalRef keeps only the runs that carry it.
func (m *Manager) List(externalRef string) ([]v1.Run, error) {
	if err := m.load(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	out := make([]v1.Run, 0, len(m.runs))
	for _, e := range m.runs {
		if externalRef == "" || e.rec.Run.ExternalRef == externalRef {
			run := e.rec.Run
			run.Output = ""
			out = append(out, run)
		}
	}
	m.mu.Unlock()
	slices.SortFunc(out, func(a, b v1.Run) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// Delete stops a run and destroys its sandbox and volume. When the cluster
// refuses, the run stays so it can be deleted again.
func (m *Manager) Delete(id string) error {
	if err := m.load(); err != nil {
		return err
	}
	m.mu.Lock()
	e, ok := m.runs[id]
	m.mu.Unlock()
	if !ok {
		return checkout.ErrNoRun
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := m.destroy(id); err != nil {
		return err
	}
	e.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[id] == e {
		delete(m.runs, id)
		close(e.changed)
	}
	return nil
}

func (m *Manager) destroy(id string) error {
	sandbox, err := m.ops.SandboxOn(id)
	if err != nil {
		return fmt.Errorf("find the sandbox of %s: %w", id, err)
	}
	if sandbox != "" {
		if err := m.ops.DeleteSandbox(sandbox); err != nil {
			return fmt.Errorf("delete the sandbox of %s: %w", id, err)
		}
	}
	// A volume somebody else deleted must not keep its run listed.
	if err := m.ops.DeleteVolume(id); err != nil && !strings.Contains(err.Error(), "(NotFound)") {
		return fmt.Errorf("delete the volume of %s: %w", id, err)
	}
	return nil
}

// update changes a run, tells its followers, and writes its record. A run that
// has been deleted, or a manager that is closing, takes no more changes.
func (m *Manager) update(e *entry, change func(*v1.Run)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m.updateLocked(e, change)
}

// updateLocked is update for a caller that holds e.mu.
func (m *Manager) updateLocked(e *entry, change func(*v1.Run)) {
	m.mu.Lock()
	if e.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	change(&e.rec.Run)
	e.rec.Run.UpdatedAt = time.Now().UTC()
	rec := e.rec
	close(e.changed)
	e.changed = make(chan struct{})
	m.mu.Unlock()

	raw, err := json.Marshal(rec)
	if err == nil {
		err = m.ops.Save(rec.Run.ID, raw)
	}
	if err != nil {
		log.Warn("could not record the run", "run", rec.Run.ID, "state", rec.Run.State, "err", err)
	}
}

func (m *Manager) fail(e *entry, why string) {
	m.update(e, func(r *v1.Run) { r.State, r.Error = v1.RunFailed, why })
}

// pause waits d, and says whether ctx is still live.
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

// Close stops driving and watching, and waits until ctx ends for what is in
// hand. Agents carry on in their sandboxes; Resume finds them again.
func (m *Manager) Close(ctx context.Context) {
	m.cancel()
	m.mu.Lock()
	for _, e := range m.runs {
		close(e.changed)
		e.changed = make(chan struct{})
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}
