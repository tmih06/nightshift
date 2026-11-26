package live

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

// claimPoll is how often a claim that is being waited for is looked at.
const claimPoll = 300 * time.Millisecond

// Claims makes and releases claims on warm pools.
type Claims struct{}

// Claim claims a sandbox of the pool.
func (*Claims) Claim(ctx context.Context, pool string) (v1.Claim, error) {
	name, err := call(ctx, func() (string, error) { return cluster.Claim(pool) })
	if err != nil {
		return v1.Claim{}, err
	}
	return v1.Claim{Name: name, Pool: pool}, nil
}

// Claimed reads the claim, waiting up to wait for it to have a sandbox. A wait
// that runs out is not an error: the claim comes back without one.
func (*Claims) Claimed(ctx context.Context, name string, wait time.Duration) (v1.Claim, error) {
	deadline := time.Now().Add(wait)
	for {
		type info struct{ pool, sandbox string }
		got, err := call(ctx, func() (info, error) {
			pool, sandbox, err := cluster.ClaimInfo(name)
			return info{pool, sandbox}, wrapErr("read the claim", err)
		})
		if err != nil {
			return v1.Claim{}, err
		}
		c := v1.Claim{Name: name, Pool: got.pool, Sandbox: got.sandbox}
		if c.Bound() || !time.Now().Add(claimPoll).Before(deadline) {
			return c, nil
		}
		if !pause(ctx, claimPoll) {
			return c, fmt.Errorf("waiting for claim %s: %w", name, ctx.Err())
		}
	}
}

// DeleteClaim releases a claim; its sandbox and pod go with it.
func (*Claims) DeleteClaim(ctx context.Context, name string) error {
	_, err := call(ctx, func() (struct{}, error) { return struct{}{}, cluster.DeleteClaim(name) })
	return err
}
