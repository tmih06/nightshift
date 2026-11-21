package session

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	if !isGeneratedName(Name(pod)) {
		t.Fatalf("Name(pod) should be recognized as generated: %q", Name(pod))
	}
	if isGeneratedName("dev") {
		t.Fatal("custom name must not be recognized as generated")
	}
}

func TestTerminalLabel(t *testing.T) {
	if got := (Terminal{Address: "t1", Name: Name("pod-a"), Command: "claude"}).Label(); got != "claude" {
		t.Errorf("generated name label = %q, want claude", got)
	}
	if got := (Terminal{Address: "t2", Name: "tests", Command: "bash"}).Label(); got != "tests" {
		t.Errorf("explicit name label = %q, want tests", got)
	}
	if got := (Terminal{Address: "t3"}).Label(); got != "t3" {
		t.Errorf("bare terminal label = %q, want t3", got)
	}
}

func TestKubectlCommand(t *testing.T) {
	want := []string{"kubectl", "--context", "kind-agent-sandbox", "-n", "nightshift", "exec", "-i", "-t", "agent-pool-5ss7w", "--", "hc", "attach", "example"}
	if got := kubectl("agent-pool-5ss7w", true, true, "attach", "example").Args; !reflect.DeepEqual(got, want) {
		t.Errorf("kubectl = %v, want %v", got, want)
	}
	if got := Attach("agent-pool-5ss7w", "t2").Args; got[len(got)-1] != "t2" {
		t.Errorf("Attach(..., t2) = %v", got)
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
	_, err = List("old-pod")
	if err == nil || err.Error() != "hc missing in old-pod; claim a new sandbox with the updated image" {
		t.Fatalf("unexpected connect error: %v", err)
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

// TestViewerProcess acts as a multi-terminal hc connect daemon for TestViewerStream.
func TestViewerProcess(t *testing.T) {
	if os.Getenv("NIGHTSHIFT_FAKE_VIEWER") != "1" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	if _, err := r.ReadString('\n'); err != nil {
		os.Exit(1)
	}
	_, _ = fmt.Fprintln(os.Stdout, `{"jsonrpc":"2.0","id":1,"result":{}}`)

	type fakeFG struct {
		Name string `json:"name,omitempty"`
		CWD  string `json:"cwd,omitempty"`
	}
	type fakeTerm struct {
		Address    string   `json:"address"`
		Name       string   `json:"name,omitempty"`
		Argv       []string `json:"argv"`
		State      string   `json:"state"`
		CWD        string   `json:"cwd,omitempty"`
		Foreground *fakeFG  `json:"foreground,omitempty"`
		CreatedMS  uint64   `json:"created_ms"`
	}
	terms := map[string]fakeTerm{
		"t1": {
			Address:    "t1",
			Argv:       []string{"bash"},
			State:      "live",
			CWD:        "/home/agent",
			Foreground: &fakeFG{Name: "bash", CWD: "/home/agent/workspace/app"},
			CreatedMS:  1,
		},
	}
	nextTerm := 2
	nextChan := uint32(1)
	chanToTerm := map[uint32]string{}

	sendRPC := func(id uint64, result any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		_ = writeFrame(os.Stdout, 0, 0, b)
	}

	for {
		channel, kind, payload, err := readFrame(r)
		if err != nil {
			os.Exit(0)
		}
		if channel == 0 && kind == 0 {
			var req struct {
				ID     uint64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(payload, &req) != nil {
				continue
			}
			switch req.Method {
			case "terminal.list":
				list := make([]fakeTerm, 0, len(terms))
				for _, ft := range terms {
					list = append(list, ft)
				}
				sendRPC(req.ID, map[string]any{"terminals": list, "hidden_finished": 0, "machines": []any{}})
			case "terminal.get":
				var p struct {
					Terminal string `json:"terminal"`
				}
				_ = json.Unmarshal(req.Params, &p)
				ft, ok := terms[p.Terminal]
				if !ok {
					b, _ := json.Marshal(map[string]any{
						"jsonrpc": "2.0",
						"id":      req.ID,
						"error": map[string]any{
							"code":    -32000,
							"message": p.Terminal + " not found",
							"data":    map[string]any{"kind": "not_found"},
						},
					})
					_ = writeFrame(os.Stdout, 0, 0, b)
					continue
				}
				sendRPC(req.ID, ft)
			case "terminal.run":
				var p struct {
					Name string `json:"name"`
					CWD  string `json:"cwd"`
				}
				_ = json.Unmarshal(req.Params, &p)
				addr := fmt.Sprintf("t%d", nextTerm)
				nextTerm++
				ft := fakeTerm{Address: addr, Name: p.Name, Argv: []string{"bash"}, State: "live", CWD: p.CWD, CreatedMS: uint64(nextTerm)}
				terms[addr] = ft
				sendRPC(req.ID, ft)
			case "terminal.kill":
				var p struct {
					Terminal string `json:"terminal"`
				}
				_ = json.Unmarshal(req.Params, &p)
				delete(terms, p.Terminal)
				// Match real herdr-core 0.1.21 behavior on rm: true terminals.
				b, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"error": map[string]any{
						"code":    -32000,
						"message": p.Terminal + " was removed",
						"data":    map[string]any{"kind": "not_found"},
					},
				})
				_ = writeFrame(os.Stdout, 0, 0, b)
			case "terminal.attach":
				var p struct {
					Terminal string `json:"terminal"`
				}
				_ = json.Unmarshal(req.Params, &p)
				ch := nextChan
				nextChan++
				chanToTerm[ch] = p.Terminal
				sendRPC(req.ID, map[string]any{"channel": ch})
				// Include a query to exercise the emulator's reply drain.
				paint := fmt.Sprintf("\x1b[6n\x1b[2J\x1b[H\x1b[31mred-%s\x1b[0m", p.Terminal)
				_ = writeFrame(os.Stdout, ch, 0, []byte(paint))
			}
			continue
		}
		if channel > 0 && kind == 0 {
			term := chanToTerm[channel]
			_ = writeFrame(os.Stdout, channel, 0, []byte(fmt.Sprintf("\r\n\x1b[38;5;33mblue-%s\x1b[0m", term)))
		}
		if channel > 0 && kind == 2 {
			delete(chanToTerm, channel)
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
	t.Cleanup(func() { ClosePod(pod) })

	// List, create a second terminal, and multiplex both over one podConn.
	terms, err := List(pod)
	if err != nil || len(terms) != 1 || terms[0].Address != "t1" {
		t.Fatalf("List = %v, %v", terms, err)
	}
	created, err := Create(pod, "agent")
	if err != nil || created.Address != "t2" || created.Label() != "agent" {
		t.Fatalf("Create = %+v, %v", created, err)
	}
	terms, err = List(pod)
	if err != nil || len(terms) != 2 {
		t.Fatalf("List after Create = %v, %v", terms, err)
	}

	if _, err := WatchScreen(pod, "t1", 20, 4, true, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := WatchScreen(pod, "t2", 20, 4, true, 0); err != nil {
		t.Fatal(err)
	}

	// Both viewers share the same single podConn.
	conns.Lock()
	c := conns.byPod[pod]
	conns.Unlock()
	if c == nil {
		t.Fatal("missing shared podConn")
	}
	c.mu.Lock()
	v1 := c.viewers["t1"]
	v2 := c.viewers["t2"]
	c.mu.Unlock()
	if v1 == nil || v2 == nil || v1.channel == v2.channel {
		t.Fatalf("expected two multiplexed channels on one podConn: v1=%v v2=%v", v1, v2)
	}

	waitFor := func(term, word string) []string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			lines, err := WatchScreen(pod, term, 20, 4, false, 100*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), word) {
				return lines
			}
		}
		t.Fatalf("screen(%s) did not contain %q", term, word)
		return nil
	}
	lines := waitFor("t1", "red-t1")
	if !strings.Contains(lines[0], "\x1b[") {
		t.Fatalf("initial paint has no color: %q", lines)
	}
	waitFor("t2", "red-t2")

	if err := SendBatch(pod, "t1", []Key{{Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	lines = waitFor("t1", "blue-t1")
	if !strings.Contains(lines[1], "\x1b[") {
		t.Fatalf("output has no color: %q", lines)
	}

	// Detaching one viewer keeps the shared podConn and the second viewer alive.
	DisconnectViewer(pod, "t1")
	<-v1.done
	if err := SendBatch(pod, "t2", []Key{{Text: "y"}}); err != nil {
		t.Fatal(err)
	}
	waitFor("t2", "blue-t2")

	// Reattaching t1 gets a fresh paint over the same podConn.
	if _, err := WatchScreen(pod, "t1", 20, 4, true, 0); err != nil {
		t.Fatal(err)
	}
	lines = waitFor("t1", "red-t1")
	if !strings.Contains(lines[0], "\x1b[") {
		t.Fatalf("reconnected paint has no color: %q", lines)
	}

	// Killing t2 removes it from List while leaving t1 alive.
	if err := Kill(pod, "t2"); err != nil {
		t.Fatal(err)
	}
	terms, err = List(pod)
	if err != nil || len(terms) != 1 || terms[0].Address != "t1" {
		t.Fatalf("List after Kill = %v, %v", terms, err)
	}
}

func TestCreateFromInheritsActiveCWD(t *testing.T) {
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
	pod := "cwd-test"
	t.Cleanup(func() { ClosePod(pod) })

	inherited, err := CreateFrom(pod, "", "t1")
	if err != nil {
		t.Fatalf("CreateFrom(t1) error: %v", err)
	}
	if inherited.CWD != "/home/agent/workspace/app" {
		t.Fatalf("CreateFrom(t1).CWD = %q, want /home/agent/workspace/app", inherited.CWD)
	}

	fallback, err := CreateFrom(pod, "", "missing")
	if err != nil {
		t.Fatalf("CreateFrom(missing) should fall back gracefully: %v", err)
	}
	if fallback.CWD != "" {
		t.Fatalf("CreateFrom(missing).CWD = %q, want empty", fallback.CWD)
	}

	explicit, err := CreateIn(pod, "custom", "/tmp")
	if err != nil {
		t.Fatalf("CreateIn(/tmp) error: %v", err)
	}
	if explicit.CWD != "/tmp" || explicit.Label() != "custom" {
		t.Fatalf("CreateIn(/tmp) = %+v", explicit)
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
	pod := "agent-pool-test"
	t.Cleanup(func() {
		ClosePod(pod)
		_ = exec.Command("hc", "server", "stop").Run()
	})
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
	if _, err := WatchScreen(pod, "", 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, "", []Key{{Text: "printf NIGHTSHIFT_%s HERDR_OK"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "NIGHTSHIFT_HERDR_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output not captured: %q, %v", lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := SendBatch(pod, "", []Key{{Text: `printf '\033[38;2;12;34;56mNIGHTSHIFT_COLOR\033[0m\n'`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "38;2;12;34;56mNIGHTSHIFT_COLOR") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("color not captured: %q, %v", lines, err)
		}
	}
	// Closing the dashboard's connection must not end the shell. Reconnect and
	// check that a later command still reaches the same running terminal.
	ClosePod(pod)
	if !Has(pod) {
		t.Fatal("viewer disconnect ended the shell")
	}
	if _, err := WatchScreen(pod, "", 91, 27, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, "", []Key{{Text: "stty size"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "27 91") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resize not applied: %q, %v", lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// CreateFrom should open a new window in the active window's live cwd.
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(sub); err == nil {
		sub = resolved
	}
	if err := SendBatch(pod, "", []Key{{Text: fmt.Sprintf("cd %q && printf NIGHTSHIFT_CD_OK", sub)}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "NIGHTSHIFT_CD_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cd not completed: %q, %v", lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	terms, err := List(pod)
	if err != nil || len(terms) == 0 {
		t.Fatalf("List before CreateFrom = %v, %v", terms, err)
	}
	w2, err := CreateFrom(pod, "", terms[0].Address)
	if err != nil {
		t.Fatalf("CreateFrom failed: %v", err)
	}
	if _, err := WatchScreen(pod, w2.Address, 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, w2.Address, []Key{{Text: `printf 'NIGHTSHIFT_PWD=%s\n' "$PWD"`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		lines, err := WatchScreen(pod, w2.Address, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "NIGHTSHIFT_PWD="+sub) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new window did not inherit cwd %q: %q, %v", sub, lines, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := Kill(pod, w2.Address); err != nil {
		t.Fatalf("Kill(%s) failed: %v", w2.Address, err)
	}
	if err := SendBatch(pod, "", []Key{{Text: "exit"}, {Name: "Enter"}}); err != nil {
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
	t.Cleanup(func() {
		if terms, err := List(pod); err == nil {
			for _, term := range terms {
				_ = Kill(pod, term.Address)
			}
		}
		ClosePod(pod)
	})
	if err := Ensure(pod); err != nil {
		t.Fatal(err)
	}
	if _, err := WatchScreen(pod, "", 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	// Output must wake the watcher even after the input batch is long gone.
	if err := SendBatch(pod, "", []Key{{Text: `export NIGHTSHIFT_RECONNECT=LIVE_OK; sleep 0.3; printf '\033[38;2;12;34;56mNIGHTSHIFT_%s\033[0m\n' LIVE_OK`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "38;2;12;34;56mNIGHTSHIFT_LIVE_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live output not captured: %q, %v", lines, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Create a second window over the same connection and verify isolation.
	w2, err := Create(pod, "win2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WatchScreen(pod, w2.Address, 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, w2.Address, []Key{{Text: "printf SECOND_WIN_%s OK"}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, w2.Address, 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "SECOND_WIN_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("second window output not captured: %q, %v", lines, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := Kill(pod, w2.Address); err != nil {
		t.Fatalf("Kill(%s) failed: %v", w2.Address, err)
	}
	ClosePod(pod)
	if !Has(pod) {
		t.Fatal("shell ended when kubectl connection closed")
	}
	if _, err := WatchScreen(pod, "", 80, 24, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := SendBatch(pod, "", []Key{{Text: `printf RECONNECTED_%s "$NIGHTSHIFT_RECONNECT"`}, {Name: "Enter"}}); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		lines, err := WatchScreen(pod, "", 80, 24, false, 100*time.Millisecond)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "RECONNECTED_LIVE_OK") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("same shell not reconnected: %q, %v", lines, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
