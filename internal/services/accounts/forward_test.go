package accounts

import (
	"strings"
	"testing"
	"time"
)

func TestForwardStartsOnceAndStopsOnClose(t *testing.T) {
	dir := stubKubectl(t, `echo "$*" >> "$TRACE"
echo "Forwarding from 127.0.0.1:43210 -> 8317"
exec sleep 30
`)
	t.Setenv("TRACE", dir+"/trace")
	f := &Forward{Namespace: "nightshift", Target: "svc/cpa", Port: 8317}
	first, err := f.URL(t.Context())
	if err != nil || first != "http://127.0.0.1:43210" {
		t.Fatalf("URL = %q, %v", first, err)
	}
	if again, err := f.URL(t.Context()); err != nil || again != first {
		t.Errorf("a second URL = %q, %v, want the same forward", again, err)
	}
	f.Close()
	lines := strings.Split(strings.TrimSpace(readFile(t, dir, "trace")), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "-n nightshift port-forward svc/cpa :8317") {
		t.Errorf("kubectl ran %v, want one port-forward of svc/cpa", lines)
	}
}

func TestForwardRestartsAfterItDrops(t *testing.T) {
	stubKubectl(t, `echo "Forwarding from 127.0.0.1:43211 -> 8317"
exit 0
`)
	f := &Forward{Namespace: "nightshift", Target: "svc/cpa", Port: 8317}
	defer f.Close()
	if _, err := f.URL(t.Context()); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(5 * time.Second)
	for f.port.Load() != 0 {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("the forward never noticed that kubectl exited")
		}
	}
	if _, err := f.URL(t.Context()); err != nil {
		t.Errorf("a dropped forward must start again: %v", err)
	}
}

func TestForwardReportsKubectlThatNeverGetsReady(t *testing.T) {
	stubKubectl(t, `echo "error: unable to forward" >&2
exit 1
`)
	f := &Forward{Namespace: "nightshift", Target: "svc/cpa", Port: 8317}
	defer f.Close()
	if _, err := f.URL(t.Context()); err == nil {
		t.Error("a kubectl that exits at once must be an error")
	}
}
