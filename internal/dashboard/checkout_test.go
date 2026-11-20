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
		"\nrm -rf repo\n",
		"\ngit clone --progress https://github.com/acme/rocket.git repo 2>&1 | lines\n",
		"\ncd repo\n",
		"\ngit fetch --progress origin pull/7/head 2>&1 | lines\n",
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
	calls          []string
}

func (f *fakeOps) ops() checkoutOps {
	return checkoutOps{
		volumeState: func(string) (bool, bool, error) { f.calls = append(f.calls, "state"); return f.exists, f.cloned, nil },
		createVolume: func(name string, pr cluster.VolumeFor) error {
			f.calls = append(f.calls, "create "+name+" "+pr.Repo)
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
