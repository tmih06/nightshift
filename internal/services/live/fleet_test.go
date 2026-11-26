package live

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tmih06/nightshift/internal/services/cluster"
)

func countedFleet(reads *atomic.Int32) *Fleet {
	f := NewFleet()
	f.fetch = func() ([]cluster.Sandbox, map[string]cluster.Pool, bool, error) {
		reads.Add(1)
		<-time.After(50 * time.Millisecond)
		return boxes(), nil, false, nil
	}
	return f
}

func claimed(t *testing.T, f *Fleet, pod string) bool {
	t.Helper()
	ok, err := f.Claimed(context.Background(), pod)
	if err != nil {
		t.Errorf("Claimed(%s): %v", pod, err)
	}
	return ok
}

// Screen reads and key presses ask Claimed many times a second. Starting a
// cluster read for each stalled typing and got the services OOM-killed.
func TestClaimedSharesOneClusterRead(t *testing.T) {
	var reads atomic.Int32
	f := countedFleet(&reads)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if !claimed(t, f, "pi-claimed") {
				t.Errorf("pi-claimed is not claimed")
			}
		})
	}
	wg.Wait()
	if n := reads.Load(); n > 2 {
		t.Errorf("20 requests at once made %d cluster reads, want 1", n)
	}

	reads.Store(0)
	f.snap.at = time.Now().Add(-10 * snapshotTTL / 2)
	if !claimed(t, f, "pi-claimed") || reads.Load() != 0 {
		t.Errorf("a recently seen claim was read again: %d reads", reads.Load())
	}
	if claimed(t, f, "pi-warm") || reads.Load() == 0 {
		t.Errorf("a warm sandbox must be refused after a fresh read, got %d reads", reads.Load())
	}
}
