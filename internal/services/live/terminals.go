package live

import (
	"context"
	"sync"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/server"
	"github.com/tmih06/nightshift/internal/services/session"
)

const (
	// viewerIdle is how long a viewer may go unasked-for before its channel
	// into the sandbox is dropped. A frontend that vanished leaves one behind.
	viewerIdle = 2 * time.Minute
	reapEvery  = 30 * time.Second
)

type viewerRef struct{ pod, term, who string }

// Terminals drives the terminals in claimed sandboxes. It owns the reaper that
// drops the viewers of frontends that went away; Close stops it.
type Terminals struct {
	fleet *Fleet

	mu   sync.Mutex
	seen map[viewerRef]time.Time

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewTerminals returns Terminals that check pods against fleet.
func NewTerminals(fleet *Fleet) *Terminals {
	t := &Terminals{fleet: fleet, seen: map[viewerRef]time.Time{}, stop: make(chan struct{})}
	t.wg.Go(func() { t.reap(reapEvery, viewerIdle) })
	return t
}

// Close stops the reaper.
func (t *Terminals) Close() {
	close(t.stop)
	t.wg.Wait()
}

func (t *Terminals) reap(every, idle time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case now := <-tick.C:
			t.dropIdle(now, idle)
		}
	}
}

func (t *Terminals) dropIdle(now time.Time, idle time.Duration) {
	t.mu.Lock()
	var gone []viewerRef
	for ref, at := range t.seen {
		if now.Sub(at) > idle {
			gone = append(gone, ref)
			delete(t.seen, ref)
		}
	}
	t.mu.Unlock()
	for _, ref := range gone {
		session.DisconnectViewerAs(ref.pod, ref.term, ref.who)
	}
}

func (t *Terminals) touch(ref viewerRef) {
	if ref.who == "" {
		return
	}
	t.mu.Lock()
	t.seen[ref] = time.Now()
	t.mu.Unlock()
}

// guard lets a request through only for a sandbox someone holds. Without it
// the API would open a shell in any pod of the namespace: the credential
// refresher's, for one.
func (t *Terminals) guard(ctx context.Context, pod string) error {
	ok, err := t.fleet.Claimed(ctx, pod)
	if err != nil {
		return err
	}
	if !ok {
		return &notFoundError{what: "claimed sandbox " + pod}
	}
	return nil
}

func terminalOf(x session.Terminal) v1.Terminal {
	return v1.Terminal{
		Address: x.Address, Name: x.Name, Command: x.Command, Title: x.Title,
		Status: x.Status, CWD: x.CWD, CreatedAt: x.Created(),
	}
}

// Terminals lists the live terminals of a claimed sandbox.
func (t *Terminals) Terminals(ctx context.Context, pod string) ([]v1.Terminal, error) {
	if err := t.guard(ctx, pod); err != nil {
		return nil, err
	}
	terms, err := call(ctx, func() ([]session.Terminal, error) { return session.List(pod) })
	if err != nil {
		return nil, err
	}
	out := make([]v1.Terminal, 0, len(terms))
	for _, x := range terms {
		out = append(out, terminalOf(x))
	}
	return out, nil
}

// OpenTerminal opens a terminal. The first one a sandbox gets is the default
// one its session starts with, so asking for no particular terminal on a
// sandbox that has none starts that session.
func (t *Terminals) OpenTerminal(ctx context.Context, pod string, req v1.TerminalRequest) (v1.Terminal, error) {
	if err := t.guard(ctx, pod); err != nil {
		return v1.Terminal{}, err
	}
	x, err := call(ctx, func() (session.Terminal, error) { return open(pod, req) })
	if err != nil {
		return v1.Terminal{}, err
	}
	return terminalOf(x), nil
}

func open(pod string, req v1.TerminalRequest) (session.Terminal, error) {
	plain := req.Name == "" && req.From == "" && req.CWD == ""
	if existing, err := session.List(pod); plain && err == nil && len(existing) == 0 {
		if err := session.Ensure(pod); err != nil {
			return session.Terminal{}, err
		}
		terms, err := session.List(pod)
		if err != nil || len(terms) == 0 {
			return session.Terminal{}, firstErr(err, "the session started with no terminal")
		}
		return terms[0], nil
	}
	switch {
	case req.From != "":
		return session.CreateFrom(pod, req.Name, req.From)
	case req.CWD != "":
		return session.CreateIn(pod, req.Name, req.CWD)
	}
	return session.Create(pod, req.Name)
}

// CloseTerminal stops a terminal.
func (t *Terminals) CloseTerminal(ctx context.Context, pod, address string) error {
	if err := t.guard(ctx, pod); err != nil {
		return err
	}
	_, err := call(ctx, func() (struct{}, error) { return struct{}{}, session.Kill(pod, address) })
	return err
}

// Screen waits for a terminal to change, as q says, and returns what it shows.
func (t *Terminals) Screen(ctx context.Context, pod, address string, q server.ScreenQuery) (v1.Screen, error) {
	if err := t.guard(ctx, pod); err != nil {
		return v1.Screen{}, err
	}
	ref := viewerRef{pod, address, q.Viewer}
	t.touch(ref)
	defer t.touch(ref)
	lines, err := call(ctx, func() ([]string, error) {
		return session.WatchScreenAs(pod, address, q.Viewer, q.Cols, q.Rows, q.Resize, q.Wait)
	})
	if err != nil {
		return v1.Screen{}, err
	}
	return v1.Screen{Changed: lines != nil, Cols: q.Cols, Rows: q.Rows, Lines: lines}, nil
}

// Input presses keys in a terminal.
func (t *Terminals) Input(ctx context.Context, pod, address string, keys []v1.Key) error {
	if err := t.guard(ctx, pod); err != nil {
		return err
	}
	ks := make([]session.Key, len(keys))
	for i, k := range keys {
		ks[i] = session.Key{Text: k.Text, Name: k.Name}
	}
	_, err := call(ctx, func() (struct{}, error) { return struct{}{}, session.SendBatch(pod, address, ks) })
	return err
}
