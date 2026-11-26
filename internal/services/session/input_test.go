package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeHerdr points kubectl at TestViewerProcess.
func fakeHerdr(t *testing.T, pod string) {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=^TestViewerProcess$\n", exe)
	stubPath := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(stubPath, []byte(stub), 0o600); err != nil {
		t.Fatal(err)
	}
	chmod := exec.CommandContext(context.Background(), "sh", "-c", `chmod 700 "$STUB"`)
	chmod.Env = append(os.Environ(), "STUB="+stubPath)
	if out, err := chmod.CombinedOutput(); err != nil {
		t.Fatalf("make the stub runnable: %v: %s", err, out)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIGHTSHIFT_FAKE_VIEWER", "1")
	t.Cleanup(func() { ClosePod(pod) })
}

// attached lists the viewers a pod's connection holds.
func attached(t *testing.T, pod string) map[string]*viewer {
	t.Helper()
	conns.Lock()
	c := conns.byPod[pod]
	conns.Unlock()
	if c == nil {
		t.Fatal("no connection to the pod")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]*viewer{}
	for k, v := range c.viewers {
		out[k] = v
	}
	return out
}

// Herdr gives a terminal the size of whoever typed last. Typing through a
// second viewer at 160x40 resized the terminal away from what was on screen.
func TestTypingKeepsTheTerminalsSize(t *testing.T) {
	const pod = "input-test"
	fakeHerdr(t, pod)
	if _, err := WatchScreenFor(Watch{Pod: pod, Term: "t1", Viewer: "alice", W: 20, H: 4, Resize: true}); err != nil {
		t.Fatal(err)
	}
	if err := SendBatchFrom(pod, "t1", "alice", []Key{{Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	if got := attached(t, pod); len(got) != 1 {
		t.Errorf("typing as a watching viewer attached another: %d viewers", len(got))
	}

	if err := SendBatch(pod, "t1", []Key{{Text: "y"}}); err != nil {
		t.Fatal(err)
	}
	v := attached(t, pod)["t1"]
	if v == nil {
		t.Fatal("no shared viewer for a caller that is not watching")
	}
	v.screenMu.Lock()
	defer v.screenMu.Unlock()
	if w, h := v.screen.Width(), v.screen.Height(); w != 20 || h != 4 {
		t.Errorf("typed without watching at %dx%d, want the terminal's own 20x4", w, h)
	}
}
