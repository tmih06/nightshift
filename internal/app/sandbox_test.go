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
