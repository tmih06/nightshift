package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	"github.com/tmih06/nightshift/internal/tui/dashboard"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

var rocket7 = ghtui.SandboxPullRequestMsg{Repo: "acme/rocket", Number: 7,
	URL: "https://github.com/acme/rocket/pull/7", Branch: "fix-auth"}

// piHarness and codexHarness are what the services report for the two pools.
func piHarness() v1.Harness {
	return v1.Harness{Name: "pi", Label: "Pi", Command: "pi", Models: []v1.HarnessModel{
		{ID: "anthropic/claude-sonnet", Label: "anthropic/claude-sonnet", Note: "anthropic"},
		{ID: "anthropic/claude-opus", Label: "anthropic/claude-opus", Note: "anthropic"},
		{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
		{ID: "google/gemini-2.5-pro", Label: "google/gemini-2.5-pro", Note: "google"},
		{ID: "", Label: "default", Note: "harness default"},
	}}
}

func codexHarness() v1.Harness {
	return v1.Harness{Name: "codex", Label: "Codex", Command: "codex", Models: []v1.HarnessModel{
		{ID: "gpt-5.3-codex", Label: "gpt-5.3-codex", Note: "coding"},
		{ID: "gpt-5.2-codex", Label: "gpt-5.2-codex", Note: "coding"},
		{ID: "gpt-5.5", Label: "gpt-5.5", Note: "general"},
		{ID: "gpt-5.2", Label: "gpt-5.2", Note: "general"},
		{ID: "", Label: "default", Note: "harness default"},
	}}
}

// withPools is m on the GitHub tab over a fleet of two pools.
func withPools(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, gh := newModel(t)
	m.sandboxes = m.sandboxes.WithFleet(nil, map[string]fleet.Pool{
		"pi-pool":    {Name: "pi-pool", Template: "pi-template", Ready: 1, Desired: 1, Harness: piHarness()},
		"codex-pool": {Name: "codex-pool", Template: "codex-template", Ready: 0, Desired: 1, Harness: codexHarness()},
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

func TestPickingAPoolOpensModelSubmenuAndEnterStartsCheckout(t *testing.T) {
	m, _ := openPicker(t)
	m = press(m, "j", "k") // codex-pool
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("opening the model submenu should not start checkout yet")
	}
	if m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "codex" {
		t.Fatalf("picker = %+v, want open codex model submenu", m.picker)
	}

	// Verify submenu forks to the right of the parent pool dialog and stays in bounds.
	parent := m.pickerPlaced()
	sub, hasSub := m.pickerSubPlaced()
	if !hasSub {
		t.Fatal("expected model submenu bounds")
	}
	if sub.X < parent.X+parent.W {
		t.Errorf("submenu X=%d overlaps parent right edge %d", sub.X, parent.X+parent.W)
	}
	if sub.X+sub.W > m.width {
		t.Errorf("submenu right edge %d exceeds width %d", sub.X+sub.W, m.width)
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
	for _, want := range []string{"Codex", "Model", "gpt-5.3-codex", "gpt-5.2-codex", "├─┤"} {
		if !strings.Contains(body, want) {
			t.Errorf("submenu view lacks %q:\n%s", want, body)
		}
	}

	// Move down to second model (gpt-5.2-codex) with j, then press enter to start.
	m = press(m, "j")
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("picking a model returned no command")
	}
	if m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("picker=%v active=%d, want it closed on the sandbox tab", m.picker != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on codex-template · codex (gpt-5.2-codex)") {
		t.Errorf("the sandbox tab does not show template, harness, and model:\n%s", body)
	}
}

func TestSubmenuNavigationWithArrowsAndHJKL(t *testing.T) {
	m, _ := openPicker(t)
	// l / right opens submenu for codex-pool.
	m = press(m, "l")
	if m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "codex" {
		t.Fatalf("l did not open codex submenu: %+v", m.picker)
	}
	// j / down and k / up move through models.
	m = press(m, "j")
	if m.picker.modelCursor != 1 {
		t.Fatalf("modelCursor after j = %d, want 1", m.picker.modelCursor)
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = next.(Model)
	if m.picker.modelCursor != 0 {
		t.Fatalf("modelCursor after up = %d, want 0", m.picker.modelCursor)
	}
	// h / left closes submenu back to pool list (keeping picker open).
	m = press(m, "h")
	if m.picker == nil || m.picker.modelOpen {
		t.Fatalf("h should close submenu and stay in pool picker: %+v", m.picker)
	}
	// Move to pi-pool with down arrow, open with right arrow.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m = next.(Model)
	if m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "pi" {
		t.Fatalf("right did not open pi submenu: %+v", m.picker)
	}
	// esc closes submenu first; second esc closes the whole picker.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.picker == nil || m.picker.modelOpen {
		t.Fatalf("first esc should close submenu only: %+v", m.picker)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	if m.picker != nil {
		t.Fatal("second esc did not close picker")
	}
}

func TestSubmenuFitsOnNarrowTerminal(t *testing.T) {
	m, _ := openPicker(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 68, Height: 24})
	m = next.(Model)
	m = press(m, "l")
	if m.picker == nil || !m.picker.modelOpen {
		t.Fatal("submenu did not open on narrow terminal")
	}
	f := frame(m)
	if len(f) != 24 {
		t.Errorf("frame height = %d, want 24", len(f))
	}
	for i, row := range f {
		if w := ansi.StringWidth(row); w > 68 {
			t.Errorf("narrow row %d is %d wide (> 68)", i, w)
		}
	}
	if body := strings.Join(f, "\n"); !strings.Contains(body, "gpt-5.3-codex") {
		t.Errorf("narrow terminal submenu missing model option:\n%s", body)
	}
}

func TestTheBarFollowsTheCheckoutFromTheGitHubTab(t *testing.T) {
	m, _ := openPicker(t)
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // open model submenu
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = press(next.(Model), "tab")
	if m.active != TabGitHub {
		t.Fatal("tab did not go back to GitHub")
	}
	if bar := frame(m)[0]; !strings.Contains(bar, "acme/rocket#7 · volume 1/4") {
		t.Errorf("bar = %q, want the checkout's progress", bar)
	}
}

func TestANumberPicksItsPoolThenModel(t *testing.T) {
	m, _ := openPicker(t)
	next, cmd := m.Update(key("2")) // pi-pool -> opens Pi model submenu
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "pi" {
		t.Fatalf("2 did not open pi model submenu: cmd=%v picker=%+v", cmd != nil, m.picker)
	}
	next, cmd = m.Update(key("2")) // 2nd Pi model: anthropic/claude-opus
	m = next.(Model)
	if cmd == nil || m.active != TabSandboxes {
		t.Fatal("model digit did not start checkout")
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template · pi (anthropic/claude-opus)") {
		t.Errorf("2->2 claimed the wrong pool/model:\n%s", body)
	}
}

func TestAClickOnARowOpensSubmenuAndClickingModelStarts(t *testing.T) {
	m, _ := openPicker(t)
	p := m.pickerPlaced()
	next, _ := m.Update(tea.MouseClickMsg{X: 0, Y: barHeight, Button: tea.MouseLeft})
	if next.(Model).picker != nil {
		t.Error("a click outside left the picker open")
	}
	// Click on pool row 1 (pi-pool) opens Pi model submenu.
	next, cmd := m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.OptionY(1), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "pi" {
		t.Fatalf("a click on a pool did not open its model submenu: cmd=%v picker=%+v", cmd != nil, m.picker)
	}
	// Click on model row 0 inside submenu starts checkout.
	sub, ok := m.pickerSubPlaced()
	if !ok {
		t.Fatal("submenu not placed")
	}
	next, cmd = m.Update(tea.MouseClickMsg{X: sub.X + 4, Y: barHeight + sub.OptionY(0), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd == nil || m.active != TabSandboxes {
		t.Fatal("a click on a model did not start checkout")
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template · pi (anthropic/claude-sonnet)") {
		t.Errorf("the click claimed the wrong pool/model:\n%s", body)
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
	if m.picker == nil || m.picker.kind != dashboard.CheckoutIssue || !m.picker.focusBranch {
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

	// Replace with a valid branch, tab to pool list, press 2 to open pi-pool's model submenu, then enter to start.
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
	if cmd != nil || m.picker == nil || !m.picker.modelOpen || m.picker.harness.Command != "pi" {
		t.Fatalf("picking pool 2 did not open pi model submenu: cmd=%v picker=%+v", cmd != nil, m.picker)
	}
	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("confirming model failed: cmd=%v picker=%v active=%d", cmd != nil, m.picker != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Checking out acme/rocket#42") || !strings.Contains(body, "on pi-template · pi (anthropic/claude-sonnet)") {
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

	// Click on pool row opens its model submenu; clicking a model starts checkout.
	next, cmd = m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.OptionY(1), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.modelOpen {
		t.Fatalf("click on pool did not open model submenu: cmd=%v picker=%+v", cmd != nil, m.picker)
	}
	sub, ok := m.pickerSubPlaced()
	if !ok {
		t.Fatal("expected model submenu bounds")
	}
	next, cmd = m.Update(tea.MouseClickMsg{X: sub.X + 4, Y: barHeight + sub.OptionY(0), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd == nil || m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("click on model did not start checkout: cmd=%v active=%d", cmd != nil, m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "on pi-template · pi (anthropic/claude-sonnet)") {
		t.Errorf("click picked wrong pool/model:\n%s", body)
	}
}

func TestValidateBranchName(t *testing.T) {
	for _, good := range []string{"issue-42", "feat/dark-mode", "fix/user_login.v2", "main"} {
		if got := v1.BranchProblem(good); got != "" {
			t.Errorf("v1.BranchProblem(%q) = %q, want valid", good, got)
		}
	}
	for _, bad := range []string{"", "has space", "-leading", "/leading", ".leading", "trailing/", "trailing.", "foo.lock", "a..b", "a//b", "a@{b", "@", "a~1", "a^2", "a:b", "a?b", "a*b", "a[b", `a\b`} {
		if got := v1.BranchProblem(bad); got == "" {
			t.Errorf("v1.BranchProblem(%q) = valid, want error", bad)
		}
	}
}

func BenchmarkIssuePickerTyping(b *testing.B) {
	t := &testing.T{}
	m, _ := openIssuePicker(t)
	k := tea.KeyPressMsg{Code: 'a', Text: "a"}
	for b.Loop() {
		_, _ = m.updatePickerBranch(k)
	}
}

func openRepoPickerHelper(t *testing.T, repo, branch, url string) (Model, *fakePRs) {
	t.Helper()
	m, gh := withPools(t)
	next, cmd := m.openRepoPicker(repo, branch, url)
	if cmd != nil {
		t.Error("opening the repo picker ran a command")
	}
	return next, gh
}

func TestRepoPickerOpensWithBranchAndDynamicVolume(t *testing.T) {
	m, _ := openRepoPickerHelper(t, "acme/rocket", "develop", "https://github.com/acme/rocket")
	if m.picker == nil || m.picker.kind != dashboard.CheckoutRepo || !m.picker.focusBranch {
		t.Fatalf("picker = %+v, want repo picker focused on branch", m.picker)
	}
	if got := m.picker.branchInput.Value(); got != "develop" {
		t.Errorf("branch = %q, want develop", got)
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
		"Check out acme/rocket in a sandbox",
		"volume repo-acme-rocket-develop",
		"Branch",
		"develop",
		"Pool",
		"codex-pool",
		"pi-pool",
		"esc cancel",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the repo picker lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "acme/rocket#") {
		t.Errorf("repo picker title includes a number:\n%s", body)
	}
	if v := m.View(); v.Cursor != nil {
		t.Error("gh-dash's cursor shows through the repo picker")
	}

	// Clearing and typing updates the volume preview line dynamically.
	next, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(Model)
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "volume repo-acme-rocket-default") {
		t.Errorf("empty branch preview did not fall back to default:\n%s", body)
	}
	next, _ = m.Update(tea.PasteMsg{Content: "feat/search"})
	m = next.(Model)
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "volume repo-acme-rocket-feat-search") {
		t.Errorf("typed branch preview did not update:\n%s", body)
	}
}

func TestRepoPickerDefaultsToMainAndValidatesRepoAndPools(t *testing.T) {
	m, _ := openRepoPickerHelper(t, "acme/rocket", "", "")
	if m.picker == nil || m.picker.branchInput.Value() != "main" {
		t.Fatalf("picker = %+v, want default branch main", m.picker)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "volume repo-acme-rocket-main") {
		t.Errorf("default branch preview missing:\n%s", body)
	}

	// Invalid repo names are rejected with an error notice.
	for _, bad := range []string{"", "noslash", "/name", "owner/"} {
		mBad, _ := withPools(t)
		next, _ := mBad.openRepoPicker(bad, "main", "")
		if next.picker != nil {
			t.Errorf("bad repo %q opened picker", bad)
		}
		if bar := frame(next)[0]; !strings.Contains(bar, "cannot check out") {
			t.Errorf("bad repo %q bar = %q", bad, bar)
		}
	}

	// Missing warm pools shows notice without opening picker.
	mNoPools, _ := newModel(t)
	mNoPools = press(mNoPools, "tab")
	next, _ := mNoPools.openRepoPicker("acme/rocket", "main", "")
	if next.picker != nil {
		t.Fatal("repo picker opened with no warm pools")
	}
	if bar := frame(next)[0]; !strings.Contains(bar, "no warm pools") {
		t.Errorf("bar = %q, want missing pools notice", bar)
	}
}

func TestRepoPickerEditsValidatesMouseAndStartsCheckout(t *testing.T) {
	m, gh := openRepoPickerHelper(t, "acme/rocket", "main", "https://github.com/acme/rocket")
	before := len(gh.keys)

	// Invalid branch blocks start and shows validation error.
	next, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(Model)
	m = press(m, "b", "a", "d", " ", "x")
	if len(gh.keys) != before {
		t.Error("keys typed in repo branch input reached gh-dash")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || m.picker == nil || m.picker.branchErr == "" {
		t.Fatalf("invalid branch started repo checkout: picker=%v cmd=%v", m.picker != nil, cmd != nil)
	}

	// Fix branch, tab to pool list, click branch to refocus, then click pool 2 to start.
	next, _ = m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m = next.(Model)
	next, _ = m.Update(tea.PasteMsg{Content: "release/v2"})
	m = next.(Model)
	m = press(m, "tab")
	if m.picker.focusBranch {
		t.Fatal("tab did not move focus to pools")
	}
	p := m.pickerPlaced()
	next, cmd = m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.FieldY(), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.focusBranch {
		t.Fatalf("click on branch field did not refocus it: picker=%v focus=%v", m.picker != nil, m.picker != nil && m.picker.focusBranch)
	}
	next, cmd = m.Update(tea.MouseClickMsg{X: p.X + 4, Y: barHeight + p.OptionY(1), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd != nil || m.picker == nil || !m.picker.modelOpen {
		t.Fatalf("click on pool did not open model submenu: cmd=%v picker=%+v", cmd != nil, m.picker)
	}
	sub, ok := m.pickerSubPlaced()
	if !ok {
		t.Fatal("expected model submenu bounds")
	}
	next, cmd = m.Update(tea.MouseClickMsg{X: sub.X + 4, Y: barHeight + sub.OptionY(0), Button: tea.MouseLeft})
	m = next.(Model)
	if cmd == nil || m.picker != nil || m.active != TabSandboxes {
		t.Fatalf("click on model did not start repo checkout: cmd=%v picker=%v active=%d", cmd != nil, m.picker != nil, m.active)
	}
	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "Checking out acme/rocket@release/v2") || !strings.Contains(body, "on pi-template · pi (anthropic/claude-sonnet)") {
		t.Errorf("sandbox tab did not start repo checkout:\n%s", body)
	}
}
