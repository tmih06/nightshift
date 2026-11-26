package dashboard

import (
	"context"
	"sync"
	"time"

	"github.com/tmih06/nightshift/api/client"
	v1 "github.com/tmih06/nightshift/api/v1"
)

// fakeAPI stands in for the services.
type fakeAPI struct {
	mu          sync.Mutex
	fleets      int
	include     []string
	fleet       v1.Fleet
	fleetErr    error
	events      []v1.CheckoutEvent
	checkout    v1.CheckoutRequest
	checkoutErr error
	followErr   error
	logs        []string
	keys        []v1.Key
	deleted     string
	dropVolume  bool
}

func (f *fakeAPI) Fleet(_ context.Context, include ...string) (v1.Fleet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fleets++
	f.include = include
	return f.fleet, f.fleetErr
}
func (f *fakeAPI) PodLogs(context.Context, string, string, int) ([]string, error) {
	return f.logs, nil
}
func (f *fakeAPI) Claim(_ context.Context, pool string) (v1.Claim, error) {
	return v1.Claim{Name: "c1", Pool: pool}, nil
}
func (f *fakeAPI) WaitClaim(_ context.Context, name string, _ time.Duration) (v1.Claim, error) {
	return v1.Claim{Name: name, Sandbox: "pi-abc"}, nil
}
func (f *fakeAPI) DeleteClaim(_ context.Context, name string) error { f.deleted = name; return nil }
func (f *fakeAPI) DeleteSandbox(_ context.Context, name string, drop bool) error {
	f.deleted, f.dropVolume = name, drop
	return nil
}
func (f *fakeAPI) Checkout(_ context.Context, req v1.CheckoutRequest) (v1.Checkout, error) {
	f.checkout = req
	return v1.Checkout{ID: "co-1"}, f.checkoutErr
}
func (f *fakeAPI) FollowCheckout(_ context.Context, _ string, fn func(v1.CheckoutEvent) error) error {
	for _, ev := range f.events {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return f.followErr
}
func (f *fakeAPI) Terminals(context.Context, string) ([]v1.Terminal, error) {
	return []v1.Terminal{{Address: "t1", Label: "bash"}}, nil
}
func (f *fakeAPI) OpenTerminal(context.Context, string, v1.TerminalRequest) (v1.Terminal, error) {
	return v1.Terminal{Address: "t2", Label: "bash"}, nil
}
func (f *fakeAPI) CloseTerminal(context.Context, string, string) error { return nil }
func (f *fakeAPI) Screen(_ context.Context, r client.ScreenRequest) (v1.Screen, error) {
	return v1.Screen{Changed: true, Cols: r.Cols, Rows: r.Rows, Lines: []string{"$ "}}, nil
}
func (f *fakeAPI) Input(_ context.Context, _, _ string, keys []v1.Key) error {
	f.keys = append(f.keys, keys...)
	return nil
}
