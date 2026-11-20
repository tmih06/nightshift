package session

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

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

func TestStyledScreen(t *testing.T) {
	v := &viewer{dirty: make(chan struct{}, 1), done: make(chan struct{}), screen: vt.NewEmulator(20, 4)}
	v.drainScreenReplies()
	defer v.closeScreen()

	// An attach paint and later output can split escape sequences between frames.
	for _, part := range []string{"\x1b[2J\x1b[H", "\x1b[3", "1mred\x1b[0m normal", "\r\n\x1b[38;2;12;34;56mRGB\x1b[0m"} {
		v.applyOutput([]byte(part))
	}
	lines := strings.Split(v.screen.Render(), "\n")
	if got := ansi.Strip(lines[0]); got != "red normal" {
		t.Fatalf("first line = %q", got)
	}
	if !strings.Contains(lines[0], "\x1b[") || !strings.Contains(lines[1], "38;2;12;34;56") {
		t.Fatalf("colors missing from render: %q", lines)
	}
	if got := ansi.Strip(lines[1]); got != "RGB" {
		t.Fatalf("second line = %q", got)
	}

	// A redraw and an alternate screen must replace the previous content.
	v.applyOutput([]byte("\x1b[?1049h\x1b[H\x1b[44mBLUE\x1b[0m"))
	alt := v.screen.Render()
	if !strings.Contains(ansi.Strip(alt), "BLUE") || strings.Contains(ansi.Strip(alt), "red normal") || !strings.Contains(alt, "\x1b[") {
		t.Fatalf("alternate screen = %q", alt)
	}
	v.applyOutput([]byte("\x1b[?1049l\x1b[1;1H\x1b[2Knew"))
	if got := ansi.Strip(v.screen.Render()); !strings.HasPrefix(got, "new\nRGB") {
		t.Fatalf("restored screen = %q", got)
	}
}

func TestSameSizeResizePreservesScrollRegion(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	v := &viewer{in: input, channel: 9, dirty: make(chan struct{}, 1), screen: vt.NewEmulator(10, 4)}
	v.drainScreenReplies()
	defer v.closeScreen()
	v.applyOutput([]byte("\x1b[1;1Htop\x1b[2;1Hone\x1b[3;1Htwo\x1b[4;1Hbottom\x1b[2;3r\x1b[3;1H"))
	if err := v.resize(10, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	channel, kind, size, err := readFrame(input)
	if err != nil || channel != 9 || kind != 3 || !bytes.Equal(size, []byte{10, 0, 4, 0}) {
		t.Fatalf("resize frame = %d %d %v %v", channel, kind, size, err)
	}
	v.applyOutput([]byte("\nnew"))
	if got := ansi.Strip(v.screen.Render()); got != "top\ntwo\nnew\nbottom" {
		t.Fatalf("scroll region after resize = %q", got)
	}
}

// TestViewerProcess acts as hc connect for TestViewerStream.
func TestViewerProcess(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_FAKE_VIEWER") != "1" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	if _, err := r.ReadString('\n'); err != nil {
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	if _, _, _, err := readFrame(r); err != nil {
		os.Exit(1)
	}
	_ = writeFrame(os.Stdout, 0, 0, []byte(`{"jsonrpc":"2.0","id":2,"result":{"channel":1}}`))
	// Include a query to exercise the emulator's reply drain.
	_ = writeFrame(os.Stdout, 1, 0, []byte("\x1b[6n\x1b[2J\x1b[H\x1b[31mred\x1b[0m"))
	for {
		channel, kind, _, err := readFrame(r)
		if err != nil {
			os.Exit(0)
		}
		if channel == 1 && kind == 0 {
			_ = writeFrame(os.Stdout, 1, 0, []byte("\r\n\x1b[38;5;33mblue\x1b[0m"))
		}
	}
}

func TestViewerStream(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stub := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=^TestViewerProcess$\n", exe)
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NIGHTSHIFT_FAKE_VIEWER", "1")
	pod := "stream-test"
	if _, err := WatchScreen(pod, 20, 4, true, 0); err != nil {
		t.Fatal(err)
	}
	viewers.Lock()
	v := viewers.byPod[pod]
	viewers.Unlock()
	t.Cleanup(func() {
		DisconnectViewer(pod)
		<-v.done
	})
	waitFor := func(word string) []string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			lines, err := WatchScreen(pod, 20, 4, false, 100*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), word) {
				return lines
			}
		}
		t.Fatalf("screen did not contain %q", word)
		return nil
	}
	lines := waitFor("red")
	if !strings.Contains(lines[0], "\x1b[") {
		t.Fatalf("initial paint has no color: %q", lines)
	}
	if err := SendBatch(pod, []Key{{Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	lines = waitFor("blue")
	if !strings.Contains(lines[1], "\x1b[") {
		t.Fatalf("output has no color: %q", lines)
	}
	DisconnectViewer(pod)
	<-v.done
	if _, err := WatchScreen(pod, 20, 4, true, 0); err != nil {
		t.Fatal(err)
	}
	viewers.Lock()
	v = viewers.byPod[pod]
	viewers.Unlock()
	lines = waitFor("red")
	if !strings.Contains(lines[0], "\x1b[") {
		t.Fatalf("reconnected paint has no color: %q", lines)
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
	if err := SendBatch(pod, []Key{{Text: `printf '\033[38;2;12;34;56mNIGHTSHIFT_COLOR\033[0m\n'`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "38;2;12;34;56mNIGHTSHIFT_COLOR") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("color not captured: %q, %v", lines, err)
		}
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
	if err := SendBatch(pod, []Key{{Text: `export NIGHTSHIFT_RECONNECT=LIVE_OK; sleep 0.3; printf '\033[38;2;12;34;56mNIGHTSHIFT_%s\033[0m\n' LIVE_OK`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "38;2;12;34;56mNIGHTSHIFT_LIVE_OK") {
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
