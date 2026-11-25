package checkout

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/session"
)

// rocket is the pull request most tests check out.
func rocket() Spec {
	return Spec{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7, Branch: "fix-auth"}}
}

func issue(branch string) Spec {
	return Spec{Target: v1.Target{Kind: v1.TargetIssue, Repo: "acme/rocket", Number: 42, Branch: branch}}
}

func branch(name string) Spec {
	return Spec{Target: v1.Target{Kind: v1.TargetBranch, Repo: "acme/rocket", Branch: name}}
}

func TestCloneScriptClonesTheHeadIntoTheVolume(t *testing.T) {
	got := rocket().CloneScript()
	for _, want := range []string{
		"set -euo pipefail\n",
		"http.version GIT_CONFIG_VALUE_0=HTTP/1.1",
		"http.lowSpeedLimit GIT_CONFIG_VALUE_1=1024",
		"http.lowSpeedTime GIT_CONFIG_VALUE_2=20",
		"rm -rf repo\n",
		"git clone --progress https://github.com/acme/rocket.git repo 2>&1 | lines",
		"\ncd repo\n",
		"git fetch --progress origin pull/7/head 2>&1 | lines",
		"\ngit checkout -B fix-auth FETCH_HEAD",
		`tr '\r' '\n'`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script lacks %q:\n%s", want, got)
		}
	}
	noBranch := Spec{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7}}
	if got := noBranch.CloneScript(); !strings.HasSuffix(got, "git checkout -B pr-7 FETCH_HEAD") {
		t.Errorf("no branch: %s", got)
	}
	if got := rocket().ShellLine(); got != "cd /home/node/workspace/rocket && git status -sb" {
		t.Errorf("shell line = %s", got)
	}
}

func TestCloneScriptQuotesBranchNames(t *testing.T) {
	s := Spec{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7, Branch: "it's;$(rm -rf \x7e)"}}
	if got := s.CloneScript(); !strings.HasSuffix(got, "git checkout -B 'it'\\''s;$(rm -rf \x7e)' FETCH_HEAD") {
		t.Errorf("branch not quoted: %s", got)
	}
	if q := shellQuote("-x"); q != "'-x'" {
		t.Errorf("a leading dash is left bare: %s", q)
	}
}

func TestIssueCloneScriptChecksOutTheRequestedBranch(t *testing.T) {
	s := issue("feat/dark-mode")
	if got := s.Volume(); got != "issue-acme-rocket-42" {
		t.Errorf("volume = %q", got)
	}
	got := s.CloneScript()
	want := "if git show-ref --verify --quiet refs/remotes/origin/feat/dark-mode; then git checkout -B feat/dark-mode --track refs/remotes/origin/feat/dark-mode; else git checkout -B feat/dark-mode; fi"
	if !strings.Contains(got, want) || strings.Contains(got, "pull/") {
		t.Errorf("issue script:\n%s", got)
	}
	if s := issue("").CloneScript(); !strings.Contains(s, "git checkout -B issue-42;") {
		t.Errorf("default branch script:\n%s", s)
	}
	if s := issue("it's;$(rm)").CloneScript(); !strings.Contains(s, `'it'\''s;$(rm)'`) {
		t.Errorf("issue branch not quoted:\n%s", s)
	}
}

func TestBranchCheckoutNamesItsVolumeAndBranch(t *testing.T) {
	s := branch("feat/search")
	if got := s.String(); got != "acme/rocket@feat/search" {
		t.Errorf("String() = %q", got)
	}
	if got := s.Volume(); got != "repo-acme-rocket-feat-search" {
		t.Errorf("Volume() = %q", got)
	}
	if got := branch("").String(); got != "acme/rocket@main" {
		t.Errorf("default String() = %q", got)
	}
	if got := branch("").Volume(); got != "repo-acme-rocket-main" {
		t.Errorf("default Volume() = %q", got)
	}
	if s := branch("").CloneScript(); !strings.Contains(s, "git checkout -B main;") {
		t.Errorf("default branch script:\n%s", s)
	}
}

func TestGitProgressLinesAreTidied(t *testing.T) {
	if cloneDetail("remote: Counting objects:  10% (1/10)") != "Counting objects:  10% (1/10)" || cloneDetail("") != "starting the clone pod" {
		t.Error("clone lines are not tidied")
	}
	if podDetail("Pending ContainerCreating") != "pod Pending · pulling the image" {
		t.Errorf("pod detail = %q", podDetail("Pending ContainerCreating"))
	}
}

// fakeOps records what the pipeline asks of the cluster.
type fakeOps struct {
	mu             sync.Mutex
	exists, cloned bool
	cloneErr       error
	hold           chan struct{} // when set, the clone waits for it
	open           string        // the sandbox already on the volume
	volumeFor      cluster.VolumeFor
	calls          []string
}

func (f *fakeOps) note(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeOps) ops() Ops {
	return Ops{
		PoolTemplate: func(string) (string, error) { return "pi-template", nil },
		SandboxOn: func(string) (string, error) {
			f.note("on volume")
			return f.open, nil
		},
		VolumeState: func(string) (bool, bool, error) {
			f.note("state")
			return f.exists, f.cloned, nil
		},
		CreateVolume: func(name string, target cluster.VolumeFor) error {
			f.volumeFor = target
			f.note("create " + name + " " + target.Repo)
			return nil
		},
		TemplatePod: func(tpl string) (map[string]any, string, error) {
			f.note("template " + tpl)
			return map[string]any{}, "img", nil
		},
		Clone: func(vol, image, _ string, _ time.Duration, progress func(string)) error {
			f.note("clone " + vol + " " + image)
			progress("")
			progress("Receiving objects:  45% (1/2), 1 MiB | 2 MiB/s")
			progress("Receiving objects:  45% (1/2), 1 MiB | 2 MiB/s")
			if f.hold != nil {
				<-f.hold
			}
			return f.cloneErr
		},
		CreateSandbox: func(tpl string, _ map[string]any, vol, mount string) (string, error) {
			f.note("sandbox " + tpl + " " + vol + " " + mount)
			return "pi-pr-1", nil
		},
		WaitPod: func(pod string, _ time.Duration, progress func(string)) error {
			f.note("wait " + pod)
			progress("Pending ContainerCreating")
			return nil
		},
		Ensure: func(pod string) error { f.note("bash " + pod); return nil },
		Send: func(pod string, keys []session.Key) error {
			f.note("type " + keys[0].Text)
			return nil
		},
	}
}

func events(f *fakeOps, s Spec) []v1.CheckoutEvent {
	var evs []v1.CheckoutEvent
	Run(f.ops(), s, "pi-template", func(ev v1.CheckoutEvent) { evs = append(evs, ev) })
	return evs
}

func TestTheCheckoutClonesStartsAndTypesInOrder(t *testing.T) {
	f := &fakeOps{}
	evs := events(f, rocket())
	want := []string{"state", "create pr-acme-rocket-7 acme/rocket", "template pi-template", "clone pr-acme-rocket-7 img",
		"sandbox pi-template pr-acme-rocket-7 /home/node/workspace/rocket", "wait pi-pr-1", "bash pi-pr-1",
		"type cd /home/node/workspace/rocket && git status -sb"}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s", strings.Join(f.calls, "\n"))
	}
	last := evs[len(evs)-1]
	if !last.Done || last.Sandbox != "pi-pr-1" || last.At.IsZero() {
		t.Errorf("last event = %+v", last)
	}
	var progress int
	steps := map[string]bool{}
	for _, ev := range evs {
		if strings.HasPrefix(ev.Detail, "Receiving objects") {
			progress++
		}
		steps[ev.Step] = true
	}
	if progress != 1 {
		t.Errorf("the same progress line came %d times, want once", progress)
	}
	if len(steps) != 4 {
		t.Errorf("went through %d steps, want 4: %v", len(steps), steps)
	}
}

func TestVolumeKindsReachTheCluster(t *testing.T) {
	f := &fakeOps{}
	events(f, issue("feat/login"))
	if f.volumeFor.Kind != cluster.VolumeIssue || f.volumeFor.Number != 42 || f.volumeFor.Branch != "feat/login" {
		t.Errorf("issue volumeFor = %+v", f.volumeFor)
	}
	f = &fakeOps{}
	events(f, branch("feat/search"))
	if f.volumeFor.Kind != cluster.VolumeRepo || f.volumeFor.Number != 0 || f.volumeFor.Branch != "feat/search" {
		t.Errorf("branch volumeFor = %+v", f.volumeFor)
	}
}

func TestAKeptVolumeSwitchesBranchInsteadOfCloning(t *testing.T) {
	for name, s := range map[string]Spec{"issue": issue("feat/login"), "branch": branch("feat/login")} {
		f := &fakeOps{exists: true, cloned: true}
		evs := events(f, s)
		for _, c := range f.calls {
			if strings.HasPrefix(c, "clone") || strings.HasPrefix(c, "create") {
				t.Errorf("%s: a kept volume got %q", name, c)
			}
		}
		want := "type cd /home/node/workspace/rocket && (git checkout feat/login 2>/dev/null || git checkout -b feat/login) && git status -sb"
		if got := f.calls[len(f.calls)-1]; got != want {
			t.Errorf("%s: shell command = %q, want %q", name, got, want)
		}
		if !evs[len(evs)-1].Done {
			t.Errorf("%s: the checkout did not finish", name)
		}
	}
}

func TestAHarnessAndModelStartInTheClone(t *testing.T) {
	s := rocket()
	s.Harness, s.Model = "claude", "opus"
	f := &fakeOps{}
	evs := events(f, s)
	want := "type cd /home/node/workspace/rocket && git status -sb && claude --model opus"
	if got := f.calls[len(f.calls)-1]; got != want {
		t.Errorf("shell command = %q, want %q", got, want)
	}
	var starting string
	for _, ev := range evs {
		if ev.Step == v1.StepShell && ev.Detail != "" {
			starting = ev.Detail
		}
	}
	if starting != "starting claude (opus)" {
		t.Errorf("shell detail = %q", starting)
	}
}

func TestACloneThatFailsStopsTheCheckout(t *testing.T) {
	f := &fakeOps{cloneErr: errors.New("repository not found")}
	evs := events(f, rocket())
	last := evs[len(evs)-1]
	if last.Error == "" || last.Step != v1.StepClone {
		t.Fatalf("last event = %+v", last)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "sandbox") {
			t.Error("a sandbox started after the clone failed")
		}
	}
}

func request() v1.CheckoutRequest {
	return v1.CheckoutRequest{Target: rocket().Target, Pool: "pi-pool"}
}

func collect(t *testing.T, m *Manager, id string) []v1.CheckoutEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var got []v1.CheckoutEvent
	if err := m.Follow(ctx, id, func(ev v1.CheckoutEvent) error { got = append(got, ev); return nil }); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAManagerRunsACheckoutAndReplaysItToALateFollower(t *testing.T) {
	f := &fakeOps{}
	m := NewManager(f.ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	co, err := m.Start(request())
	if err != nil {
		t.Fatal(err)
	}
	if co.State != v1.CheckoutRunning || co.Volume != "pr-acme-rocket-7" || co.ID == "" {
		t.Fatalf("started = %+v", co)
	}
	first := collect(t, m, co.ID)
	if last := first[len(first)-1]; !last.Done {
		t.Fatalf("last = %+v", last)
	}
	late := collect(t, m, co.ID) // after it finished
	if len(late) == 0 || !late[len(late)-1].Done {
		t.Fatalf("a follower who came late got %+v", late)
	}
	finished := map[string]bool{}
	for _, ev := range late {
		if ev.Finished {
			finished[ev.Step] = true
		}
	}
	for _, step := range []string{v1.StepVolume, v1.StepClone, v1.StepSandbox} {
		if !finished[step] {
			t.Errorf("the late follower never heard that %s finished: %+v", step, late)
		}
	}
	got, err := m.Get(co.ID)
	if err != nil || got.State != v1.CheckoutDone || got.Sandbox != "pi-pr-1" || got.Last == nil || !got.Last.Done {
		t.Errorf("Get = %+v, %v", got, err)
	}
}

func TestTheSameTargetIsNotCheckedOutTwiceAtOnce(t *testing.T) {
	f := &fakeOps{hold: make(chan struct{})}
	m := NewManager(f.ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	a, err := m.Start(request())
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Start(request())
	if err != nil || a.ID != b.ID {
		t.Fatalf("the second request got %+v, %v; want the first, %s", b, err, a.ID)
	}
	close(f.hold)
	collect(t, m, a.ID)
	c, err := m.Start(request())
	if err != nil || c.ID == a.ID {
		t.Errorf("a finished checkout was handed out again: %+v, %v", c, err)
	}
}

func TestAFailedCheckoutIsReportedAsFailed(t *testing.T) {
	f := &fakeOps{cloneErr: errors.New("repository not found")}
	m := NewManager(f.ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	co, err := m.Start(request())
	if err != nil {
		t.Fatal(err)
	}
	collect(t, m, co.ID)
	got, err := m.Get(co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != v1.CheckoutFailed || got.Last == nil || !strings.Contains(got.Last.Error, "repository not found") {
		t.Errorf("Get = %+v", got)
	}
}

func TestABadRequestIsRefusedBeforeAnythingRuns(t *testing.T) {
	f := &fakeOps{}
	m := NewManager(f.ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	bad := []v1.CheckoutRequest{
		{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket"}, Pool: "p"},
		{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "not a repo", Number: 1}, Pool: "p"},
		{Target: v1.Target{Kind: v1.TargetBranch, Repo: "acme/rocket", Branch: "a..b"}, Pool: "p"},
		{Target: v1.Target{Kind: "wat", Repo: "acme/rocket"}, Pool: "p"},
		{Target: v1.Target{Kind: v1.TargetBranch, Repo: "acme/rocket"}},
	}
	for _, req := range bad {
		var inv *InvalidError
		if _, err := m.Start(req); !errors.As(err, &inv) {
			t.Errorf("%+v: err = %v, want an InvalidError", req, err)
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("a refused request still touched the cluster: %v", f.calls)
	}
}

func TestFollowingAnUnknownCheckoutFails(t *testing.T) {
	m := NewManager((&fakeOps{}).ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	if err := m.Follow(context.Background(), "nope", func(v1.CheckoutEvent) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestHarnessForPoolResolvesAllHarnesses(t *testing.T) {
	for pool, want := range map[string]string{
		"claude-code-pool": "claude-code", "codex-pool": "codex", "opencode-pool": "opencode",
		"agy-pool": "agy", "ompig-slim-pool": "ompig-slim", "pi-pool": "pi",
	} {
		if got := ForPool(pool, strings.Replace(pool, "pool", "template", 1), ""); got.Name != want || len(got.Models) == 0 || got.Command == "" {
			t.Errorf("%s: harness = %+v, want %s", pool, got, want)
		}
	}
	if got := ForPool("custom-pool", "", ""); got.Command != "" || len(got.Models) != 1 {
		t.Errorf("an unknown pool should leave bash open: %+v", got)
	}
}

// A volume holds one sandbox at a time: asking again for a target that is open
// goes back to its sandbox instead of starting a second one on the same volume.
func TestACheckoutOfAnOpenTargetGoesBackToItsSandbox(t *testing.T) {
	f := &fakeOps{open: "pi-pr-9"}
	m := NewManager(f.ops())
	t.Cleanup(func() { m.Close(context.Background()) })
	co, err := m.Start(request())
	if err != nil {
		t.Fatal(err)
	}
	if co.State != v1.CheckoutDone || co.Sandbox != "pi-pr-9" || co.Last == nil || !co.Last.Done {
		t.Fatalf("checkout = %+v", co)
	}
	for _, c := range f.calls {
		if c != "on volume" {
			t.Errorf("an open target still ran %q", c)
		}
	}
	events := collect(t, m, co.ID)
	if len(events) != 1 || !events[0].Done || events[0].Sandbox != "pi-pr-9" {
		t.Errorf("the reopened checkout can be followed: %+v", events)
	}
}
