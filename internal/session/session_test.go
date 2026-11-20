package session

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tmih06/nightshift/internal/cluster"
)

func TestName(t *testing.T) {
	pod := strings.Repeat("long-pod-", 20)
	if got := Name(pod); len(got) > 32 || got != Name(pod) || got == Name(pod+"x") {
		t.Fatalf("invalid or colliding name: %q", got)
	}
}

func TestKubectlCommand(t *testing.T) {
	want := []string{"kubectl", "--context", "kind-agent-sandbox", "-n", "sandbox-demo", "exec", "-i", "-t", "agent-pool-5ss7w", "--", "hc", "attach", "example"}
	if got := kubectl("agent-pool-5ss7w", true, true, "attach", "example").Args; !reflect.DeepEqual(got, want) {
		t.Errorf("kubectl = %v, want %v", got, want)
	}
}

func TestMissingHerdrReportsOldImage(t *testing.T) {
	dir := t.TempDir()
	stub := "#!/bin/sh\necho 'OCI runtime exec failed: exec: \"hc\": executable file not found in $PATH' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := hc("old-pod", "run")
	if err == nil || err.Error() != "hc missing in old-pod; claim a new sandbox with the updated image" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFrameAndKeys(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, 42, 0, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	channel, kind, data, err := readFrame(&buf)
	if err != nil || channel != 42 || kind != 0 || string(data) != "hello" {
		t.Fatalf("frame = %d %d %q %v", channel, kind, data, err)
	}
	for name, want := range map[string]string{"Enter": "\r", "C-c": "\x03", "Up": "\x1b[A", "BSpace": "\x7f"} {
		if got, ok := keyBytes(name); !ok || got != want {
			t.Errorf("keyBytes(%q) = %q, %v", name, got, ok)
		}
	}
}

// This integration test uses an isolated Herdr core and a fake kubectl, so it
// tests the real protocol without creating or modifying cluster resources.
func TestHerdrSession(t *testing.T) {
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc not installed")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte("#!/bin/sh\nwhile [ \"$1\" != -- ]; do shift; done\nshift\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HC_SESSION", "nightshift-test")
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Cleanup(func() { _ = exec.Command("hc", "server", "stop").Run() })
	pod := "agent-pool-test"
	if err := Ensure(pod); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(pod); err != nil {
		t.Fatal(err)
	}
	boxes := []cluster.Sandbox{{Name: pod, OwnerKind: "SandboxClaim", Phase: "Running"}}
	if !Has(pod) || !Live(boxes)[pod] {
		t.Fatal("session not listed")
	}
	if _, err := WatchScreen(pod, 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, []Key{{Text: "printf NIGHTSHIFT_%s HERDR_OK"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "NIGHTSHIFT_HERDR_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output not captured: %q, %v", lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Closing the dashboard's viewer must not end the shell. Reconnect and
	// check that a later command still reaches the same running terminal.
	viewers.Lock()
	v := viewers.byPod[pod]
	viewers.Unlock()
	if err := v.in.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		viewers.Lock()
		gone := viewers.byPod[pod] == nil
		viewers.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("viewer did not disconnect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !Has(pod) {
		t.Fatal("viewer disconnect ended the shell")
	}
	if _, err := WatchScreen(pod, 91, 27, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, []Key{{Text: "stty size"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "27 91") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resize not applied: %q, %v", lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := SendBatch(pod, []Key{{Text: "exit"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for Has(pod) {
		if time.Now().After(deadline) {
			t.Fatal("session remained after command exited")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Opt in with a disposable claimed pod to exercise the actual kubectl tunnel.
func TestHerdrLivePod(t *testing.T) {
	pod := os.Getenv("NIGHTSHIFT_TEST_POD")
	if pod == "" {
		t.Skip("NIGHTSHIFT_TEST_POD not set")
	}
	t.Cleanup(func() { _, _ = hc(pod, "kill", Name(pod)) })
	if err := Ensure(pod); err != nil {
		t.Fatal(err)
	}
	if _, err := WatchScreen(pod, 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	// Output must wake the watcher even after the input batch is long gone.
	if err := SendBatch(pod, []Key{{Text: "export NIGHTSHIFT_RECONNECT=LIVE_OK; sleep 0.3; printf NIGHTSHIFT_%s LIVE_OK"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "NIGHTSHIFT_LIVE_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live output not captured: %q, %v", lines, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	viewers.Lock()
	v := viewers.byPod[pod]
	viewers.Unlock()
	if err := v.in.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		viewers.Lock()
		gone := viewers.byPod[pod] == nil
		viewers.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("viewer did not disconnect")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !Has(pod) {
		t.Fatal("shell ended when kubectl viewer disconnected")
	}
	if _, err := WatchScreen(pod, 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, []Key{{Text: `printf RECONNECTED_%s "$NIGHTSHIFT_RECONNECT"`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "RECONNECTED_LIVE_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("same shell not reconnected: %q, %v", lines, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
