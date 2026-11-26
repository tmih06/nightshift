package server

import (
	"context"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// The server speaks only in the contract's types. What stands behind each
// interface is the services' business: the cluster in production, a fake in
// tests. A nil interface means the server does not offer that part, and says
// so in its capabilities.

// Include says which extras a fleet read gathers.
type Include struct {
	Configs   bool
	Live      bool
	Versions  bool
	Terminals bool
}

// Fleet reads the sandbox fleet.
type Fleet interface {
	Fleet(ctx context.Context, inc Include) (v1.Fleet, error)
	PodLogs(ctx context.Context, namespace, pod string, tail int) ([]string, error)
}

// Claims makes and releases claims on warm pools.
type Claims interface {
	Claim(ctx context.Context, pool string) (v1.Claim, error)
	// Claimed returns the claim, waiting up to wait for it to have a sandbox.
	Claimed(ctx context.Context, name string, wait time.Duration) (v1.Claim, error)
	DeleteClaim(ctx context.Context, name string) error
}

// Checkouts runs checkouts in the background.
type Checkouts interface {
	Start(req v1.CheckoutRequest) (v1.Checkout, error)
	Get(id string) (v1.Checkout, error)
	Follow(ctx context.Context, id string, fn func(v1.CheckoutEvent) error) error
}

// Volumes keeps what checkouts leave behind.
type Volumes interface {
	Volumes(ctx context.Context) ([]v1.Volume, error)
	DeleteVolume(ctx context.Context, name string) error
	DeleteSandbox(ctx context.Context, name string, deleteVolume bool) error
}

// ScreenQuery is what a viewer asks of a terminal's screen.
type ScreenQuery struct {
	Viewer string
	Cols   int
	Rows   int
	Resize bool
	Wait   time.Duration
}

// Terminals drives the terminals inside claimed sandboxes.
type Terminals interface {
	Terminals(ctx context.Context, pod string) ([]v1.Terminal, error)
	OpenTerminal(ctx context.Context, pod string, req v1.TerminalRequest) (v1.Terminal, error)
	CloseTerminal(ctx context.Context, pod, address string) error
	Screen(ctx context.Context, pod, address string, q ScreenQuery) (v1.Screen, error)
	Input(ctx context.Context, pod, address, viewer string, keys []v1.Key) error
}

// Accounts manages the coding-agent accounts.
type Accounts interface {
	Accounts(ctx context.Context) (v1.Accounts, error)
	StartLogin(ctx context.Context, provider string) (v1.Login, error)
	CompleteLogin(ctx context.Context, id string, c v1.LoginCompletion) error
	CancelLogin(ctx context.Context, id, provider string) error
	SetDisabled(ctx context.Context, name string, disabled bool) error
	Remove(ctx context.Context, name string) error
	Refresh(ctx context.Context, name string) error
	SetActive(ctx context.Context, provider, name string) error
	SetChain(ctx context.Context, provider string, names []string) error
	Publish(ctx context.Context) (string, error)
	RefreshQuotas(ctx context.Context) error
}
