package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestEachViewerOfATerminalHasItsOwnChannel keeps two frontends that watch one
// terminal from sharing a viewer: they would take each other's "something
// changed" signal, and the one with the smaller window would resize the other.
func TestEachViewerOfATerminalHasItsOwnChannel(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=^TestViewerProcess$\n", exe)
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIGHTSHIFT_FAKE_VIEWER", "1")
	pod := "viewers-test"
	t.Cleanup(func() { ClosePod(pod) })

	for _, who := range []string{"alice", "bob"} {
		if _, err := WatchScreenAs(pod, "t1", who, 20, 4, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	conns.Lock()
	c := conns.byPod[pod]
	conns.Unlock()
	if c == nil {
		t.Fatal("no connection to the pod")
	}
	c.mu.Lock()
	alice, bob := c.viewers[viewerKey("t1", "alice")], c.viewers[viewerKey("t1", "bob")]
	c.mu.Unlock()
	if alice == nil || bob == nil || alice == bob || alice.channel == bob.channel {
		t.Fatalf("alice and bob should each have a channel: alice=%v bob=%v", alice, bob)
	}

	DisconnectViewerAs(pod, "t1", "alice")
	<-alice.done
	select {
	case <-bob.done:
		t.Fatal("detaching alice detached bob")
	default:
	}
}
