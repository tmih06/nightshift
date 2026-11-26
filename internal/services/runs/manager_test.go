package runs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
	"github.com/tmih06/nightshift/internal/services/session"
)

// fakeCluster is the cluster a manager acts on.
type fakeCluster struct {
	mu       sync.Mutex
	records  map[string][]byte
	sandbox  string
	files    map[string]string
	typed    string
	answer   string // what the status script prints
	cloneErr error
	hold     chan struct{} // when set, Clone waits for it
	deleted  []string
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{records: map[string][]byte{}, files: map[string]string{}, answer: "running\n"}
}

func (f *fakeCluster) locked(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeCluster) ops() Ops {
	return Ops{
		PoolTemplate: func(string) (string, error) { return "claude-code-template", nil },
		TemplatePod:  func(string) (map[string]any, string, error) { return map[string]any{}, "img", nil },
		CreateVolume: func(name, _, _ string, record []byte) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if _, ok := f.records[name]; ok {
				return false, nil
			}
			f.records[name] = record
			return true, nil
		},
		Records: func() (map[string][]byte, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			out := map[string][]byte{}
			for k, v := range f.records {
				out[k] = v
			}
			return out, nil
		},
		Save: func(name string, record []byte) error {
			f.locked(func() { f.records[name] = record })
			return nil
		},
		Clone: func(string, string, string, time.Duration, func(string)) error {
			if f.hold != nil {
				<-f.hold
			}
			return f.cloneErr
		},
		CreateSandbox: func(string, map[string]any, string, string) (string, error) {
			f.locked(func() { f.sandbox = "claude-run-abc" })
			return "claude-run-abc", nil
		},
		WaitPod: func(string, time.Duration, func(string)) error { return nil },
		SandboxOn: func(string) (name string, err error) {
			f.locked(func() { name = f.sandbox })
			return name, nil
		},
		Exec: func(_ string, stdin []byte, args ...string) (out []byte, err error) {
			f.locked(func() {
				cmd := args[len(args)-1]
				if cmd == statusScript {
					out = []byte(f.answer)
					return
				}
				f.files[cmd[strings.LastIndex(cmd, "/")+1:]] = string(stdin)
			})
			return out, nil
		},
		Ensure: func(string) error { return nil },
		Send: func(_ string, keys []session.Key) error {
			f.locked(func() { f.typed = keys[0].Text })
			return nil
		},
		DeleteSandbox: func(name string) error {
			f.locked(func() { f.sandbox, f.deleted = "", append(f.deleted, name) })
			return nil
		},
		DeleteVolume: func(name string) error {
			f.locked(func() { delete(f.records, name); f.deleted = append(f.deleted, name) })
			return nil
		},
	}
}

func newManager(t *testing.T, f *fakeCluster) *Manager {
	t.Helper()
	m := NewManager(f.ops())
	m.poll = time.Millisecond
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Close(ctx)
	})
	return m
}

func request() v1.RunRequest {
	return v1.RunRequest{
		Source: v1.RunSource{CloneURL: "https://gitlab.com/acme/rocket.git"},
		Prompt: "fix the 'bug'", Pool: "claude-code-pool",
		ExternalRef: "linear:ENG-42", IdempotencyKey: "eng-42",
	}
}

// until waits for the run to satisfy ok.
func until(t *testing.T, m *Manager, id string, ok func(v1.Run) bool) v1.Run {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		run, changed, err := m.Watch(id)
		if err != nil {
			t.Fatal(err)
		}
		if ok(run) {
			return run
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatalf("the run stayed at %+v", run)
		}
	}
}

func inState(state string) func(v1.Run) bool {
	return func(r v1.Run) bool { return r.State == state }
}

func TestARunGoesFromRequestToResult(t *testing.T) {
	f := newFakeCluster()
	m := newManager(t, f)
	run, err := m.Create(request())
	if err != nil {
		t.Fatal(err)
	}
	until(t, m, run.ID, inState(v1.RunWorking))
	f.locked(func() {
		if f.files[promptFile] != "fix the 'bug'" || !strings.Contains(f.files[scriptFile], "claude --dangerously-skip-permissions --print") {
			t.Errorf("the sandbox got %q", f.files)
		}
		if !strings.HasPrefix(f.typed, "bash ") {
			t.Errorf("typed %q", f.typed)
		}
		f.answer = "exit 0\nall done\n"
	})
	got := until(t, m, run.ID, v1.Run.Finished)
	if got.State != v1.RunDone || got.ExitCode == nil || *got.ExitCode != 0 || got.Output != "all done\n" {
		t.Errorf("finished as %+v", got)
	}
	list, err := m.List("linear:ENG-42")
	if err != nil || len(list) != 1 || list[0].Output != "" {
		t.Errorf("List = %+v, %v", list, err)
	}
	if other, err := m.List("jira:X-1"); err != nil || len(other) != 0 {
		t.Errorf("List of another ticket = %+v, %v", other, err)
	}
}

func TestAFailingAgentFailsTheRun(t *testing.T) {
	f := newFakeCluster()
	f.answer = "exit 3\nboom\n"
	m := newManager(t, f)
	run, err := m.Create(request())
	if err != nil {
		t.Fatal(err)
	}
	got := until(t, m, run.ID, v1.Run.Finished)
	if got.State != v1.RunFailed || got.ExitCode == nil || *got.ExitCode != 3 || got.Error == "" {
		t.Errorf("finished as %+v", got)
	}
}

func TestAFailedCloneFailsTheRun(t *testing.T) {
	f := newFakeCluster()
	f.cloneErr = errors.New("repository not found")
	m := newManager(t, f)
	run, err := m.Create(request())
	if err != nil {
		t.Fatal(err)
	}
	if got := until(t, m, run.ID, v1.Run.Finished); got.State != v1.RunFailed || !strings.Contains(got.Error, "repository not found") {
		t.Errorf("finished as %+v", got)
	}
}

func TestAskingTwiceUnderOneKeyMakesOneRun(t *testing.T) {
	f := newFakeCluster()
	m := newManager(t, f)
	first, err := m.Create(request())
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Create(request())
	if err != nil || again.ID != first.ID {
		t.Errorf("again = %+v, %v, want %s", again, err, first.ID)
	}
	other := request()
	other.Prompt = "something else"
	var conflict *checkout.ConflictError
	if _, err := m.Create(other); !errors.As(err, &conflict) {
		t.Errorf("a different request under the key: %v", err)
	}
	// A restarted server still knows the key.
	if again, err := newManager(t, f).Create(request()); err != nil || again.ID != first.ID {
		t.Errorf("after a restart = %+v, %v", again, err)
	}
}

func TestDeletingARunBeingSetUpLeavesNoSandbox(t *testing.T) {
	f := newFakeCluster()
	f.hold = make(chan struct{})
	m := newManager(t, f)
	run, err := m.Create(request())
	if err != nil {
		t.Fatal(err)
	}
	until(t, m, run.ID, inState(v1.RunCloning))
	if err := m.Delete(run.ID); err != nil {
		t.Fatal(err)
	}
	close(f.hold)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Close(ctx)
	f.locked(func() {
		if f.sandbox != "" || len(f.records) != 0 {
			t.Errorf("left sandbox %q and records %q", f.sandbox, f.records)
		}
	})
}

func TestARestartTakesUpWorkingRunsAndFailsTheRest(t *testing.T) {
	f := newFakeCluster()
	f.sandbox, f.answer = "claude-run-abc", "exit 0\n"
	for id, state := range map[string]string{"run-a": v1.RunWorking, "run-b": v1.RunCloning} {
		now := time.Now().UTC()
		raw, err := json.Marshal(record{Run: v1.Run{ID: id, State: state, Sandbox: f.sandbox, TimeoutSeconds: 60, CreatedAt: now, StartedAt: &now}})
		if err != nil {
			t.Fatal(err)
		}
		f.records[id] = raw
	}
	m := newManager(t, f)
	m.Resume()
	if got := until(t, m, "run-a", v1.Run.Finished); got.State != v1.RunDone {
		t.Errorf("the working run ended as %+v", got)
	}
	if got := until(t, m, "run-b", v1.Run.Finished); got.State != v1.RunFailed {
		t.Errorf("the half set up run ended as %+v", got)
	}
}

func TestOnlyGitHubIsOfferedTheGitHubToken(t *testing.T) {
	for url, want := range map[string]bool{
		"https://github.com/a/b":              true,
		"https://gitlab.com/a/b":              false,
		"https://github.com.evil.example/a/b": false,
		"https://evil.example/github.com/a/b": false,
	} {
		if got := strings.Contains(cloneScript("run-1", v1.RunSource{CloneURL: url}), "GITHUB_TOKEN"); got != want {
			t.Errorf("%s: offered = %v, want %v", url, got, want)
		}
	}
}

func TestEveryPoolAgentCanRunATask(t *testing.T) {
	for _, pool := range []string{"pi-pool", "codex-pool", "claude-code-pool", "agy-pool", "ompig-slim-pool", "opencode-pool"} {
		if _, ok := agentCommand(checkout.ForPool(pool, "", "").Command, ""); !ok {
			t.Errorf("%s has no command", pool)
		}
	}
}

// TestTheAgentScriptLeavesAResult runs the script in a real bash, with a stub
// for the agent.
func TestTheAgentScriptLeavesAResult(t *testing.T) {
	cases := map[string]struct {
		command string
		seconds int
		want    status
	}{
		"success": {`bash -c 'printf %s "$1"' _ "$p"`, 30, status{exited: true, code: 0, output: "say 'hi'; $(false)"}},
		"failure": {`sh -c 'echo no; exit 3'`, 30, status{exited: true, code: 3, output: "no\n"}},
		"timeout": {`sh -c 'exec sleep 20'`, 1, status{exited: true, code: 124}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := runScript(t, agentScript(c.command, t.TempDir(), c.seconds), "say 'hi'; $(false)")
			if got != c.want {
				t.Errorf("status = %+v, want %+v", got, c.want)
			}
		})
	}
}

// runScript runs script as the sandbox would, then reads the result as the
// services do.
func runScript(t *testing.T, script, prompt string) status {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".nightshift")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{scriptFile: script, promptFile: prompt} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sh := func(args ...string) []byte {
		cmd := exec.CommandContext(ctx, "bash")
		cmd.Args = append(cmd.Args, args...)
		cmd.Env = append(os.Environ(), "HOME="+home)
		out, err := cmd.Output()
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatal(err)
		}
		return out
	}
	sh(filepath.Join(dir, scriptFile))
	got, err := parseStatus(sh("-c", statusScript))
	if err != nil {
		t.Fatal(err)
	}
	return got
}
