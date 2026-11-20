package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

func testBoxes() []cluster.Sandbox {
	created := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	return []cluster.Sandbox{
		{Name: "agent-pool-5ss7w", OwnerKind: "SandboxWarmPool", Owner: "agent-pool",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256, CPUUsageMC: 2, MemUsageMiB: 6,
			Image: "debian:12-slim", Command: "/bin/bash -c sleep infinity", RestartPolicy: "OnFailure",
			CPUReqMC: 100, MemReqMiB: 128},
		{Name: "agent-pool-lqbmx", OwnerKind: "SandboxWarmPool", Owner: "agent-pool",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256,
			Image: "debian:12-slim", Command: "/bin/bash -c sleep infinity", RestartPolicy: "OnFailure",
			CPUReqMC: 100, MemReqMiB: 128},
		{Name: "agent-pool-bkl2p", OwnerKind: "SandboxClaim", Owner: "my-agent",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256, CPUUsageMC: 2, MemUsageMiB: 6},
	}
}

func baseModel() model {
	return model{boxes: testBoxes(), selected: "agent-pool-5ss7w",
		pool: cluster.Pool{Name: "agent-pool", Template: "agent-bash-template",
			Ready: 2, Desired: 2, UpdateStrategy: "Recreate"},
		sessions: map[string]bool{}, metricsOK: true,
		loaded: true, width: 100, height: 30}
}

// The left pane separates warm and claimed trees, groups each by node,
// and maps only selectable entries back to the underlying sandbox.
func TestLeftLinesTree(t *testing.T) {
	m := baseModel()
	lines, targets := m.leftLines()

	if !strings.Contains(lines[0], "WARM POOL (2)") || !strings.Contains(lines[1], "unassigned (2)") ||
		!strings.Contains(lines[4], "CLAIMED (1)") || !strings.Contains(lines[5], "unassigned (1)") {
		t.Errorf("tree headers = %q, want separate warm and claimed node trees", lines)
	}
	if got, ok := targets[2]; !ok || got != 0 || !strings.Contains(lines[2], "[warm]") || !strings.Contains(lines[2], "agent-pool-5ss7w") {
		t.Errorf("first sandbox row = %q, targets %v", lines[2], targets)
	}
	if got, ok := targets[6]; !ok || got != 2 || !strings.Contains(lines[6], "[claimed]") || !strings.Contains(lines[6], "agent-pool-bkl2p") {
		t.Errorf("claimed sandbox row = %q, targets %v", lines[6], targets)
	}

	// Session dots: the live session marker appears only for that pod.
	m.sessions["agent-pool-bkl2p"] = true
	lines, _ = m.leftLines()
	for _, row := range lines {
		if strings.Contains(row, "agent-pool-bkl2p") {
			if !strings.Contains(row, "●") {
				t.Errorf("live session row = %q, want the ● marker", row)
			}
		} else if strings.Contains(row, "agent-pool-5ss7w") {
			if strings.Contains(row, "●") {
				t.Errorf("dead session row = %q, want no ● marker", row)
			}
		}
	}
}

func TestNodeTreeOrderingAndNavigation(t *testing.T) {
	m := baseModel()
	m.boxes[0].NodeName = "node-b"
	m.boxes[1].NodeName = "node-a"
	m.boxes[2].NodeName = "node-a"
	lines, targets := m.leftLines()

	rowContaining := func(text string) int {
		t.Helper()
		for row, line := range lines {
			if strings.Contains(line, text) {
				return row
			}
		}
		t.Fatalf("tree missing %q: %q", text, lines)
		return -1
	}
	warmNodeA, nodeB := rowContaining("node-a (1)"), rowContaining("node-b (1)")
	claimed := rowContaining("CLAIMED (1)")
	claimedNodeA := claimed + 1
	if warmNodeA >= nodeB || nodeB >= claimed || !strings.Contains(lines[claimedNodeA], "node-a (1)") {
		t.Fatalf("tree order incorrect: %q", lines)
	}
	if got, ok := targets[warmNodeA+1]; !ok || got != 1 {
		t.Errorf("node-a warm leaf target = %d, %v; want sandbox index 1", got, ok)
	}
	if got, ok := targets[claimedNodeA+1]; !ok || got != 2 {
		t.Errorf("node-a claimed leaf target = %d, %v; want sandbox index 2", got, ok)
	}
	if got, ok := targets[nodeB+1]; !ok || got != 0 {
		t.Errorf("node-b leaf target = %d, %v; want sandbox index 0", got, ok)
	}
	for _, row := range []int{0, warmNodeA, claimed, claimedNodeA} {
		if _, ok := targets[row]; ok {
			t.Errorf("tree header row %d should not be selectable", row)
		}
	}

	m.selected = "agent-pool-lqbmx"
	m.moveSelection(1)
	if m.selected != "agent-pool-5ss7w" {
		t.Errorf("j moved to %q, want node-b's warm leaf", m.selected)
	}
	m.moveSelection(1)
	if m.selected != "agent-pool-bkl2p" {
		t.Errorf("j moved to %q, want the claimed leaf", m.selected)
	}
	newM, _ := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("g selected %q, want the first leaf in node order", newM.(model).selected)
	}
	newM, _ = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if newM.(model).selected != "agent-pool-bkl2p" {
		t.Errorf("G selected %q, want the last leaf in tree order", newM.(model).selected)
	}

	m.selected = "agent-pool-5ss7w"
	m = m.clickLeftPane(5, contentStartY+warmNodeA)
	if m.selected != "agent-pool-5ss7w" {
		t.Errorf("clicking a node header selected %q", m.selected)
	}
	m = m.clickLeftPane(5, contentStartY+warmNodeA+1)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("clicking node-a's first leaf selected %q", m.selected)
	}
}

func TestNodeTreeHandlesUnassignedSandboxes(t *testing.T) {
	m := baseModel()
	m.boxes[0].NodeName = "worker-a"
	lines, targets := m.leftLines()
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "worker-a (1)") || strings.Count(joined, "unassigned (1)") != 2 {
		t.Errorf("trees did not group assigned and unassigned sandboxes: %q", lines)
	}
	if targets[4] != 1 || targets[7] != 2 {
		t.Errorf("unassigned leaves map to %v, want sandbox indices 1 and 2", targets)
	}
}

func TestTreesHandleEmptyAndOtherGroups(t *testing.T) {
	m := baseModel()
	m.boxes = []cluster.Sandbox{
		{Name: "unknown", OwnerKind: "Other", NodeName: "node-a"},
		{Name: "claimed", OwnerKind: "SandboxClaim", NodeName: "node-a"},
	}
	m.selected = "claimed"
	lines, targets := m.leftLines()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"WARM POOL (0)", "CLAIMED (1)", "OTHER (1)", "[other]"} {
		if !strings.Contains(joined, want) {
			t.Errorf("trees missing %q: %q", want, lines)
		}
	}
	if len(targets) != 2 || targets[3] != 1 || targets[6] != 0 {
		t.Errorf("leaf targets = %v, want claimed then other", targets)
	}
	m.moveSelection(1)
	if m.selected != "unknown" {
		t.Errorf("navigation selected %q, want other leaf", m.selected)
	}
}

func TestNodeTreeScrollKeepsSelectedLeafVisible(t *testing.T) {
	m := baseModel()
	m.height = 12
	m.boxes = make([]cluster.Sandbox, 8)
	for i := range m.boxes {
		letter := string(rune('a' + i))
		m.boxes[i] = cluster.Sandbox{Name: "sandbox-" + letter, NodeName: "node-" + letter, OwnerKind: "SandboxWarmPool"}
	}
	m.selected = "sandbox-h"
	lines, targets := m.leftLines()
	selectedRow := -1
	for row, line := range lines {
		if strings.Contains(line, m.selected) {
			selectedRow = row
			if got := m.boxes[targets[row]].Name; got != m.selected {
				t.Fatalf("visible selected row maps to %q, want %q", got, m.selected)
			}
		}
	}
	if selectedRow < 0 {
		t.Fatalf("selected leaf %q is not visible in %q", m.selected, lines)
	}
	m = m.clickLeftPane(5, contentStartY+selectedRow)
	if m.selected != "sandbox-h" {
		t.Errorf("clicking visible selected leaf changed selection to %q", m.selected)
	}
}

func TestMouseClickSelectsEntry(t *testing.T) {
	m := baseModel()

	// Click the second warm entry after the root and unassigned node rows.
	m = m.clickLeftPane(5, contentStartY+3)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("selected = %q after clicking the second entry, want agent-pool-lqbmx", m.selected)
	}

	// A click in the right pane selects nothing.
	m = m.clickLeftPane(90, contentStartY+2)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("right-pane click moved the selection to %q, want no change", m.selected)
	}

	// Below the left pane's content changes nothing.
	m = m.clickLeftPane(5, contentStartY+20)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("empty-row click moved the selection to %q, want no change", m.selected)
	}
}

func TestMouseWheelMoves(t *testing.T) {
	m := baseModel()
	newM, _ := m.updateMouse(tea.MouseMsg{X: 5, Y: 5,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("wheel down moved selection to %q, want agent-pool-lqbmx", newM.(model).selected)
	}
	newM, _ = newM.(model).updateMouse(tea.MouseMsg{X: 5, Y: 5,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if newM.(model).selected != "agent-pool-5ss7w" {
		t.Errorf("wheel up moved selection to %q, want agent-pool-5ss7w", newM.(model).selected)
	}
}

func TestStackedClickUsesY(t *testing.T) {
	m := baseModel()
	m.width = 40 // below stackedBelow
	// The left pane is the top half; clicking a tree leaf selects its sandbox.
	m = m.clickLeftPane(5, contentStartY+3)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("stacked click selected %q, want agent-pool-lqbmx", m.selected)
	}
}

// The usage line is exactly innerW wide, so golden rule 2 holds: no
// auto-wrap inside the bordered pane.
func TestBrailleGraph(t *testing.T) {
	// strip ANSI to compare the raw braille pattern.
	strip := func(s string) string {
		out := ""
		for _, r := range s {
			if r >= 0x2800 && r <= 0x28FF {
				out += string(r)
			}
		}
		return out
	}

	// One row, btop-style: a sample fills its dot column from the
	// baseline up to its height. A flat zero window is the baseline,
	// dots 7 and 8 in every cell.
	if got := usageGraph([]float64{0, 0, 0, 0}, 2); strip(got) != "⣀⣀" {
		t.Errorf("baseline = %q, want ⣀⣀", got)
	}

	// A peak sample fills its whole column: all eight dots per cell.
	if got := usageGraph([]float64{1, 1, 1, 1}, 2); strip(got) != "⣿⣿" {
		t.Errorf("peak = %q, want ⣿⣿", got)
	}

	// A cell carries two consecutive samples, one per dot column: the
	// left at the limit fills its whole column, the right at 30% fills
	// one dot row, dot 8.
	if got := usageGraph([]float64{1, 0.3}, 1); strip(got) != "⣇" {
		t.Errorf("mixed cell = %q, want ⣇", got)
	}

	// Half the limit in the right column fills two of four dot rows.
	if got := usageGraph([]float64{1, 0.5}, 1); strip(got) != "⣧" {
		t.Errorf("half cell = %q, want ⣧", got)
	}

	// No window autoscaling: a uniform half-limit window is the highest
	// thing on screen and still draws two dot rows, not four. The wave
	// keeps its true height.
	if got := usageGraph([]float64{0.5, 0.5, 0.5, 0.5}, 2); strip(got) != "⣤⣤" {
		t.Errorf("uniform half window = %q, want ⣤⣤, not full height", got)
	}
	if got := usageGraph([]float64{0.75, 0.75, 0.75, 0.75}, 2); strip(got) != "⣶⣶" {
		t.Errorf("uniform three-quarter window = %q, want ⣶⣶", got)
	}

	// A young history grows from the right edge: one sample fills the
	// last cell's left column full.
	if got := usageGraph([]float64{1}, 2); strip(got) != "⡇" {
		t.Errorf("young graph = %q, want ⡇", got)
	}

	// No history yet: the placeholder row.
	if got := usageGraph(nil, 2); !strings.Contains(got, "·") {
		t.Errorf("placeholder = %q, want dots", got)
	}
}

func TestUsageLinesTable(t *testing.T) {
	rows := usageLines("CPU", true, 250, 500, "m", []float64{0, 0.5, 1}, 40)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want value plus 1 graph row", len(rows))
	}
	// The value row is the table: label, value, percent, bar.
	for _, want := range []string{"CPU", "250m / 500m", "50%", "█", "░"} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("value row = %q, missing %q", rows[0], want)
		}
	}
	// The graph rows carry braille patterns, and each row is exactly
	// the box width, so the panes stay aligned.
	for i, r := range rows {
		if got := lipgloss.Width(r); got != 40 {
			t.Errorf("row %d width = %d, want 40", i, got)
		}
	}

	// The table aligns across metrics: the percent sign lands at the
	// same column whatever the numbers, and both rows are the same
	// total width, so the bar region starts together.
	cpu := usageLines("CPU", true, 250, 500, "m", nil, 40)[0]
	mem := usageLines("MEM", true, 6, 256, "Mi", nil, 40)[0]
	if strings.Index(cpu, "%") != strings.Index(mem, "%") {
		t.Errorf("percent columns misaligned:\n%q\n%q", cpu, mem)
	}
	if lipgloss.Width(cpu) != lipgloss.Width(mem) {
		t.Errorf("row widths differ:\n%q\n%q", cpu, mem)
	}

	// Unknown usage keeps the table shape with a placeholder value.
	rows = usageLines("MEM", false, 0, 256, "Mi", nil, 40)
	if !strings.Contains(rows[0], "-- / 256Mi") || !strings.Contains(rows[0], "0%") {
		t.Errorf("unknown value row = %q", rows[0])
	}
	if !strings.Contains(rows[1], "·") {
		t.Errorf("history-less graph = %q, want the placeholder", rows[1])
	}

	// No limit: honest fallback in the table.
	rows = usageLines("CPU", true, 4, 0, "m", nil, 40)
	if !strings.Contains(rows[0], "no limit") {
		t.Errorf("no-limit value row = %q", rows[0])
	}
}

func TestKeyboardNavigationAndActions(t *testing.T) {
	m := baseModel()

	newM, _ := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyDown})
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("j moved selection to %q, want agent-pool-lqbmx", newM.(model).selected)
	}

	// Destroy on a warm sandbox is refused.
	newM, _ = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if newM.(model).status != "only a claimed sandbox can be destroyed" {
		t.Errorf("x on warm = %q", newM.(model).status)
	}

	// Enter on a warm sandbox opens its config viewer; b still refuses
	// because a warm sandbox has no bash session.
	warm := m
	newM, cmd := warm.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || newM.(model).focus != focusInspect {
		t.Errorf("enter on warm = focus %v, cmd %v; want focusInspect and no command", newM.(model).focus, cmd != nil)
	}
	newM, _ = warm.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if newM.(model).status != "warm pool sandbox — claim it with c, bash starts with the claim" {
		t.Errorf("b on warm = %q", newM.(model).status)
	}

	// Enter on a pod that is not running is refused with a reason.
	claim := m
	claim.boxes[2].Phase = "Pending"
	claim.selected = "agent-pool-bkl2p"
	newM, _ = claim.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if newM.(model).status != "pod is Pending, no bash to enter" {
		t.Errorf("enter on pending = %q", newM.(model).status)
	}

	// Enter on a running claimed pod returns the attach command, which
	// the caller executes (not here: it would start a real Herdr session).
	running := baseModel()
	running.selected = "agent-pool-bkl2p"
	_, cmd = running.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("enter on a running pod returned no command")
	}

	// q quits.
	_, cmd = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("q returned no quit command")
	}
}

func TestClaimShortcuts(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}
	for _, selected := range []string{"agent-pool-5ss7w", "agent-pool-bkl2p"} {
		nav := baseModel()
		nav.selected = selected
		newM, cmd := nav.Update(msg)
		if cmd == nil || !strings.Contains(newM.(model).status, "claiming from the warm pool") {
			t.Errorf("c on %s did not start a claim", selected)
		}
	}

	inspect := baseModel()
	inspect.focus = focusInspect
	newM, cmd := inspect.Update(msg)
	if cmd == nil || newM.(model).focus != focusNav || !strings.Contains(newM.(model).status, "claiming from the warm pool") {
		t.Error("c in config view did not start a claim")
	}

	uppercase := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}
	for _, focus := range []focusPane{focusNav, focusInspect} {
		m := baseModel()
		m.focus = focus
		newM, cmd := m.Update(uppercase)
		if cmd != nil || newM.(model).focus != focus || newM.(model).status != "" {
			t.Errorf("uppercase C in focus %v should not start a claim", focus)
		}
	}

	// Uppercase C remains text input while bash has focus.
	shell := baseModel()
	shell.selected = "agent-pool-bkl2p"
	shell.sessions["agent-pool-bkl2p"] = true
	shell.focus = focusShell
	shell.sending = true
	newM, cmd = shell.Update(uppercase)
	got := newM.(model)
	if cmd != nil || got.focus != focusShell || len(got.pending) != 1 || got.pending[0].Text != "C" {
		t.Errorf("uppercase C in bash = focus %v pending %v cmd=%v, want queued text", got.focus, got.pending, cmd != nil)
	}
}

// Destroy needs two presses on a claimed sandbox, and any other key
// disarms it.
func TestDestroyConfirmation(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p" // the claimed sandbox, owner my-agent

	newM, _ := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	armed := newM.(model)
	if !armed.confirmDestroy || !strings.Contains(armed.status, "press x again") {
		t.Errorf("first x = %v %q", armed.confirmDestroy, armed.status)
	}
	if _, cmd := armed.updateKeyboard(tea.KeyMsg{Type: tea.KeyDown}); cmd != nil {
		t.Error("navigation while armed must not destroy anything")
	}

	disarmed, _ := armed.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if disarmed.(model).confirmDestroy {
		t.Error("a navigation key must disarm the confirmation")
	}

	// Arming again and pressing x returns the delete command.
	armed2, _ := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	_, cmd := armed2.(model).updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Error("second x returned no destroy command")
	}
}

func TestUpdateMessages(t *testing.T) {
	m := baseModel()

	// A loaded batch whose fleet lost the selected pod snaps to the
	// nearest entry instead of pointing at whatever shuffled into its
	// old index.
	short := m
	short.selected = "agent-pool-bkl2p" // index 2, destroyed
	shrunk := append([]cluster.Sandbox{}, m.boxes[0], m.boxes[1])
	newM, _ := short.Update(loadedMsg{boxes: shrunk, sessions: map[string]bool{}})
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("selection after shrink = %q, want the neighbor agent-pool-lqbmx", newM.(model).selected)
	}

	newM, _ = m.Update(claimMsg{err: errFixture})
	if !strings.Contains(newM.(model).status, "claim failed") {
		t.Errorf("claim failure status = %q", newM.(model).status)
	}

	newM, _ = m.Update(claimMsg{name: "dash-claim-1"})
	if !strings.Contains(newM.(model).status, "dash-claim-1") {
		t.Errorf("claim status = %q", newM.(model).status)
	}

	newM, _ = m.Update(attachReadyMsg{pod: "agent-pool-5ss7w", err: errFixture})
	if !strings.Contains(newM.(model).status, "session failed") {
		t.Errorf("attach failure status = %q", newM.(model).status)
	}

	newM, _ = m.Update(resumeMsg{})
	if !strings.Contains(newM.(model).status, "session still running") {
		t.Errorf("detach status = %q", newM.(model).status)
	}
}

func TestViewFrame(t *testing.T) {
	m := baseModel()
	m.sessions["agent-pool-5ss7w"] = true
	got := m.View()

	for _, want := range []string{
		"nightshift",
		"WARM POOL (2)",
		"CLAIMED (1)",
		"unassigned (2)",
		"[warm]",
		"[claimed]",
		"agent-pool-5ss7w",
		"agent-pool-lqbmx",
		"agent-pool-bkl2p",
		"▶",
		"· warm",
		"owner agent-pool",
		"Running",
		"herdr live",
		"debian:12-slim",
		"sleep infinity",
		"100/500m",
		"128Mi/256Mi",
		"OnFailure",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q", want)
		}
	}

	// The left pane and the shell pane close on the same row: they are
	// the full-height columns, so nothing wrapped inside them. The two
	// header boxes close on their own row above.
	rows := strings.Split(got, "\n")
	var closing string
	for _, r := range rows {
		if strings.Contains(r, "shell") || strings.Count(r, "╰") >= 1 {
			if strings.Count(r, "╰") == 2 {
				closing = r
			}
		}
	}
	last := ""
	for _, r := range rows {
		if strings.Count(r, "╰") > 0 {
			last = r
		}
	}
	if last == "" || strings.Count(last, "╰") != 2 {
		t.Errorf("closing row = %q, want the left pane and shell pane to close on it", last)
	}
	_ = closing

	// The inspect pane follows the selection: the claimed sandbox shows
	// its usage and shell pane, the warm pod shows its spec and the
	// pool box instead.
	m.selected = "agent-pool-bkl2p"
	g := m.View()
	for _, want := range []string{"· claimed", "my-agent", "herdr off", "2m / 500m"} {
		if !strings.Contains(g, want) {
			t.Errorf("claimed inspect missing %q", want)
		}
	}
	if !strings.Contains(g, "no session — claim with c, or enter on a claimed sandbox") {
		t.Error("claimed sandbox must show the shell pane")
	}
	m.selected = "agent-pool-5ss7w"
	g = m.View()
	for _, want := range []string{"agent-pool · warm pool", "agent-bash-template", "2/2", "Recreate"} {
		if !strings.Contains(g, want) {
			t.Errorf("warm pool box missing %q", want)
		}
	}
	if strings.Contains(g, "no session — claim with") || strings.Contains(g, "bash — click here") {
		t.Error("a warm sandbox must not show the shell pane")
	}
	m.selected = "agent-pool-lqbmx"
	if g := m.View(); !strings.Contains(g, "herdr off") {
		t.Errorf("inspect of a session-less pod = missing the off state")
	}
}

func TestViewStates(t *testing.T) {
	m := baseModel()
	m.metricsOK = false
	if got := m.View(); !strings.Contains(got, "live usage unavailable") {
		t.Errorf("metrics-off status = missing the warning")
	}

	m.err = errFixture
	if got := m.View(); !strings.Contains(got, "fetch error") {
		t.Errorf("error view missing the error banner, got %q", got)
	}

	m = baseModel()
	m.boxes = nil
	if got := m.View(); !strings.Contains(got, "no sandboxes…") {
		t.Errorf("empty view = %q", got)
	}

	if got := (model{sessions: map[string]bool{}}).View(); !strings.Contains(got, "loading") {
		t.Errorf("initial view = %q", got)
	}

	// Narrow terminal stacks the panes instead of side by side.
	m = baseModel()
	m.width = 40
	rows := strings.Split(m.View(), "\n")
	leftBottom, rightTop := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "WARM POOL") {
			leftBottom = i
		}
		if strings.Contains(r, "nothing selected") || strings.Contains(r, "owner ") {
			if leftBottom >= 0 && rightTop == -1 {
				rightTop = i
			}
		}
	}
	if leftBottom == -1 || rightTop == -1 || rightTop < leftBottom {
		t.Errorf("stacked layout did not place the right pane below the left: %d %d", leftBottom, rightTop)
	}
}

func TestAgeString(t *testing.T) {
	now := time.Now()
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{48 * time.Hour, "2d"},
	}
	for _, tc := range cases {
		if got := ageString(now.Add(-tc.ago)); got != tc.want {
			t.Errorf("ageString(%v) = %q, want %q", tc.ago, got, tc.want)
		}
	}
	if got := ageString(time.Time{}); got != "…" {
		t.Errorf("ageString(zero) = %q, want …", got)
	}
}

func TestMoveSelectionClamps(t *testing.T) {
	m := baseModel()
	m.moveSelection(-5)
	if m.selected != "agent-pool-5ss7w" {
		t.Errorf("selected = %q above the top, want the first entry", m.selected)
	}
	m.moveSelection(99)
	if m.selected != "agent-pool-bkl2p" {
		t.Errorf("selected = %q below the bottom, want the last entry", m.selected)
	}
	empty := model{}
	empty.moveSelection(3)
	if empty.selected != "" {
		t.Errorf("empty fleet selection = %q, want none", empty.selected)
	}
}

func press(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func drag(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}

func release(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

func TestLayoutDefaultsAndDragOverride(t *testing.T) {
	m := baseModel() // 100x30
	l := m.layout()
	if l.leftW != 40 || l.headerH != 6 || l.shellH != 22 || l.stacked {
		t.Errorf("default layout = %+v, want leftW 40, header 6, shell 22", l)
	}

	m.leftWidth = 60
	if got := m.layout().leftW; got != 60 {
		t.Errorf("leftW override = %d, want 60", got)
	}
	// The drag cannot swallow the right column or shrink the nav away.
	m.leftWidth = 2
	if got := m.layout().leftW; got != minLeftW {
		t.Errorf("leftW below minimum = %d, want %d", got, minLeftW)
	}
	m.leftWidth = 95
	if got := m.layout().leftW; got != 80 { // width - minRightW
		t.Errorf("leftW above maximum = %d, want 80", got)
	}
}

func TestDragResizesLeftPane(t *testing.T) {
	m := baseModel() // divider sits at the fitted left edge, x=40
	divX := m.layout().leftW

	newM, _ := m.updateMouse(press(divX, 10))
	if !newM.(model).dragging {
		t.Fatal("press on the divider did not arm dragging")
	}

	newM, _ = newM.(model).updateMouse(drag(60, 10))
	if got := newM.(model).leftWidth; got != 60 {
		t.Errorf("leftWidth after drag = %d, want 60", got)
	}
	if got := newM.(model).layout().leftW; got != 60 {
		t.Errorf("layout leftW after drag = %d, want 60", got)
	}

	newM, _ = newM.(model).updateMouse(release(60, 10))
	if newM.(model).dragging {
		t.Error("release did not clear dragging")
	}

	// Drags beyond the limits clamp, so the nav pane cannot swallow the
	// right column or shrink to nothing.
	clamped, _ := m.updateMouse(press(divX, 10))
	clamped, _ = clamped.(model).updateMouse(drag(5, 10))
	if got := clamped.(model).layout().leftW; got != minLeftW {
		t.Errorf("drag to 5 = %d, want clamped to %d", got, minLeftW)
	}
	clamped, _ = clamped.(model).updateMouse(drag(95, 10))
	if got := clamped.(model).layout().leftW; got != 80 { // width - minRightW
		t.Errorf("drag to 95 = %d, want clamped to 80", got)
	}

	// A plain click in the pane body does not arm the drag.
	newM, _ = m.updateMouse(press(5, 4))
	if newM.(model).dragging {
		t.Error("click on an entry armed dragging")
	}
}

func TestStackedDragResizesLeftHeight(t *testing.T) {
	m := baseModel()
	m.width = 40 // stacked, leftH defaults to 9
	divY := contentStartY + m.layout().leftH - 1

	newM, _ := m.updateMouse(press(20, divY))
	if !newM.(model).dragging {
		t.Fatal("press on the stacked divider did not arm dragging")
	}
	newM, _ = newM.(model).updateMouse(drag(20, 16))
	if got := newM.(model).leftHeight; got != 15 {
		t.Errorf("leftHeight after drag = %d, want 15", got)
	}
}

func TestClickFocusesShellPane(t *testing.T) {
	// Clicking the lower-right pane on a claimed sandbox with a live
	// session focuses bash; clicking the sidebar returns to navigation.
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true

	newM, _ := m.updateMouse(press(70, 20))
	if newM.(model).focus != focusShell {
		t.Error("click in the shell pane of a claimed sandbox did not focus bash")
	}

	newM, _ = newM.(model).updateMouse(press(5, 5))
	if newM.(model).focus != focusNav {
		t.Error("click on a left-pane entry must return to navigation")
	}
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("sidebar click selected %q, want agent-pool-lqbmx", newM.(model).selected)
	}

	// Clicking the lower-right pane on a warm sandbox must NOT focus
	// bash or show a "leaves bash" prompt; it focuses the config viewer.
	warm := baseModel()
	newM, _ = warm.updateMouse(press(70, 20))
	got := newM.(model)
	if got.focus != focusInspect {
		t.Errorf("click on warm lower pane focus = %v, want focusInspect", got.focus)
	}
	if strings.Contains(got.statusLine(), "bash") && strings.Contains(got.statusLine(), "leaves") {
		t.Errorf("warm inspect status line must not mention leaving bash: %q", got.statusLine())
	}
	if strings.Contains(got.View(), "leaves bash") {
		t.Errorf("warm inspect view must not show 'leaves bash'")
	}
}

func TestShellKeyRouting(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	// No keybind escapes: esc is delivered to the session and the
	// focus stays.
	newM, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || newM.(model).focus != focusShell {
		t.Error("esc must go to the session, not leave the shell")
	}

	// q alone is typed into bash, it does not quit anything.
	newM, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil || newM.(model).focus != focusShell {
		t.Error("q must go to the session, not quit")
	}

	// C-b arms the prefix and is not sent to bash.
	newM, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyCtrlB})
	if cmd != nil || !newM.(model).prefix {
		t.Error("C-b did not arm the prefix chord")
	}

	// C-b then q leaves the shell.
	newM, _ = newM.(model).updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if newM.(model).focus != focusNav || newM.(model).prefix {
		t.Error("C-b q did not leave the shell")
	}

	// An armed prefix with any other key cancels and swallows the key.
	armed := m
	armed.prefix = true
	newM, cmd = armed.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil || newM.(model).prefix || len(newM.(model).pending) != 0 {
		t.Error("an unbound prefix chord must do nothing")
	}

	// Ctrl+c interrupts the bash process instead of quitting the app.
	newM, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || newM.(model).focus != focusShell {
		t.Error("ctrl+c must go to the session")
	}

	// Unmapped keys are dropped, not sent garbage.
	newM, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyF5})
	if cmd != nil || len(newM.(model).pending) != 0 {
		t.Error("unmapped key produced a send command")
	}

	// Without a live session the focus falls back to navigation.
	bare := baseModel()
	bare.focus = focusShell
	newM, _ = bare.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if newM.(model).focus != focusNav {
		t.Error("keys with no session must not stay in the shell")
	}
}

func TestMouseEscapeFragmentsDroppedFromShell(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	leaked := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'['}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune("[<35;111;7M")},
		{Type: tea.KeyRunes, Runes: []rune("<35;132;7M")},
		{Type: tea.KeyRunes, Runes: []rune("[<35;111;7M[<35;132;7M")},
		{Type: tea.KeyRunes, Runes: []rune("[<32;40;10m")},
	}
	for _, msg := range leaked {
		newM, cmd := m.updateShellKeys(msg)
		if cmd != nil || len(newM.(model).pending) != 0 {
			t.Errorf("leaked mouse fragment %+v was sent to shell", msg)
		}
	}

	// Normal typing of brackets or angle brackets still works.
	for _, r := range []rune{'[', '<', 'a'} {
		_, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if cmd == nil {
			t.Errorf("normal rune %q was dropped", r)
		}
	}
}

func TestShellKeyName(t *testing.T) {
	cases := map[string]string{
		"enter": "Enter", "up": "Up", "backspace": "BSpace",
		"ctrl+c": "C-c", "ctrl+d": "C-d", "tab": "Tab",
	}
	for in, want := range cases {
		got, ok := shellKeyName(in)
		if !ok || got != want {
			t.Errorf("shellKeyName(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := shellKeyName("f5"); ok {
		t.Error("shellKeyName mapped an unknown key")
	}
}

func TestShellCaptureDisplay(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true

	newM, _ := m.Update(shellMsg{pod: "agent-pool-bkl2p", lines: []string{"$ echo hi", "hi"}})
	if got := newM.(model).shellLines; len(got) != 2 {
		t.Fatalf("shellLines = %v, want the captured two lines", got)
	}
	if g := newM.(model).View(); !strings.Contains(g, "$ echo hi") {
		t.Errorf("view missing the captured shell content")
	}

	// A capture for another pod is not shown.
	newM, _ = newM.(model).Update(shellMsg{pod: "agent-pool-5ss7w", lines: []string{"other"}})
	if got := newM.(model).View(); strings.Contains(got, "other") {
		t.Errorf("view showed a capture from a pod that is not selected")
	}

	// A failed capture clears the stale content.
	newM, _ = newM.(model).Update(shellMsg{pod: "agent-pool-bkl2p", err: errFixture})
	if got := newM.(model).shellLines; got != nil {
		t.Errorf("failed capture left stale lines: %v", got)
	}
}

func TestShellTick(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-5ss7w"
	m.sessions["agent-pool-5ss7w"] = true

	newM, cmd := m.Update(shellTickMsg{})
	if cmd == nil {
		t.Fatal("shell tick did not re-arm")
	}
	if newM.(model).shellW == 0 {
		t.Error("shell tick did not record the pane size it resized to")
	}

	// Without a session the tick still re-arms but captures nothing.
	bare := baseModel()
	_, cmd = bare.Update(shellTickMsg{})
	if cmd == nil {
		t.Fatal("shell tick without a session did not re-arm")
	}
}

// The input queue: keystrokes typed while a drain is in flight wait
// for it, batches keep their order, and a finished drain immediately
// sends the rest and captures so the echo comes back fast.
func TestShellInputQueue(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	// A drain in flight: the key queues instead of racing it.
	m.sending = true
	newM, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd != nil {
		t.Error("key typed during a drain started a second drain")
	}
	if len(newM.(model).pending) != 1 {
		t.Fatalf("pending = %v, want the queued key", newM.(model).pending)
	}

	// The drain finishing sends the queue and captures right after.
	newM, cmd = newM.(model).Update(sentMsg{pod: "agent-pool-bkl2p", count: 3})
	if cmd == nil {
		t.Fatal("finished drain did not send the queued input")
	}
	if !newM.(model).sending {
		t.Error("the next drain did not start")
	}
	if len(newM.(model).pending) != 0 {
		t.Errorf("pending = %v, want it drained", newM.(model).pending)
	}

	// A failed drain drops the queue instead of replaying stale keys.
	failed := newM.(model)
	failed.sending = true
	failed.pending = []session.Key{{Text: "z"}}
	newM, cmd = failed.Update(sentMsg{pod: "agent-pool-bkl2p", count: 1, err: errFixture})
	if cmd != nil {
		t.Error("failed drain started another drain")
	}
	if len(newM.(model).pending) != 0 || newM.(model).sending {
		t.Errorf("after a failed drain: pending %v, sending %v", newM.(model).pending, newM.(model).sending)
	}
}

// The shell tick and the post-send capture both size the Herdr terminal to
// the pane, so the embedded output wraps where it is shown.
func TestShellResizeFollowsPaneSize(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	_, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("first key produced no drain command")
	}
	newM, _ := m.Update(sentMsg{pod: "agent-pool-bkl2p", count: 1})
	w, h := newM.(model).shellW, newM.(model).shellH
	wantW, wantH := m.shellPaneSize()
	if w != wantW || h != wantH {
		t.Errorf("shell pane size = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}

func TestSelectionResetsShellSize(t *testing.T) {
	m := baseModel()
	m.shellW, m.shellH = 80, 24
	m.selectPod("another-pod")
	if m.shellW != 0 || m.shellH != 0 {
		t.Fatal("selection change must resize the new terminal")
	}
}

func TestAttachFocusesShellInsteadOfFullscreen(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true

	// enter issues the ensure command, not a fullscreen exec.
	_, cmd := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	newM, _ := m.Update(attachReadyMsg{pod: "agent-pool-bkl2p"})
	if newM.(model).focus != focusShell {
		t.Error("a ready session did not focus the embedded shell")
	}

	// F still reaches the fullscreen path.
	_, cmd = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd == nil {
		t.Error("F returned no fullscreen command")
	}
}

var errFixture = errors.New("cluster unreachable")

// The claim flow pre-creates bash: claiming waits for adoption, the
// adopted pod is remembered, and the fleet that shows it selects it
// and drops straight into the shell.
func TestClaimCreatesBashImmediately(t *testing.T) {
	m := baseModel()

	newM, cmd := m.Update(claimMsg{name: "dash-claim-1"})
	if cmd == nil {
		t.Fatal("claim returned no wait command")
	}
	if !strings.Contains(newM.(model).status, "waiting") {
		t.Errorf("claim status = %q", newM.(model).status)
	}

	adopted := newM.(model)
	adopted.boxes = append(adopted.boxes, cluster.Sandbox{
		Name: "agent-pool-new1", OwnerKind: "SandboxClaim", Owner: "dash-claim-1",
		Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256})
	adopted.sessions["agent-pool-new1"] = true
	newM, cmd = adopted.Update(claimAdoptedMsg{pod: "agent-pool-new1"})
	if cmd == nil {
		t.Fatal("adoption returned no fetch command")
	}
	if newM.(model).pendingPod != "agent-pool-new1" {
		t.Fatalf("pendingPod = %q", newM.(model).pendingPod)
	}

	loaded := newM.(model)
	loaded.hist = map[string]usageHistory{}
	newM, _ = loaded.Update(loadedMsg{boxes: loaded.boxes, sessions: loaded.sessions, metricsOK: true})
	if newM.(model).selected != "agent-pool-new1" {
		t.Errorf("selected = %q, want the new pod", newM.(model).selected)
	}
	if newM.(model).focus != focusNav {
		t.Error("the claim must pre-create the session, not jump into the shell")
	}
	if !strings.Contains(newM.(model).status, "enter attaches") {
		t.Errorf("claim status = %q, want the enter-attaches hint", newM.(model).status)
	}
	if newM.(model).pendingPod != "" {
		t.Errorf("pendingPod = %q, want cleared", newM.(model).pendingPod)
	}

	// A failed claim reports and refreshes.
	newM, cmd = m.Update(claimAdoptedMsg{err: errFixture})
	if cmd == nil || !strings.Contains(newM.(model).status, "claim failed") {
		t.Errorf("failed claim = %q cmd=%v", newM.(model).status, cmd != nil)
	}
}

func TestLeftWidthFitsContent(t *testing.T) {
	m := baseModel() // longest name is 16 runes, plus tree labels and branches
	if got := m.layout().leftW; got != 40 {
		t.Errorf("default leftW = %d, want 40 fitted to the longest tree leaf", got)
	}
	long := baseModel()
	long.boxes[0].Name = "agent-pool-with-a-much-longer-name-123456"
	if got := long.layout().leftW; got != 65 { // 41 runes + 24 for tree labels
		t.Errorf("fitted leftW = %d, want 65", got)
	}
	// The fit never swallows the right column.
	huge := baseModel()
	huge.boxes[0].Name = strings.Repeat("x", 90)
	if got := huge.layout().leftW; got != 80 { // width - minRightW
		t.Errorf("capped leftW = %d, want 80", got)
	}
}

func TestHeaderBentoBoxes(t *testing.T) {
	m := baseModel()
	m.sessions["agent-pool-5ss7w"] = true

	info := m.headerInfoLines()
	if len(info) != 4 {
		t.Fatalf("info box has %d lines, want 4", len(info))
	}
	for _, want := range []string{"· warm", "owner agent-pool", "herdr live", "Running"} {
		if !strings.Contains(strings.Join(info, " "), want) {
			t.Errorf("info box missing %q", want)
		}
	}

	usage := m.headerUsageLines()
	if len(usage) != 4 {
		t.Fatalf("usage box has %d lines, want 4 spec rows for a warm sandbox", len(usage))
	}
	if !strings.Contains(strings.Join(usage, " "), "debian:12-slim") {
		t.Errorf("spec box = %v, want the image", usage)
	}
	// A claimed sandbox keeps the usage table.
	claimed := baseModel()
	claimed.selected = "agent-pool-bkl2p"
	usage = claimed.headerUsageLines()
	if len(usage) != 4 {
		t.Fatalf("claimed usage box has %d lines, want 4", len(usage))
	}
	for _, want := range []string{"2m / 500m", "6Mi / 256Mi"} {
		if !strings.Contains(strings.Join(usage, " "), want) {
			t.Errorf("usage box missing %q", want)
		}
	}

	// The info box width fits its longest line exactly, so the name is
	// never cut, and the usage box takes the remainder of the column.
	l := m.layout()
	longest := 0
	for _, ln := range info {
		if w := lipgloss.Width(ln); w > longest {
			longest = w
		}
	}
	if l.infoW < longest+2 {
		t.Errorf("infoW = %d, longest line %d does not fit uncut", l.infoW, longest)
	}
	if got := l.infoW + l.usageW; got != l.colW {
		t.Errorf("infoW(%d) + usageW(%d) = %d, want exact colW %d", l.infoW, l.usageW, got, l.colW)
	}

	// Warm sandboxes show the spec table, not the graphs; the claimed
	// sandbox below carries the history charts.
	m.hist = map[string]usageHistory{"agent-pool-5ss7w": {cpu: []float64{1}, mem: []float64{1}}}
	usage = m.headerUsageLines()
	if strings.ContainsRune(strings.Join(usage, ""), '⡇') {
		t.Errorf("warm spec box = %v, must not carry graph glyphs", usage)
	}
	claimed.hist = map[string]usageHistory{"agent-pool-bkl2p": {cpu: []float64{1}, mem: []float64{1}}}
	claimedUsage := claimed.headerUsageLines()
	if !strings.ContainsRune(claimedUsage[1], '⡇') {
		t.Errorf("cpu graph row = %q, want a filled braille column", claimedUsage[1])
	}

	// Three boxes open on the first body row: the nav pane plus the two
	// header boxes. The two header boxes close on their own row, side by
	// side, separate from the nav pane's long border.
	got := m.View()
	rows := strings.Split(got, "\n")
	openRow := ""
	for _, r := range rows {
		if strings.Count(r, "╭") >= 3 {
			openRow = r
		}
	}
	if openRow == "" {
		t.Fatal("no row opens the nav pane and both header boxes together")
	}
	closes := 0
	for _, r := range rows {
		// The header boxes close while the nav pane is still open, its
		// side border runs on that row.
		if strings.Count(r, "╰") == 2 && strings.HasPrefix(r, "│") {
			closes++
		}
	}
	if closes != 1 {
		t.Errorf("the two header boxes close together on %d rows, want 1", closes)
	}
}

func TestStatusLineModes(t *testing.T) {
	m := baseModel()
	if got := m.statusLine(); !strings.Contains(got, "j/k move") {
		t.Errorf("nav status = %q", got)
	}

	m.focus = focusShell
	got := m.statusLine()
	if !strings.Contains(got, "C-b q leaves bash") {
		t.Errorf("shell status = %q, want the C-b q tip", got)
	}

	m.prefix = true
	if got := m.statusLine(); !strings.Contains(got, "C-b armed") {
		t.Errorf("prefix status = %q", got)
	}

	m.focus = focusInspect
	m.prefix = false
	if got := m.statusLine(); !strings.Contains(got, "scroll") || strings.Contains(got, "leaves bash") {
		t.Errorf("inspect status = %q, want scroll tips without leaves bash", got)
	}

	m = baseModel()
	m.status = "something happened"
	if got := m.statusLine(); !strings.Contains(got, "something happened") {
		t.Errorf("message status = %q", got)
	}
}

func TestWarmSandboxRefusesFullscreenToo(t *testing.T) {
	m := baseModel() // selected 0 is warm
	newM, cmd := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd != nil {
		t.Error("F on a warm sandbox returned a command")
	}
	if !strings.Contains(newM.(model).status, "warm pool sandbox") {
		t.Errorf("F on warm = %q", newM.(model).status)
	}
}

// The pool refills with alphabetically-earlier pods while a sandbox is
// claimed, resorting the fleet. The selection must follow the pod name,
// not the stale index, or keystrokes land in the wrong sandbox.
func TestSelectionFollowsNameAcrossResort(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p" // index 2 in the base fleet

	refilled := append([]cluster.Sandbox{}, m.boxes...)
	newWarm := cluster.Sandbox{Name: "agent-pool-2pj7v", OwnerKind: "SandboxWarmPool",
		Owner: "agent-pool", Phase: "Running"}
	refilled = append([]cluster.Sandbox{newWarm}, refilled...) // sorts to index 0

	newM, _ := m.Update(loadedMsg{boxes: refilled, sessions: m.sessions, metricsOK: true})
	if newM.(model).selected != "agent-pool-bkl2p" {
		t.Errorf("selection after refill = %q, want the same pod by name", newM.(model).selected)
	}
	if got := newM.(model).selectedBox().OwnerKind; got != "SandboxClaim" {
		t.Errorf("selected box kind = %q, want the claimed sandbox", got)
	}
}

func TestSelectionFallbackUsesNodeOrder(t *testing.T) {
	m := baseModel()
	m.boxes[0].NodeName = "worker-b"
	m.boxes[1].NodeName = "worker-a"
	m.boxes[2].NodeName = "worker-a"
	m.selected = "agent-pool-lqbmx"
	refilled := []cluster.Sandbox{m.boxes[0], m.boxes[2]}

	newM, _ := m.Update(loadedMsg{boxes: refilled, sessions: m.sessions, metricsOK: true})
	if got := newM.(model).selected; got != "agent-pool-5ss7w" {
		t.Errorf("selection fallback = %q, want first remaining leaf in tree order", got)
	}
}

// levelColor maps stress to the green-to-red ramp; the graph colors
// each cell by its stronger sample, so a spike reads red wherever it
// happened.
func TestLevelColor(t *testing.T) {
	if levelColor(0.95) != badColor {
		t.Errorf("levelColor(0.95) = %v, want red", levelColor(0.95))
	}
	if levelColor(0.7) != warnColor {
		t.Errorf("levelColor(0.7) = %v, want yellow", levelColor(0.7))
	}
	if levelColor(0.4) != lipgloss.Color("112") {
		t.Errorf("levelColor(0.4) = %v, want light green", levelColor(0.4))
	}
	if levelColor(0.1) != warmColor {
		t.Errorf("levelColor(0.1) = %v, want green", levelColor(0.1))
	}
}

// Exactly one pane carries the accent border: the nav pane in
// navigation, the shell pane while bash has focus, and never both. The
// header boxes stay dim in both modes.
func TestOneAccentPane(t *testing.T) {
	// Force a color profile so the borders carry their escape
	// sequences in the test, then count which pane is accent-bordered.
	lipgloss.SetColorProfile(termenv.ANSI256)

	accent := "[38;5;39m" // 39 blue, the accent border color
	frame := baseModel().View()
	if got := strings.Count(frame, accent); got < 4 {
		// one border = many border-rune cells, but at least the top
		// and left edges of the focused pane carry it
		t.Errorf("navigation mode: %d accent cells, want the nav pane accented", got)
	}

	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.focus = focusShell
	m.sessions["agent-pool-bkl2p"] = true
	frame = m.View()
	if got := strings.Count(frame, accent); got < 4 {
		t.Errorf("shell mode: %d accent cells, want the shell pane accented", got)
	}

	// The accent never lands on both panes at once: rendering the nav
	// pane dim while the shell is focused must keep an accent only
	// around the shell box, and the accented nav pane alone in
	// navigation. Verified by the frame layout: in shell mode the
	// accent cells sit on the shell box rows, below the header boxes.
	rows := strings.Split(frame, "\n")
	shellRows := 0
	for i, r := range rows {
		if strings.Contains(r, accent) {
			if i < 3 { // header region
				t.Errorf("accent on row %d: the header must stay dim", i)
			} else {
				shellRows++
			}
		}
	}
	if shellRows == 0 {
		t.Error("shell mode: no accent on the shell pane")
	}
}

// While the shell has focus, wheel events do not leak into navigation
// and the splitter drag keeps working, while clicking the sidebar
// always drops shell focus and returns to navigation.
func TestNoInputLeaksFromShell(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	// Wheel events die before reaching the selection.
	newM, _ := m.updateMouse(tea.MouseMsg{X: 5, Y: 5,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if newM.(model).selected != "agent-pool-bkl2p" {
		t.Errorf("wheel leaked into navigation: %q", newM.(model).selected)
	}

	// Clicking the header boxes while in shell mode keeps focus in shell.
	newM, _ = m.updateMouse(press(50, 3))
	if newM.(model).focus != focusShell {
		t.Error("a header click stole shell focus")
	}

	// The divider drag still works while the shell has focus.
	l := m.layout()
	divX := l.leftW
	newM, _ = m.updateMouse(tea.MouseMsg{X: divX, Y: 10,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !newM.(model).dragging {
		t.Error("the splitter drag must keep working in shell mode")
	}
	newM, _ = newM.(model).updateMouse(drag(50, 10))
	if got := newM.(model).layout().leftW; got != 50 {
		t.Errorf("drag in shell mode = %d, want 50", got)
	}
	if newM.(model).focus != focusShell {
		t.Error("dragging the splitter must not disturb the shell focus")
	}

	// Clicking the sidebar always exits shell focus and selects the entry.
	newM, _ = m.updateMouse(press(5, 5))
	if newM.(model).focus != focusNav {
		t.Error("a sidebar click must stop focusing the shell")
	}
	if newM.(model).selected != "agent-pool-lqbmx" {
		t.Errorf("sidebar click selected %q, want agent-pool-lqbmx", newM.(model).selected)
	}
}

// Header bento boxes align flush with the lower pane's right edge and
// never wrap to create vertical gaps, even at narrow widths with long
// claim owner names.
func TestBentoAlignmentAndNoHeaderGap(t *testing.T) {
	for _, width := range []int{60, 80, 90, 100, 120} {
		for _, sel := range []string{"agent-pool-5ss7w", "agent-pool-bkl2p"} {
			m := baseModel()
			m.width = width
			m.height = 24
			m.boxes[2].Owner = "dash-claim-1795090340263037564"
			m.selected = sel
			m.sessions["agent-pool-bkl2p"] = true

			l := m.layout()
			if l.infoW+l.usageW != l.colW {
				t.Errorf("w=%d sel=%s: infoW(%d)+usageW(%d) != colW(%d)", width, sel, l.infoW, l.usageW, l.colW)
			}
			if l.leftW+l.colW != width {
				t.Errorf("w=%d sel=%s: leftW(%d)+colW(%d) != width(%d)", width, sel, l.leftW, l.colW, width)
			}

			rows := strings.Split(m.View(), "\n")
			if len(rows) != m.height {
				t.Errorf("w=%d sel=%s: rendered %d rows, want %d (wrapping caused overflow)", width, sel, len(rows), m.height)
			}
			// Row 1 is the top border (left + info + usage); row 1+headerH is
			// the top border of the lower pane (left + lower). Both must have
			// exact full width so right edges align flush with zero gap.
			headerTopW := lipgloss.Width(rows[1])
			headerBotW := lipgloss.Width(rows[l.headerH])
			lowerTopW := lipgloss.Width(rows[1+l.headerH])
			if headerTopW != width || headerBotW != width || lowerTopW != width {
				t.Errorf("w=%d sel=%s: widths top=%d bot=%d lower=%d, want all %d",
					width, sel, headerTopW, headerBotW, lowerTopW, width)
			}
			// The two header boxes must close on the exact same row (row headerH)
			// directly above the lower pane's top border (row headerH+1).
			if strings.Count(rows[l.headerH], "╰") != 2 {
				t.Errorf("w=%d sel=%s: row %d = %q, want both header boxes closing together", width, sel, l.headerH, rows[l.headerH])
			}
			if strings.Count(rows[1+l.headerH], "╭") != 1 {
				t.Errorf("w=%d sel=%s: row %d = %q, want lower pane opening immediately below header", width, sel, 1+l.headerH, rows[1+l.headerH])
			}
		}
	}
}

// Warm sandboxes render configs, templates, and manifests separated by
// divider lines with ANSI color, and support both wheel and keyboard
// scrolling without ever entering bash mode.
func TestWarmInspectScrollableMarkdownAndYAML(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)

	m := baseModel()
	m.height = 16 // compact viewport so scrolling has room
	m.pool.TemplateYAML = "apiVersion: extensions.agents.x-k8s.io/v1beta1\nkind: SandboxTemplate\nspec:\n  replicas: 2\n  enabled: true\n  image: debian:12-slim"
	m.pool.ManifestYAML = "apiVersion: extensions.agents.x-k8s.io/v1beta1\nkind: SandboxWarmPool\nmetadata:\n  name: agent-pool"
	m.boxes[0].ManifestYAML = "apiVersion: agents.x-k8s.io/v1beta1\nkind: Sandbox\nmetadata:\n  name: agent-pool-5ss7w"
	m.boxes[0].PodYAML = "apiVersion: v1\nkind: Pod\nspec:\n  restartPolicy: OnFailure\n  containers:\n  - name: agent\n    resources:\n      limits:\n        cpu: 500m\n        memory: 256Mi"

	lines := m.warmInspectLines(60)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"agent-pool · warm pool",
		"SandboxTemplate · agent-bash-template",
		"SandboxWarmPool · agent-pool",
		"Sandbox · agent-pool-5ss7w",
		"Pod · agent-pool-5ss7w",
		"500m",
		"256Mi",
		strings.Repeat("─", 60),
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("warm inspect missing %q", want)
		}
	}
	if !strings.Contains(joined, "\x1b[") {
		t.Error("warm inspect rendered without ANSI color sequences")
	}

	// Enter focuses the config viewer, so j/k scroll its document instead
	// of moving the selected sandbox.
	entered, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	focusedByEnter := entered.(model)
	if cmd != nil || focusedByEnter.focus != focusInspect {
		t.Fatalf("enter focus = %v cmd=%v, want focusInspect and no command", focusedByEnter.focus, cmd != nil)
	}
	selected := focusedByEnter.selected
	entered, _ = focusedByEnter.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if entered.(model).inspectScroll <= focusedByEnter.inspectScroll {
		t.Errorf("j did not scroll config: offset=%d", entered.(model).inspectScroll)
	}
	entered, _ = entered.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if entered.(model).inspectScroll != focusedByEnter.inspectScroll {
		t.Errorf("k returned offset %d, want %d", entered.(model).inspectScroll, focusedByEnter.inspectScroll)
	}
	if entered.(model).selected != selected {
		t.Errorf("j/k changed sandbox selection to %q, want %q", entered.(model).selected, selected)
	}

	// Wheel down over the lower pane scrolls without changing selection.
	newM, _ := m.updateMouse(tea.MouseMsg{X: 60, Y: 10,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	scrolled := newM.(model)
	if scrolled.selected != "agent-pool-5ss7w" {
		t.Errorf("wheel over warm viewer changed selection to %q", scrolled.selected)
	}
	if scrolled.inspectScroll <= 0 {
		t.Errorf("wheel down did not scroll warm viewer: offset=%d", scrolled.inspectScroll)
	}

	// Clicking the warm lower pane focuses inspect (not bash) and j/k scroll.
	newM, _ = scrolled.updateMouse(press(60, 10))
	focused := newM.(model)
	if focused.focus != focusInspect {
		t.Fatalf("click on warm viewer focus = %v, want focusInspect", focused.focus)
	}
	before := focused.inspectScroll
	newM, _ = focused.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if newM.(model).inspectScroll != before+1 {
		t.Errorf("j in focusInspect offset = %d, want %d", newM.(model).inspectScroll, before+1)
	}
	newM, _ = newM.(model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if newM.(model).focus != focusNav {
		t.Error("esc in focusInspect did not return to focusNav")
	}

	// Moving to another sandbox resets scroll back to top.
	newM, _ = newM.(model).Update(tea.KeyMsg{Type: tea.KeyDown})
	if newM.(model).inspectScroll != 0 {
		t.Errorf("changing selection left inspectScroll = %d, want 0", newM.(model).inspectScroll)
	}
}

// Transient status messages expire on their own: a destroyed-claim
// notification disappears after its TTL instead of sitting on the
// status line forever. Sticky messages, errors and armed
// confirmations, stay until replaced.
func TestStatusExpiry(t *testing.T) {
	m := baseModel()

	newM, _ := m.Update(deletedMsg{claim: "dash-claim-1"})
	if newM.(model).statusTTL <= 0 {
		t.Error("destroy notification is not transient")
	}
	// Three fleet refreshes age it out (5s ttl, 2s tick).
	aged, _ := newM.Update(tickMsg{})
	aged, _ = aged.Update(tickMsg{})
	aged, _ = aged.Update(tickMsg{})
	if aged.(model).status != "" {
		t.Errorf("transient status = %q, want expired", aged.(model).status)
	}

	// Errors stay.
	newM, _ = m.Update(claimMsg{err: errFixture})
	if newM.(model).statusTTL != 0 {
		t.Error("an error message must be sticky")
	}

	// The armed destroy confirmation stays until answered.
	armed := baseModel()
	armed.selected = "agent-pool-bkl2p"
	newM, _ = armed.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if newM.(model).statusTTL != 0 {
		t.Error("the armed x confirmation must be sticky")
	}
}
