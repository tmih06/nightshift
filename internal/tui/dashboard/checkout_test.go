package dashboard

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

// rocket is the pull request most tests check out.
func rocket() Checkout {
	return Checkout{Repo: "acme/rocket", Number: 7, Branch: "fix-auth"}
}

func running() model {
	m := baseModel()
	m.pools["pi-pool"] = fleet.Pool{Name: "pi-pool", Template: "pi-template"}
	m, _ = m.CheckOut("pi-pool", rocket())
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
	m, cmd := running().CheckOut("pi-pool", rocket())
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
	m.boxes = append(m.boxes, fleet.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7", Volume: "pr-acme-rocket-7",
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
	m := baseModel().WithFleet(nil, map[string]fleet.Pool{"pi-pool": {Name: "pi-pool"}, "codex-pool": {Name: "codex-pool"}})
	pools := m.WarmPools()
	if len(pools) != 2 || pools[0].Name != "codex-pool" || pools[1].Name != "pi-pool" {
		t.Errorf("pools = %v", pools)
	}
}

func TestCheckOutNeedsAPoolWithATemplate(t *testing.T) {
	next, cmd := baseModel().CheckOut("gone-pool", rocket())
	if cmd != nil || !next.statusBad || next.checkout != nil {
		t.Errorf("a pool without a template went ahead: %q", next.status)
	}
}

// A sandbox already on the volume is where the work goes on: no second one.
func TestCheckOutReturnsToTheSandboxOnTheVolume(t *testing.T) {
	m := baseModel()
	m.section = sectionWarm
	m.boxes = append(m.boxes, fleet.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7",
		Volume: "pr-acme-rocket-7", Phase: "Running",
		Target: &v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7}})
	next, cmd := m.CheckOut("agent-pool", rocket())
	if cmd == nil || next.selected != "pi-pr-1" || next.section != sectionAll || next.checkout != nil {
		t.Fatalf("selected=%q section=%d cmd=%v", next.selected, next.section, cmd != nil)
	}
	if !strings.Contains(next.status, "already open") {
		t.Errorf("status = %q", next.status)
	}
}

func onVolume() model {
	m := baseModel()
	m.boxes = append(m.boxes, fleet.Sandbox{Name: "pi-pr-1", Owner: "pr-acme-rocket-7", Volume: "pr-acme-rocket-7", Phase: "Running"})
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
	dropped, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
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
	m.boxes = []fleet.Sandbox{
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

func TestCheckoutAsksTheServicesForWhatTheTargetIs(t *testing.T) {
	req := Checkout{Repo: "acme/rocket", Number: 7, Branch: "fix-auth", Harness: "codex", Model: "gpt-5.3-codex"}.Request("codex-pool")
	want := v1.CheckoutRequest{
		Target:  v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7, Branch: "fix-auth"},
		Pool:    "codex-pool",
		Harness: "codex",
		Model:   "gpt-5.3-codex",
	}
	if req != want {
		t.Errorf("PR request = %+v, want %+v", req, want)
	}
	if got := (Checkout{Repo: "acme/rocket", Number: 42, Kind: CheckoutIssue}).Request("p").Target; got.Kind != v1.TargetIssue || got.Number != 42 {
		t.Errorf("issue target = %+v", got)
	}
	if got := (Checkout{Repo: "acme/rocket", Number: 9, Branch: "dev", Kind: CheckoutRepo}).Request("p").Target; got.Kind != v1.TargetBranch || got.Number != 0 || got.Branch != "dev" {
		t.Errorf("a branch has no number: %+v", got)
	}
}

func TestACheckoutIsFoundOnTheSandboxThatKeepsIt(t *testing.T) {
	pr := fleet.Sandbox{Target: &v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7}}
	issue := fleet.Sandbox{Target: &v1.Target{Kind: v1.TargetIssue, Repo: "acme/rocket", Number: 7}}
	branch := fleet.Sandbox{Target: &v1.Target{Kind: v1.TargetBranch, Repo: "acme/rocket", Branch: "main"}}
	other := fleet.Sandbox{Target: &v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/other", Number: 7}}
	for name, tc := range map[string]struct {
		co   Checkout
		box  fleet.Sandbox
		want bool
	}{
		"the pull request":             {Checkout{Repo: "acme/rocket", Number: 7}, pr, true},
		"not the issue of that number": {Checkout{Repo: "acme/rocket", Number: 7}, issue, false},
		"the issue":                    {Checkout{Repo: "acme/rocket", Number: 7, Kind: CheckoutIssue}, issue, true},
		"the default branch":           {Checkout{Repo: "acme/rocket", Kind: CheckoutRepo}, branch, true},
		"another branch":               {Checkout{Repo: "acme/rocket", Kind: CheckoutRepo, Branch: "dev"}, branch, false},
		"another repository":           {Checkout{Repo: "acme/rocket", Number: 7}, other, false},
		"a sandbox with no volume":     {Checkout{Repo: "acme/rocket", Number: 7}, fleet.Sandbox{}, false},
	} {
		if got := tc.co.onSandbox(&tc.box); got != tc.want {
			t.Errorf("%s: onSandbox = %v, want %v", name, got, tc.want)
		}
	}
}

func TestCheckoutCardNamesTheHarnessAndModel(t *testing.T) {
	co := Checkout{Repo: "acme/rocket", Number: 7, Branch: "fix-auth", Harness: "codex", Model: "gpt-5.3-codex"}
	m := baseModel()
	m.pools["codex-pool"] = fleet.Pool{Name: "codex-pool", Template: "codex-template"}
	m, _ = m.CheckOut("codex-pool", co)
	if view := m.plainView(); !strings.Contains(view, "on codex-template · codex (gpt-5.3-codex)") {
		t.Errorf("checkout card missing harness/model:\n%s", view)
	}
}

// follow turns the services' events into the card's, and ends with the last.
func TestFollowingACheckoutReportsEachStepAndTheEnd(t *testing.T) {
	api := &fakeAPI{events: []v1.CheckoutEvent{
		{Step: v1.StepVolume, Finished: true, Detail: "pr-acme-rocket-7 · new"},
		{Step: v1.StepClone, Detail: "Receiving objects:  45% (1/2)"},
		{Step: v1.StepShell, Sandbox: "pi-pr-1", Done: true},
	}}
	var got []checkoutEvent
	follow(api, rocket(), "pi-pool", func(ev checkoutEvent) { got = append(got, ev) })
	if len(got) != 3 || !got[0].finished || got[0].step != stepVolume || got[1].step != stepClone || !got[2].done || got[2].pod != "pi-pr-1" {
		t.Fatalf("events = %+v", got)
	}
	if api.checkout.Pool != "pi-pool" || api.checkout.Target.Number != 7 {
		t.Errorf("asked for %+v", api.checkout)
	}
}

func TestACheckoutThatCannotStartOrDropsIsReportedAsFailed(t *testing.T) {
	var got []checkoutEvent
	follow(&fakeAPI{checkoutErr: errors.New("no such pool")}, rocket(), "x", func(ev checkoutEvent) { got = append(got, ev) })
	if len(got) != 1 || got[0].err == nil {
		t.Errorf("a refused checkout: %+v", got)
	}
	got = nil
	follow(&fakeAPI{events: []v1.CheckoutEvent{{Step: v1.StepClone, Detail: "x"}}, followErr: errors.New("connection reset")}, rocket(), "x", func(ev checkoutEvent) { got = append(got, ev) })
	if last := got[len(got)-1]; last.err == nil || last.step != stepClone {
		t.Errorf("a stream that dropped must end the card with an error: %+v", got)
	}
}

func TestAnErrorEventEndsTheCardWithItsText(t *testing.T) {
	got := eventOf(v1.CheckoutEvent{Step: v1.StepSandbox, Error: "image pull failed"})
	if got.err == nil || got.err.Error() != "image pull failed" || got.step != stepSandbox {
		t.Errorf("eventOf = %+v", got)
	}
}
