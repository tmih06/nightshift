package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

var rocket = Checkout{Repo: "acme/rocket", Number: 7, Branch: "fix-auth"}

func TestCloneScriptClonesTheHeadIntoTheVolume(t *testing.T) {
	got := rocket.CloneScript()
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
	if got := (Checkout{Repo: "acme/rocket", Number: 7}).CloneScript(); !strings.HasSuffix(got, "git checkout -B pr-7 FETCH_HEAD") {
		t.Errorf("no branch: %s", got)
	}
	if got := rocket.ShellLine(); got != "cd /home/node/workspace/rocket && git status -sb" {
		t.Errorf("shell line = %s", got)
	}
}

func TestCloneScriptQuotesBranchNames(t *testing.T) {
	got := Checkout{Repo: "acme/rocket", Number: 7, Branch: "it's;$(rm -rf ~)"}.CloneScript()
	if !strings.HasSuffix(got, `git checkout -B 'it'\''s;$(rm -rf ~)' FETCH_HEAD`) {
		t.Errorf("branch not quoted: %s", got)
	}
	if q := shellQuote("-x"); q != "'-x'" {
		t.Errorf("a leading dash is left bare: %s", q)
	}
}

func TestIssueCloneScriptChecksOutTheRequestedBranch(t *testing.T) {
	issue := Checkout{Repo: "acme/rocket", Number: 42, Branch: "feat/dark-mode", Kind: CheckoutIssue}
	if got := issue.Volume(); got != "issue-acme-rocket-42" {
		t.Errorf("volume = %q", got)
	}
	got := issue.CloneScript()
	for _, want := range []string{
		"set -euo pipefail\n",
		"http.version GIT_CONFIG_VALUE_0=HTTP/1.1",
		"rm -rf repo\n",
		"git clone --progress https://github.com/acme/rocket.git repo 2>&1 | lines",
		"\ncd repo\n",
		"if git show-ref --verify --quiet refs/remotes/origin/feat/dark-mode; then git checkout -B feat/dark-mode --track refs/remotes/origin/feat/dark-mode; else git checkout -B feat/dark-mode; fi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "pull/") {
		t.Errorf("issue script fetches a pull request ref:\n%s", got)
	}
	defaultIssue := Checkout{Repo: "acme/rocket", Number: 42, Kind: CheckoutIssue}
	if s := defaultIssue.CloneScript(); !strings.Contains(s, "git checkout -B issue-42;") {
		t.Errorf("default branch script:\n%s", s)
	}
	if s := (Checkout{Repo: "acme/rocket", Number: 42, Branch: "it's;$(rm)", Kind: CheckoutIssue}).CloneScript(); !strings.Contains(s, `'it'\''s;$(rm)'`) {
		t.Errorf("issue branch not quoted:\n%s", s)
	}
}

func TestIssueCheckoutClonesFreshAndSwitchesBranchOnKeptVolume(t *testing.T) {
	issue := Checkout{Repo: "acme/rocket", Number: 42, Branch: "feat/login", Kind: CheckoutIssue}

	fresh := &fakeOps{}
	var evs []checkoutEvent
	runCheckout(fresh.ops(), issue, "pi-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	wantFresh := []string{
		"state",
		"create issue-acme-rocket-42 acme/rocket",
		"template pi-template",
		"clone issue-acme-rocket-42 img",
		"sandbox pi-template issue-acme-rocket-42 /home/node/workspace/rocket",
		"wait pi-pr-1",
		"bash pi-pr-1",
		"type cd /home/node/workspace/rocket && git status -sb",
	}
	if strings.Join(fresh.calls, "\n") != strings.Join(wantFresh, "\n") {
		t.Errorf("fresh calls:\n%s", strings.Join(fresh.calls, "\n"))
	}
	if fresh.volumeFor.Kind != cluster.VolumeIssue || fresh.volumeFor.Number != 42 || fresh.volumeFor.Branch != "feat/login" {
		t.Errorf("volumeFor = %+v", fresh.volumeFor)
	}
	if !evs[len(evs)-1].done {
		t.Errorf("fresh checkout did not finish: %+v", evs[len(evs)-1])
	}

	kept := &fakeOps{exists: true, cloned: true}
	evs = nil
	runCheckout(kept.ops(), issue, "pi-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	wantLine := "type cd /home/node/workspace/rocket && (git checkout feat/login 2>/dev/null || git checkout -b feat/login) && git status -sb"
	if got := kept.calls[len(kept.calls)-1]; got != wantLine {
		t.Errorf("kept shell command = %q, want %q", got, wantLine)
	}
	if !evs[len(evs)-1].done {
		t.Errorf("kept checkout did not finish: %+v", evs[len(evs)-1])
	}
}

func TestRepoCheckoutScriptVolumeAndRun(t *testing.T) {
	repo := Checkout{Repo: "acme/rocket", Branch: "feat/search", Kind: CheckoutRepo}
	if got := repo.String(); got != "acme/rocket@feat/search" {
		t.Errorf("String() = %q, want acme/rocket@feat/search", got)
	}
	if got := repo.Volume(); got != "repo-acme-rocket-feat-search" {
		t.Errorf("Volume() = %q, want repo-acme-rocket-feat-search", got)
	}
	got := repo.CloneScript()
	for _, want := range []string{
		"set -euo pipefail\n",
		"git clone --progress https://github.com/acme/rocket.git repo 2>&1 | lines",
		"\ncd repo\n",
		"if git show-ref --verify --quiet refs/remotes/origin/feat/search; then git checkout -B feat/search --track refs/remotes/origin/feat/search; else git checkout -B feat/search; fi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("repo script lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "pull/") {
		t.Errorf("repo script fetches a pull request ref:\n%s", got)
	}

	defaultRepo := Checkout{Repo: "acme/rocket", Kind: CheckoutRepo}
	if got := defaultRepo.String(); got != "acme/rocket@main" {
		t.Errorf("default String() = %q, want acme/rocket@main", got)
	}
	if got := defaultRepo.Volume(); got != "repo-acme-rocket-main" {
		t.Errorf("default Volume() = %q, want repo-acme-rocket-main", got)
	}
	if s := defaultRepo.CloneScript(); !strings.Contains(s, "git checkout -B main;") {
		t.Errorf("default repo script:\n%s", s)
	}

	fresh := &fakeOps{}
	var evs []checkoutEvent
	runCheckout(fresh.ops(), repo, "pi-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	if fresh.volumeFor.Kind != cluster.VolumeRepo || fresh.volumeFor.Repo != "acme/rocket" || fresh.volumeFor.Branch != "feat/search" || fresh.volumeFor.Number != 0 {
		t.Errorf("repo volumeFor = %+v", fresh.volumeFor)
	}
	if got := fresh.calls[len(fresh.calls)-1]; got != "type cd /home/node/workspace/rocket && git status -sb" {
		t.Errorf("fresh repo shell command = %q", got)
	}
	if !evs[len(evs)-1].done {
		t.Errorf("fresh repo checkout did not finish: %+v", evs[len(evs)-1])
	}

	kept := &fakeOps{exists: true, cloned: true}
	evs = nil
	runCheckout(kept.ops(), repo, "pi-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	wantLine := "type cd /home/node/workspace/rocket && (git checkout feat/search 2>/dev/null || git checkout -b feat/search) && git status -sb"
	if got := kept.calls[len(kept.calls)-1]; got != wantLine {
		t.Errorf("kept repo shell command = %q, want %q", got, wantLine)
	}
	if !evs[len(evs)-1].done {
		t.Errorf("kept repo checkout did not finish: %+v", evs[len(evs)-1])
	}
}

func TestGitProgressIsRead(t *testing.T) {
	phase, pct, rest, ok := parseProgress("Receiving objects:  49% (48647/99278), 34.21 MiB | 10.97 MiB/s")
	if !ok || phase != "Receiving objects" || pct != 49 || rest != "34.21 MiB | 10.97 MiB/s" {
		t.Errorf("got %q %d %q %v", phase, pct, rest, ok)
	}
	phase, pct, rest, ok = parseProgress("Resolving deltas: 100% (68158/68158), done.")
	if !ok || phase != "Resolving deltas" || pct != 100 || rest != "" {
		t.Errorf("done line: %q %d %q %v", phase, pct, rest, ok)
	}
	if _, _, _, ok := parseProgress("Cloning into 'repo'..."); ok {
		t.Error("a plain line read as progress")
	}
	if cloneDetail("remote: Counting objects:  10% (1/10)") != "Counting objects:  10% (1/10)" || cloneDetail("") != "starting the clone pod" {
		t.Error("clone lines are not tidied")
	}
	if podDetail("Pending ContainerCreating") != "pod Pending · pulling the image" {
		t.Errorf("pod detail = %q", podDetail("Pending ContainerCreating"))
	}
}

// fakeOps records what the pipeline asks of the cluster.
type fakeOps struct {
	exists, cloned bool
	cloneErr       error
	volumeFor      cluster.VolumeFor
	calls          []string
}

func (f *fakeOps) ops() checkoutOps {
	return checkoutOps{
		volumeState: func(string) (bool, bool, error) { f.calls = append(f.calls, "state"); return f.exists, f.cloned, nil },
		createVolume: func(name string, target cluster.VolumeFor) error {
			f.volumeFor = target
			f.calls = append(f.calls, "create "+name+" "+target.Repo)
			return nil
		},
		templatePod: func(tpl string) (map[string]any, string, error) {
			f.calls = append(f.calls, "template "+tpl)
			return map[string]any{}, "img", nil
		},
		clone: func(vol, image, script string, _ time.Duration, progress func(string)) error {
			f.calls = append(f.calls, "clone "+vol+" "+image)
			progress("")
			progress("Receiving objects:  45% (1/2), 1 MiB | 2 MiB/s")
			progress("Receiving objects:  45% (1/2), 1 MiB | 2 MiB/s")
			return f.cloneErr
		},
		createSandbox: func(tpl string, _ map[string]any, vol, mount string) (string, error) {
			f.calls = append(f.calls, "sandbox "+tpl+" "+vol+" "+mount)
			return "pi-pr-1", nil
		},
		waitPod: func(pod string, _ time.Duration, progress func(string)) error {
			f.calls = append(f.calls, "wait "+pod)
			progress("Pending ContainerCreating")
			return nil
		},
		ensure: func(pod string) error { f.calls = append(f.calls, "bash "+pod); return nil },
		send: func(pod string, keys []session.Key) error {
			f.calls = append(f.calls, "type "+keys[0].Text)
			return nil
		},
	}
}

func run(f *fakeOps) []checkoutEvent {
	var evs []checkoutEvent
	runCheckout(f.ops(), rocket, "pi-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	return evs
}

func TestTheCheckoutClonesStartsAndTypesInOrder(t *testing.T) {
	f := &fakeOps{}
	evs := run(f)
	want := []string{"state", "create pr-acme-rocket-7 acme/rocket", "template pi-template", "clone pr-acme-rocket-7 img",
		"sandbox pi-template pr-acme-rocket-7 /home/node/workspace/rocket", "wait pi-pr-1", "bash pi-pr-1",
		"type cd /home/node/workspace/rocket && git status -sb"}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s", strings.Join(f.calls, "\n"))
	}
	last := evs[len(evs)-1]
	if !last.done || last.pod != "pi-pr-1" {
		t.Errorf("last event = %+v", last)
	}
	var progress, steps int
	seen := map[step]bool{}
	for _, ev := range evs {
		if strings.HasPrefix(ev.detail, "Receiving objects") {
			progress++
		}
		if !seen[ev.step] {
			seen[ev.step] = true
			steps++
		}
	}
	if progress != 1 {
		t.Errorf("the same progress line came %d times, want once", progress)
	}
	if steps != int(stepCount) {
		t.Errorf("went through %d steps, want %d", steps, stepCount)
	}
}

func TestAKeptCloneIsNotClonedAgain(t *testing.T) {
	f := &fakeOps{exists: true, cloned: true}
	evs := run(f)
	for _, c := range f.calls {
		if strings.HasPrefix(c, "clone") || strings.HasPrefix(c, "create") {
			t.Errorf("a kept volume got %q", c)
		}
	}
	if !evs[len(evs)-1].done {
		t.Error("the checkout did not finish")
	}
}

func TestACloneThatFailsStopsTheCheckout(t *testing.T) {
	f := &fakeOps{cloneErr: errors.New("repository not found")}
	evs := run(f)
	last := evs[len(evs)-1]
	if last.err == nil || last.step != stepClone {
		t.Fatalf("last event = %+v", last)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "sandbox") {
			t.Error("a sandbox started after the clone failed")
		}
	}
}

func running() model {
	m := baseModel()
	m.pools["pi-pool"] = cluster.Pool{Name: "pi-pool", Template: "pi-template"}
	m, _ = m.CheckOut("pi-pool", rocket)
	return m
}

func feed(m model, evs ...checkoutEvent) (model, tea.Cmd) {
	var cmd tea.Cmd
	for _, ev := range evs {
		m, cmd = m.Update(checkoutMsg{ev: ev, events: make(chan checkoutEvent)})
	}
	return m, cmd
}

func TestCheckOutShowsACardOfItsSteps(t *testing.T) {
	m := running()
	if m.checkout == nil {
		t.Fatal("no checkout in flight")
	}
	m, cmd := feed(m,
		checkoutEvent{step: stepVolume, finished: true, detail: "pr-acme-rocket-7 · new"},
		checkoutEvent{step: stepClone, detail: "Receiving objects:  45% (1/2), 1 MiB | 2 MiB/s"})
	if cmd == nil {
		t.Fatal("the checkout stopped listening")
	}
	view := m.plainView()
	for _, want := range []string{"Checking out acme/rocket#7", "on pi-template", "✓ Volume", "pr-acme-rocket-7 · new",
		"Clone    Receiving objects", "█████████░░░", "45% · 1 MiB | 2 MiB/s", "○ Sandbox", "○ Shell"} {
		if !strings.Contains(view, want) {
			t.Errorf("the card lacks %q:\n%s", want, view)
		}
	}
	rows := strings.Split(view, "\n")
	if len(rows) != m.height {
		t.Errorf("view is %d rows, want %d", len(rows), m.height)
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w > m.width {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
	if line := m.CheckoutLine(); !strings.Contains(line, "acme/rocket#7 · clone 2/4 · Receiving objects") {
		t.Errorf("line = %q", line)
	}
}

func TestOneCheckoutAtATime(t *testing.T) {
	m, cmd := running().CheckOut("pi-pool", rocket)
	if cmd != nil || !strings.Contains(m.status, "one at a time") {
		t.Errorf("status=%q cmd=%v", m.status, cmd != nil)
	}
}

func TestAFailedStepSaysWhichAndDropsTheCard(t *testing.T) {
	m, _ := feed(running(), checkoutEvent{step: stepClone, err: errors.New("repository not found")})
	if m.checkout != nil || !m.statusBad || !strings.Contains(m.status, "clone failed for acme/rocket#7: repository not found") {
		t.Errorf("checkout=%v status=%q", m.checkout != nil, m.status)
	}
	if strings.Contains(m.plainView(), "Checking out") {
		t.Error("the card is still drawn")
	}
}

// Once done, the new sandbox's shell takes the keyboard when the fleet
// shows it.
func TestADoneCheckoutFocusesItsShell(t *testing.T) {
	m := running()
	m.section = sectionWarm
	m, _ = feed(m, checkoutEvent{step: stepShell, pod: "pi-pr-1", done: true})
	if m.checkout != nil || m.pendingPod != "pi-pr-1" || m.pendingCheckout == nil {
		t.Fatalf("checkout=%v pending=%q", m.checkout != nil, m.pendingPod)
	}
	m.boxes = append(m.boxes, cluster.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7", Volume: "pr-acme-rocket-7",
		Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256})
	m.sessions["pi-pr-1"] = true
	m.hist = map[string]usageHistory{}
	m, _ = m.Update(loadedMsg{boxes: m.boxes, sessions: m.sessions, metricsOK: true})
	if m.selected != "pi-pr-1" || m.focus != focusShell || m.section != sectionAll {
		t.Errorf("selected=%q focus=%v section=%d", m.selected, m.focus, m.section)
	}
	if !strings.Contains(m.plainView(), "volume pr-acme-rocket-7") {
		t.Error("the sidebar does not name the volume")
	}
}

func TestWarmPoolsAreByName(t *testing.T) {
	m := baseModel().WithFleet(nil, map[string]cluster.Pool{"pi-pool": {Name: "pi-pool"}, "codex-pool": {Name: "codex-pool"}})
	pools := m.WarmPools()
	if len(pools) != 2 || pools[0].Name != "codex-pool" || pools[1].Name != "pi-pool" {
		t.Errorf("pools = %v", pools)
	}
}

func TestCheckOutNeedsAPoolWithATemplate(t *testing.T) {
	next, cmd := baseModel().CheckOut("gone-pool", rocket)
	if cmd != nil || !next.statusBad || next.checkout != nil {
		t.Errorf("a pool without a template went ahead: %q", next.status)
	}
}

// A sandbox already on the volume is where the work goes on: no second one.
func TestCheckOutReturnsToTheSandboxOnTheVolume(t *testing.T) {
	m := baseModel()
	m.section = sectionWarm
	m.boxes = append(m.boxes, cluster.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7",
		Volume: "pr-acme-rocket-7", Phase: "Running"})
	next, cmd := m.CheckOut("agent-pool", rocket)
	if cmd == nil || next.selected != "pi-pr-1" || next.section != sectionAll || next.checkout != nil {
		t.Fatalf("selected=%q section=%d cmd=%v", next.selected, next.section, cmd != nil)
	}
	if !strings.Contains(next.status, "already open") {
		t.Errorf("status = %q", next.status)
	}
}

func onVolume() model {
	m := baseModel()
	m.boxes = append(m.boxes, cluster.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7", Volume: "pr-acme-rocket-7", Phase: "Running"})
	m.selected = "pi-pr-1"
	m, _ = m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	return m
}

func TestDestroyingASandboxOnAVolumeAsksAboutTheVolume(t *testing.T) {
	m := onVolume()
	if m.destroy == nil || !m.Capturing() {
		t.Fatal("x did not open the dialog")
	}
	view := m.plainView()
	for _, want := range []string{"Destroy sandbox pi-pr-1?", "Volume pr-acme-rocket-7 holds the clone",
		"Destroy the sandbox, keep the volume", "Destroy the sandbox and the volume", "esc cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dialog lacks %q:\n%s", want, view)
		}
	}
	if got := len(strings.Split(view, "\n")); got != m.height {
		t.Errorf("view is %d rows, want %d", got, m.height)
	}

	closed, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if closed.destroy != nil || cmd != nil {
		t.Error("esc did not just close the dialog")
	}

	kept, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || kept.destroy != nil || !strings.Contains(kept.status, "keeping volume pr-acme-rocket-7") {
		t.Errorf("enter: status=%q cmd=%v", kept.status, cmd != nil)
	}
	dropped, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	dropped, cmd = dropped.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || !strings.Contains(dropped.status, "destroying pi-pr-1 and volume pr-acme-rocket-7") {
		t.Errorf("down enter: status=%q cmd=%v", dropped.status, cmd != nil)
	}

	done, _ := m.Update(deletedMsg{sandbox: "pi-pr-1", volume: "pr-acme-rocket-7", volumeGone: true})
	if !strings.Contains(done.status, "destroyed sandbox pi-pr-1 and volume pr-acme-rocket-7") {
		t.Errorf("status = %q", done.status)
	}
	done, _ = m.Update(deletedMsg{sandbox: "pi-pr-1", volume: "pr-acme-rocket-7"})
	if !strings.Contains(done.status, "volume pr-acme-rocket-7 kept") {
		t.Errorf("status = %q", done.status)
	}
}

func TestTheDestroyDialogTakesClicks(t *testing.T) {
	m := onVolume()
	p := m.destroyPlaced()
	next, cmd := m.Update(tea.MouseClickMsg{X: p.X + 3, Y: p.OptionY(dropVolume), Button: tea.MouseLeft})
	if cmd == nil || !strings.Contains(next.status, "and volume") {
		t.Errorf("click on drop: status=%q", next.status)
	}
	next, cmd = m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if next.destroy != nil || cmd != nil {
		t.Error("a click outside did not just close the dialog")
	}
}

func TestBoxesReturnsShallowCopy(t *testing.T) {
	empty := baseModel()
	empty.boxes = nil
	if got := empty.Boxes(); got != nil {
		t.Fatalf("empty Boxes() = %v, want nil", got)
	}

	m := baseModel()
	got := m.Boxes()
	if len(got) != len(m.boxes) {
		t.Fatalf("len(Boxes()) = %d, want %d", len(got), len(m.boxes))
	}
	orig := m.boxes[0].Name
	got[0].Name = "mutated"
	if m.boxes[0].Name != orig {
		t.Errorf("mutating Boxes() changed model: got %q, want %q", m.boxes[0].Name, orig)
	}
}

func TestFocusPodSwitchesSectionSelectsAndAttaches(t *testing.T) {
	m := baseModel()
	m.boxes = []cluster.Sandbox{
		{Name: "pi-pool-a", OwnerKind: "SandboxWarmPool", Owner: "pi-pool", Phase: "Running"},
		{Name: "pi-repo-1", Owner: "repo-acme-rocket-main", Volume: "repo-acme-rocket-main", Phase: "Running"},
		{Name: "pi-repo-pending", Owner: "repo-acme-rocket-dev", Volume: "repo-acme-rocket-dev", Phase: "Pending"},
	}
	m.selected = "pi-pool-a"
	m.section = sectionWarm

	// Unknown pod is a no-op.
	same, cmd := m.FocusPod("does-not-exist")
	if cmd != nil || same.selected != "pi-pool-a" || same.section != sectionWarm || same.focus != focusNav {
		t.Fatalf("missing pod changed state: selected=%q section=%d focus=%v cmd=%v", same.selected, same.section, same.focus, cmd != nil)
	}

	// Claimed & running pod hidden by sectionWarm switches to sectionAll and attaches.
	next, cmd := m.FocusPod("pi-repo-1")
	if cmd == nil || next.selected != "pi-repo-1" || next.section != sectionAll {
		t.Fatalf("claimed running without session: selected=%q section=%d cmd=%v", next.selected, next.section, cmd != nil)
	}

	// Claimed & running pod with an active session focuses the shell and watches it.
	mWithSess := m
	mWithSess.sessions = map[string]bool{"pi-repo-1": true}
	next, cmd = mWithSess.FocusPod("pi-repo-1")
	if cmd == nil || next.selected != "pi-repo-1" || next.section != sectionAll || next.focus != focusShell {
		t.Fatalf("claimed running with session: selected=%q section=%d focus=%v cmd=%v", next.selected, next.section, next.focus, cmd != nil)
	}

	// Claimed pod already visible in sectionClaimed keeps sectionClaimed.
	mClaimed := mWithSess
	mClaimed.section = sectionClaimed
	next, cmd = mClaimed.FocusPod("pi-repo-1")
	if cmd == nil || next.section != sectionClaimed || next.focus != focusShell {
		t.Fatalf("visible claimed pod changed section: section=%d focus=%v cmd=%v", next.section, next.focus, cmd != nil)
	}

	// Claimed but non-running pod selects without attaching.
	next, cmd = m.FocusPod("pi-repo-pending")
	if cmd != nil || next.selected != "pi-repo-pending" || next.section != sectionAll || next.focus != focusNav {
		t.Fatalf("pending claimed pod: selected=%q section=%d focus=%v cmd=%v", next.selected, next.section, next.focus, cmd != nil)
	}

	// Warm pod hidden by sectionClaimed switches to sectionAll and does not attach.
	mWarm := m
	mWarm.section = sectionClaimed
	next, cmd = mWarm.FocusPod("pi-pool-a")
	if cmd != nil || next.selected != "pi-pool-a" || next.section != sectionAll || next.focus != focusNav {
		t.Fatalf("warm pod: selected=%q section=%d focus=%v cmd=%v", next.selected, next.section, next.focus, cmd != nil)
	}
}

func TestHarnessForPoolResolvesAllHarnesses(t *testing.T) {
	cases := []struct {
		pool    cluster.Pool
		wantCmd string
		wantLbl string
	}{
		{cluster.Pool{Name: "claude-code-pool", Template: "claude-code-template"}, "claude", "Claude Code"},
		{cluster.Pool{Name: "codex-pool", Template: "codex-template"}, "codex", "Codex"},
		{cluster.Pool{Name: "opencode-pool", Template: "opencode-template"}, "opencode", "OpenCode"},
		{cluster.Pool{Name: "pi-pool", Template: "pi-template"}, "pi", "Pi"},
		{cluster.Pool{Name: "custom-pool", TemplateYAML: "image: ghcr.io/tmih06/nightshift-pi:latest"}, "pi", "Pi"},
		{cluster.Pool{Name: "api-pool", Template: "api-template"}, "", "api-template"},
	}
	for _, tc := range cases {
		h := HarnessForPool(tc.pool)
		if h.Command != tc.wantCmd || h.Label != tc.wantLbl {
			t.Errorf("HarnessForPool(%q) = (%q, %q), want (%q, %q)", tc.pool.Name, h.Command, h.Label, tc.wantCmd, tc.wantLbl)
		}
		if len(h.Models) == 0 {
			t.Errorf("HarnessForPool(%q) returned no model options", tc.pool.Name)
		}
	}
}

func TestCheckoutLaunchesHarnessWithSelectedModel(t *testing.T) {
	co := Checkout{
		Repo:    "acme/rocket",
		Number:  7,
		Branch:  "fix-auth",
		Harness: "codex",
		Model:   "gpt-5.3-codex",
	}
	if got := co.ShellLine(); got != "cd /home/node/workspace/rocket && git status -sb && codex --model gpt-5.3-codex" {
		t.Errorf("ShellLine() = %q", got)
	}

	// Default model omits --model flag.
	coDefault := Checkout{Repo: "acme/rocket", Number: 7, Harness: "claude"}
	if got := coDefault.ShellLine(); got != "cd /home/node/workspace/rocket && git status -sb && claude" {
		t.Errorf("default ShellLine() = %q", got)
	}

	// Model ID with special characters is shell-quoted.
	coQuoted := Checkout{Repo: "acme/rocket", Number: 7, Harness: "pi", Model: "a b;rm"}
	if got := coQuoted.ShellLine(); !strings.HasSuffix(got, " && pi --model 'a b;rm'") {
		t.Errorf("quoted ShellLine() = %q", got)
	}

	// Pipeline types the launch line and reports harness+model on the shell step.
	f := &fakeOps{}
	var evs []checkoutEvent
	runCheckout(f.ops(), co, "codex-template", func(ev checkoutEvent) { evs = append(evs, ev) })
	wantLastCall := "type cd /home/node/workspace/rocket && git status -sb && codex --model gpt-5.3-codex"
	if got := f.calls[len(f.calls)-1]; got != wantLastCall {
		t.Errorf("last call = %q, want %q", got, wantLastCall)
	}
	foundStart := false
	for _, ev := range evs {
		if ev.step == stepShell && strings.Contains(ev.detail, "starting codex (gpt-5.3-codex)") {
			foundStart = true
		}
	}
	if !foundStart {
		t.Errorf("stepShell detail did not name harness+model: %+v", evs)
	}

	// Checkout card displays the selected harness and model.
	m := baseModel()
	m.pools["codex-pool"] = cluster.Pool{Name: "codex-pool", Template: "codex-template"}
	m, _ = m.CheckOut("codex-pool", co)
	if view := m.plainView(); !strings.Contains(view, "on codex-template · codex (gpt-5.3-codex)") {
		t.Errorf("checkout card missing harness/model:\n%s", view)
	}
}
