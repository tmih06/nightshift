package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
)

var rocket7 = ghtui.SandboxPullRequestMsg{Repo: "acme/rocket", Number: 7,
	URL: "https://github.com/acme/rocket/pull/7", Branch: "fix-auth"}

// withPools is m on the GitHub tab over a fleet of two pools.
func withPools(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, gh := newModel(t)
	m.sandboxes = m.sandboxes.WithFleet(nil, map[string]cluster.Pool{
		"pi-pool":    {Name: "pi-pool", Template: "pi-template", Ready: 1, Desired: 1},
		"codex-pool": {Name: "codex-pool", Template: "codex-template", Ready: 0, Desired: 1},
	})
	return press(m, "tab"), gh
}

func openPicker(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, gh := withPools(t)
	next, cmd := m.Update(rocket7)
	if cmd != nil {
		t.Error("opening the picker ran a command")
	}
	return next.(Model), gh
}

func TestSandboxKeyAsksWhichPool(t *testing.T) {
	m, _ := openPicker(t)
	if m.picker == nil {
		t.Fatal("no picker opened")
	}
	if got := m.picker.pools[m.picker.cursor].Name; got != "codex-pool" {
		t.Errorf("chosen pool = %q, want the first", got)
	}
	f := frame(m)
	if len(f) != 40 {
		t.Errorf("frame is %d rows, want 40", len(f))
	}
	for i, row := range f {
		if w := ansi.StringWidth(row); w > 120 {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
	body := strings.Join(f, "\n")
	for _, want := range []string{"Check out acme/rocket#7 in a sandbox", "branch fix-auth · volume pr-acme-rocket-7",
		"codex-pool", "pi-pool", "pi-template", "esc cancel"} {
		if !strings.Contains(body, want) {
			t.Errorf("the picker lacks %q:\n%s", want, body)
		}
	}
	if v := m.View(); v.Cursor != nil {
		t.Error("gh-dash's cursor shows through the picker")
	}
}

func TestThePickerHoldsTheKeyboard(t *testing.T) {
	m, gh := openPicker(t)
	before := len(gh.keys)
	m = press(m, "k", "tab", "j")
	if len(gh.keys) != before {
		t.Error("a key meant for the picker reached gh-dash")
	}
	if m.active != TabGitHub {
		t.Error("tab switched tabs under the picker")
	}
	if !m.capturing() {
		t.Error("the picker does not claim the keyboard")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.picker != nil {
		t.Fatal("esc left the picker open")
	}
	if body := strings.Join(frame(m), "\n"); strings.Contains(body, "in a sandbox") {
		t.Error("the picker is still drawn after esc")
	}
}

func TestPickingAPoolStartsTheCheckoutOnTheSandboxTab(t *testing.T) {
	m, _ := openPicker(t)
	m = press(m, "j", "k") // codex-pool
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("picking a pool returned no command")
	}
	if m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("picker=%v active=%d, want it closed on the sandbox tab", m.picker != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on codex-template") {
		t.Errorf("the sandbox tab does not say what it starts:\n%s", body)
	}
}

func TestTheBarFollowsTheCheckoutFromTheGitHubTab(t *testing.T) {
	m, _ := openPicker(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(next.(Model), "tab")
	if m.active != TabGitHub {
		t.Fatal("tab did not go back to GitHub")
	}
	if bar := frame(m)[0]; !strings.Contains(bar, "acme/rocket#7 · volume 1/4") {
		t.Errorf("bar = %q, want the checkout's progress", bar)
	}
}

func TestANumberPicksItsPool(t *testing.T) {
	m, _ := openPicker(t)
	next, cmd := m.Update(key("2"))
	m = next.(Model)
	if cmd == nil || m.active != TabSandboxes {
		t.Fatal("2 did not claim")
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template") {
		t.Errorf("2 claimed the wrong pool:\n%s", body)
	}
}

func TestAClickOnARowPicksItAndOneOutsideCancels(t *testing.T) {
	m, _ := openPicker(t)
	p := m.pickerPlaced()
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: barHeight, Button: tea.MouseLeft})
	if next.(Model).picker != nil {
		t.Error("a click outside left the picker open")
	}
	next, cmd := m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.OptionY(1), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd == nil || m.active != TabSandboxes {
		t.Fatal("a click on a pool did not claim it")
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template") {
		t.Errorf("the click claimed the wrong pool:\n%s", body)
	}
}

func TestNoPoolsSaysSoInTheBar(t *testing.T) {
	m, _ := newModel(t)
	m = press(m, "tab")
	next, _ := m.Update(rocket7)
	m = next.(Model)
	if m.picker != nil {
		t.Fatal("a picker opened with nothing to pick")
	}
	if bar := frame(m)[0]; !strings.Contains(bar, "no warm pools") {
		t.Errorf("bar = %q, want the missing pools named", bar)
	}
}

var rocket42Issue = ghtui.SandboxIssueMsg{
	Repo:   "acme/rocket",
	Number: 42,
	URL:    "https://github.com/acme/rocket/issues/42",
	Title:  "Fix login",
}

func openIssuePicker(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, gh := withPools(t)
	next, cmd := m.Update(rocket42Issue)
	if cmd != nil {
		t.Error("opening the issue picker ran a command")
	}
	return next.(Model), gh
}

func TestIssueSandboxKeyAsksForBranchAndPool(t *testing.T) {
	m, _ := openIssuePicker(t)
	if m.picker == nil || !m.picker.issue || !m.picker.focusBranch {
		t.Fatalf("picker = %+v, want issue picker focused on branch", m.picker)
	}
	if got := m.picker.branchInput.Value(); got != "issue-42" {
		t.Errorf("default branch = %q, want issue-42", got)
	}
	f := frame(m)
	if len(f) != 40 {
		t.Errorf("frame is %d rows, want 40", len(f))
	}
	for i, row := range f {
		if w := ansi.StringWidth(row); w > 120 {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
	body := strings.Join(f, "\n")
	for _, want := range []string{
		"Check out acme/rocket#42 in a sandbox",
		"volume issue-acme-rocket-42",
		"Branch",
		"issue-42",
		"Pool",
		"codex-pool",
		"pi-pool",
		"esc cancel",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the issue picker lacks %q:\n%s", want, body)
		}
	}
	if v := m.View(); v.Cursor != nil {
		t.Error("gh-dash's cursor shows through the issue picker")
	}
}

func TestIssuePickerEditsBranchPicksPoolAndValidates(t *testing.T) {
	m, gh := openIssuePicker(t)
	before := len(gh.keys)

	// Typing edits the branch name (including q and digits) without leaking to gh-dash.
	next, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}) // clear default
	m = next.(Model)
	m = press(m, "f", "i", "x", "-", "q", "1")
	if len(gh.keys) != before {
		t.Error("keys typed in branch input reached gh-dash")
	}
	if got := m.picker.branchInput.Value(); got != "fix-q1" {
		t.Fatalf("branch = %q, want fix-q1", got)
	}

	// Paste appends into the branch input.
	next, _ = m.Update(tea.PasteMsg{Content: "-extra"})
	m = next.(Model)
	if got := m.picker.branchInput.Value(); got != "fix-q1-extra" {
		t.Fatalf("branch after paste = %q, want fix-q1-extra", got)
	}

	// Down arrow moves the pool selection even while editing the branch.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if got := m.picker.pools[m.picker.cursor].Name; got != "pi-pool" {
		t.Fatalf("pool cursor = %q, want pi-pool", got)
	}

	// Invalid branch name keeps the dialog open with an error.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(Model)
	m = press(m, "b", "a", "d", " ", "b")
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.picker == nil || m.picker.branchErr == "" {
		t.Fatalf("invalid branch started checkout: picker=%v cmd=%v", m.picker != nil, cmd != nil)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "invalid git branch name") {
		t.Errorf("dialog lacks validation error:\n%s", body)
	}

	// Replace with a valid branch, tab to pool list, press 2 to pick pi-pool.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(Model)
	next, _ = m.Update(tea.PasteMsg{Content: "feat/issue-42"})
	m = next.(Model)
	m = press(m, "tab")
	if m.picker.focusBranch {
		t.Fatal("tab did not switch focus to pool list")
	}
	next, cmd = m.Update(key("2"))
	m = next.(Model)
	if cmd == nil || m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("picking pool 2 failed: cmd=%v picker=%v active=%d", cmd != nil, m.picker != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Checking out acme/rocket#42") || !strings.Contains(body, "on pi-template") {
		t.Errorf("sandbox tab did not start issue checkout:\n%s", body)
	}
}

func TestIssuePickerMouseFocusesBranchAndPicksPool(t *testing.T) {
	m, _ := openIssuePicker(t)
	m = press(m, "tab") // focus pool list
	if m.picker.focusBranch {
		t.Fatal("expected pool focus")
	}
	p := m.pickerPlaced()

	// Click on branch field refocuses branch input.
	next, cmd := m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.FieldY(), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.focusBranch {
		t.Fatalf("click on branch did not focus it: picker=%v focusBranch=%v", m.picker != nil, m.picker != nil && m.picker.focusBranch)
	}

	// Click on pool row starts checkout.
	next, cmd = m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.OptionY(1), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd == nil || m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("click on pool did not start checkout: cmd=%v active=%d", cmd != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template") {
		t.Errorf("click picked wrong pool:\n%s", body)
	}
}

func TestValidateBranchName(t *testing.T) {
	for _, good := range []string{"issue-42", "feat/dark-mode", "fix/user_login.v2", "main"} {
		if got := validateBranchName(good); got != "" {
			t.Errorf("validateBranchName(%q) = %q, want valid", good, got)
		}
	}
	for _, bad := range []string{"", "has space", "-leading", "/leading", ".leading", "trailing/", "trailing.", "foo.lock", "a..b", "a//b", "a@{b", "@", "a~1", "a^2", "a:b", "a?b", "a*b", "a[b", `a\b`} {
		if got := validateBranchName(bad); got == "" {
			t.Errorf("validateBranchName(%q) = valid, want error", bad)
		}
	}
}
