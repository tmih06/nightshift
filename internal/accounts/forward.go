package accounts

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/log/v2"

	"github.com/tmih06/nightshift/internal/cluster"
)

// Endpoint is somewhere an HTTP service can be reached.
type Endpoint interface {
	// URL is the service's base URL, starting whatever it needs to.
	URL(ctx context.Context) (string, error)
	Close()
}

// Static is an Endpoint at a fixed address.
type Static string

// URL returns the address.
func (s Static) URL(context.Context) (string, error) { return string(s), nil }

// Close does nothing.
func (Static) Close() {}

var forwardingLine = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

const forwardReady = 20 * time.Second

// Forward keeps a kubectl port-forward to one in-cluster service running on a
// local port of its own choosing, and starts it again if it drops. It owns the
// kubectl process and the goroutine that watches it; Close ends both.
type Forward struct {
	Namespace string
	Target    string // for example svc/cpa
	Port      int    // the service's port

	startMu sync.Mutex // one start at a time
	port    atomic.Int64
	stopMu  sync.Mutex
	stop    context.CancelFunc
	wg      sync.WaitGroup
}

func address(port int64) string { return "http://127.0.0.1:" + strconv.FormatInt(port, 10) }

// URL starts the forward when it is not running and returns its address.
func (f *Forward) URL(ctx context.Context) (string, error) {
	f.startMu.Lock()
	defer f.startMu.Unlock()
	if p := f.port.Load(); p != 0 {
		return address(p), nil
	}
	return f.start(ctx)
}

func (f *Forward) start(ctx context.Context) (string, error) {
	// The forward outlives the call that started it: only Close ends it.
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	cmd := cluster.PortForward(runCtx, f.Namespace, f.Target, f.Port)
	out, err := cmd.StdoutPipe()
	if err != nil {
		stop()
		return "", fmt.Errorf("port-forward %s: %w", f.Target, err)
	}
	if err := cmd.Start(); err != nil {
		stop()
		return "", fmt.Errorf("port-forward %s: %w", f.Target, err)
	}
	ready := make(chan int64, 1)
	f.wg.Add(1)
	go f.watch(cmd, out, ready)
	port, err := f.await(ctx, ready)
	if err != nil {
		stop()
		return "", err
	}
	f.stopMu.Lock()
	f.stop = stop
	f.stopMu.Unlock()
	return address(port), nil
}

// watch reads kubectl's output until it exits. Only this goroutine writes the
// port, so a forward that dies right away cannot be recorded as alive later.
func (f *Forward) watch(cmd *exec.Cmd, out io.Reader, ready chan<- int64) {
	defer f.wg.Done()
	sc := bufio.NewScanner(out)
	sent := false
	for sc.Scan() {
		m := forwardingLine.FindStringSubmatch(sc.Text())
		if m == nil || sent {
			continue
		}
		if p, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			f.port.Store(p)
			ready <- p
			sent = true
		}
	}
	if err := cmd.Wait(); err != nil {
		log.Debug("accounts: port-forward ended", "target", f.Target, "err", err)
	}
	f.port.Store(0) // the next URL call starts a fresh forward
	close(ready)
}

func (f *Forward) await(ctx context.Context, ready <-chan int64) (int64, error) {
	select {
	case p, ok := <-ready:
		if !ok {
			return 0, fmt.Errorf("port-forward %s ended before it was ready", f.Target)
		}
		return p, nil
	case <-ctx.Done():
		return 0, fmt.Errorf("port-forward %s: %w", f.Target, ctx.Err())
	case <-time.After(forwardReady):
		return 0, errors.New("port-forward " + f.Target + " did not become ready in 20 seconds")
	}
}

// Close stops the forward and waits for its goroutine.
func (f *Forward) Close() {
	f.stopMu.Lock()
	stop := f.stop
	f.stop = nil
	f.stopMu.Unlock()
	if stop != nil {
		stop()
	}
	f.wg.Wait()
}
