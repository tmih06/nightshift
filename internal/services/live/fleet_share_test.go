package live

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/server"
)

// Every frontend polls the fleet, and the services have little memory. Reads
// that overlap must not each start their own kubectl processes.
func TestOverlappingFleetReadsShareTheirKubectls(t *testing.T) {
	stubs, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("CALLS", filepath.Join(dir, "calls"))
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := NewFleet()
	f.fetch = func() ([]cluster.Sandbox, map[string]cluster.Pool, bool, error) {
		<-time.After(50 * time.Millisecond)
		return []cluster.Sandbox{{Name: "pi-on-volume", Volume: "pr-acme-rocket-7", Phase: "Running"}}, nil, false, nil
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := f.Fleet(context.Background(), server.Include{Live: true, Versions: true}); err != nil {
				t.Errorf("Fleet: %v", err)
			}
		})
	}
	wg.Wait()
	got, err := fs.ReadFile(os.DirFS(dir), "calls")
	if err != nil {
		t.Fatal(err)
	}
	// One each for the cgroups, the version, and the volumes.
	if n := strings.Count(string(got), "x"); n != 3 {
		t.Errorf("5 overlapping fleet reads started %d kubectls, want 3", n)
	}
}
