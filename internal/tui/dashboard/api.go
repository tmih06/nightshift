package dashboard

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/api/client"
	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

// API is what the Pods tab asks of the Nightshift services. *client.Client
// satisfies it, and a test can stand in for it.
type API interface {
	Fleet(ctx context.Context, include ...string) (v1.Fleet, error)
	PodLogs(ctx context.Context, namespace, pod string, tail int) ([]string, error)
	Claim(ctx context.Context, pool string) (v1.Claim, error)
	WaitClaim(ctx context.Context, name string, wait time.Duration) (v1.Claim, error)
	DeleteClaim(ctx context.Context, name string) error
	DeleteSandbox(ctx context.Context, name string, deleteVolume bool) error
	Checkout(ctx context.Context, req v1.CheckoutRequest) (v1.Checkout, error)
	FollowCheckout(ctx context.Context, id string, fn func(v1.CheckoutEvent) error) error
	Terminals(ctx context.Context, sandbox string) ([]v1.Terminal, error)
	OpenTerminal(ctx context.Context, sandbox string, req v1.TerminalRequest) (v1.Terminal, error)
	CloseTerminal(ctx context.Context, sandbox, address string) error
	Screen(ctx context.Context, r client.ScreenRequest) (v1.Screen, error)
	Input(ctx context.Context, sandbox, address string, keys []v1.Key) error
}

const (
	// defaultTerminal is the address a sandbox's first terminal has.
	defaultTerminal = "t1"
	// defaultPool is claimed when nothing picks one.
	defaultPool = "pi-pool"

	fetchTimeout = 20 * time.Second
	claimTimeout = 40 * time.Second
	callTimeout  = 30 * time.Second
	// screenSlack is how long past its wait a screen request may take.
	screenSlack = 10 * time.Second
)

// call runs fn with a deadline of its own: every command goes out under one.
func call[T any](timeout time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return fn(ctx)
}

// fetchCmd reloads the fleet, with the live numbers, the agent versions, the
// manifests, and each sandbox's terminals.
func fetchCmd(api API) tea.Cmd {
	return func() tea.Msg {
		f, err := call(fetchTimeout, func(ctx context.Context) (v1.Fleet, error) {
			return api.Fleet(ctx, v1.IncludeConfigs, v1.IncludeLive, v1.IncludeVersions, v1.IncludeTerminals)
		})
		if err != nil {
			return loadedMsg{err: err}
		}
		boxes, pools := fleet.Load(f)
		windows := map[string][]v1.Terminal{}
		sessions := map[string]bool{}
		metricsOK := false
		for i := range boxes {
			metricsOK = metricsOK || boxes[i].UsageKnown
			if len(boxes[i].Terminals) > 0 {
				windows[boxes[i].Name] = boxes[i].Terminals
				sessions[boxes[i].Name] = true
			}
		}
		return loadedMsg{boxes: boxes, pools: pools, sessions: sessions, windows: windows, metricsOK: metricsOK}
	}
}

func claimCmd(api API, pool string) tea.Cmd {
	return func() tea.Msg {
		c, err := call(callTimeout, func(ctx context.Context) (v1.Claim, error) { return api.Claim(ctx, pool) })
		return claimMsg{name: c.Name, err: err}
	}
}

// claimPool is the pool a claim draws from: the selected warm sandbox's, or
// the default.
func (m model) claimPool() string {
	if b := m.selectedBox(); b != nil && b.Warm() {
		return b.Owner
	}
	return defaultPool
}

func deleteClaimCmd(api API, claim string) tea.Cmd {
	return func() tea.Msg {
		_, err := call(callTimeout, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, api.DeleteClaim(ctx, claim)
		})
		return deletedMsg{claim: claim, err: err}
	}
}

// waitClaimCmd waits for the claim's sandbox and makes sure it has a terminal.
func waitClaimCmd(api API, claim string) tea.Cmd {
	return func() tea.Msg {
		c, err := call(claimTimeout, func(ctx context.Context) (v1.Claim, error) {
			return api.WaitClaim(ctx, claim, 30*time.Second)
		})
		if err != nil {
			return claimAdoptedMsg{err: err}
		}
		if !c.Bound() {
			return claimAdoptedMsg{err: errNoSandbox(claim)}
		}
		if _, err := openFirst(api, c.Sandbox); err != nil {
			return claimAdoptedMsg{err: err}
		}
		return claimAdoptedMsg{pod: c.Sandbox}
	}
}

type errNoSandbox string

func (e errNoSandbox) Error() string { return "claim " + string(e) + " adopted no sandbox in time" }

// openFirst lists a sandbox's terminals, starting its session when it has none.
func openFirst(api API, pod string) ([]v1.Terminal, error) {
	return call(callTimeout, func(ctx context.Context) ([]v1.Terminal, error) {
		terms, err := api.Terminals(ctx, pod)
		if err == nil && len(terms) > 0 {
			return terms, nil
		}
		if _, err := api.OpenTerminal(ctx, pod, v1.TerminalRequest{}); err != nil {
			return nil, fmt.Errorf("open first: %w", err)
		}
		return api.Terminals(ctx, pod)
	})
}

func attachCmd(api API, pod string) tea.Cmd {
	return func() tea.Msg {
		terms, err := openFirst(api, pod)
		return attachReadyMsg{pod: pod, terms: terms, err: err}
	}
}

func createWindowCmd(api API, pod, fromTerm string) tea.Cmd {
	return func() tea.Msg {
		term, err := call(callTimeout, func(ctx context.Context) (v1.Terminal, error) {
			return api.OpenTerminal(ctx, pod, v1.TerminalRequest{From: fromTerm})
		})
		return windowCreatedMsg{pod: pod, term: term, err: err}
	}
}

func killWindowCmd(api API, pod, term string) tea.Cmd {
	return func() tea.Msg {
		_, err := call(callTimeout, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, api.CloseTerminal(ctx, pod, term)
		})
		return windowKilledMsg{pod: pod, term: term, err: err}
	}
}

// watchShellCmd waits a moment for the terminal to change and returns what it
// shows; nil lines mean nothing changed.
func watchShellCmd(api API, r client.ScreenRequest) tea.Cmd {
	pod, term := r.Sandbox, r.Address
	r.Wait = shellRefreshEvery
	return func() tea.Msg {
		sc, err := call(shellRefreshEvery+screenSlack, func(ctx context.Context) (v1.Screen, error) {
			return api.Screen(ctx, r)
		})
		var lines []string
		if sc.Changed {
			lines = sc.Lines
			if lines == nil {
				lines = []string{}
			}
		}
		return shellMsg{pod: pod, term: term, lines: lines, err: err}
	}
}

func drainCmd(api API, pod, term string, keys []v1.Key) tea.Cmd {
	return func() tea.Msg {
		_, err := call(callTimeout, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, api.Input(ctx, pod, term, keys)
		})
		return sentMsg{pod: pod, term: term, count: len(keys), err: err}
	}
}
