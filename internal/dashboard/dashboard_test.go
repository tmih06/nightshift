package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
		pools: map[string]cluster.Pool{"agent-pool": {Name: "agent-pool", Template: "agent-bash-template",
			Ready: 2, Desired: 2, UpdateStrategy: "Recreate"}},
		sessions: map[string]bool{}, metricsOK: true,
		loaded: true, width: 140, height: 30}
}

// The table draws gh-dash's header and two lines plus a rule per sandbox.
func TestLeftLinesTree(t *testing.T) {
	m := baseModel()
	m.sessions["agent-pool-bkl2p"] = true
	l := m.layout()
	table := m.tableView(l)
	plain := ansi.Strip(strings.Join(table, "\n"))
	for _, want := range []string{"Sandbox", "CPU", "MEM", "agent-pool-5ss7w", "agent-pool-bkl2p", "unassigned · my-agent", "2m/500m", "6Mi/256Mi"} {
		if !strings.Contains(plain, want) {
			t.Errorf("table missing %q:\n%s", want, plain)
		}
	}
	if len(table) != l.tableH {
		t.Errorf("table is %d rows, want %d", len(table), l.tableH)
	}
	for i, row := range table {
		if w := ansi.StringWidth(row); w != l.tableW {
			t.Errorf("table row %d is %d wide, want %d", i, w, l.tableW)
		}
	}
	rows := strings.Split(plain, "\n")
	claimed := rowOf(t, m, "agent-pool-bkl2p") - l.bodyTop
	if !strings.Contains(rows[claimed], "●") {
		t.Errorf("live session row = %q, want ●", rows[claimed])
	}
	if warm := rowOf(t, m, "agent-pool-5ss7w") - l.bodyTop; strings.Contains(rows[warm], "●") {
		t.Errorf("dead session row = %q, want ○", rows[warm])
	}
	if !strings.Contains(rows[claimed+2], "───") {
		t.Errorf("row under a sandbox = %q, want gh-dash's separator", rows[claimed+2])
	}
}

// The selected row carries gh-dash's selection background and no other does.
func TestSelectedRowIsHighlighted(t *testing.T) {
	m := baseModel()
	bgSeq := strings.TrimSuffix(lipgloss.NewStyle().Background(th.SelectedBackground).Render("x"), "x\x1b[m")
	l := m.layout()
	table := m.tableView(l)
	sel := rowOf(t, m, "agent-pool-5ss7w") - l.bodyTop
	other := rowOf(t, m, "agent-pool-lqbmx") - l.bodyTop
	if !strings.Contains(table[sel], bgSeq) || !strings.Contains(table[sel+1], bgSeq) {
		t.Error("the selected row is not highlighted")
	}
	if strings.Contains(table[other], bgSeq) {
		t.Error("an unselected row is highlighted")
	}
}

func TestNodeTreeOrderingAndNavigation(t *testing.T) {
	m := baseModel()
	m.boxes[0].NodeName = "node-b"
	m.boxes[1].NodeName = "node-a"
	m.boxes[2].NodeName = "node-a"
	order := []string{}
	for _, i := range m.navigationIndices() {
		order = append(order, m.boxes[i].Name)
	}
	want := []string{"agent-pool-lqbmx", "agent-pool-5ss7w", "agent-pool-bkl2p"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}

	m.selected = "agent-pool-lqbmx"
	m.moveSelection(1)
	if m.selected != "agent-pool-5ss7w" {
		t.Errorf("j moved to %q, want node-b's warm sandbox", m.selected)
	}
	m.moveSelection(1)
	if m.selected != "agent-pool-bkl2p" {
		t.Errorf("j moved to %q, want the claimed sandbox", m.selected)
	}
	m = m.clickTable(5, rowOf(t, m, "agent-pool-lqbmx")+1)
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("clicking a row's second line selected %q", m.selected)
	}
}

// Sections filter the table the way gh-dash's section tabs do.
func TestSectionsFilterTheTable(t *testing.T) {
	m := baseModel()
	tabs := ansi.Strip(m.tabsView())
	for _, want := range []string{"All (3)", "Warm (2)", "Claimed (1)", "ns sandbox-demo", "━"} {
		if !strings.Contains(tabs, want) {
			t.Errorf("section tabs %q missing %q", tabs, want)
		}
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if next.section != sectionWarm || len(next.navigationIndices()) != 2 || next.selected != "agent-pool-5ss7w" {
		t.Errorf("l = section %d, %d rows, selected %q", next.section, len(next.navigationIndices()), next.selected)
	}
	next, _ = next.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if next.section != sectionClaimed || next.selected != "agent-pool-bkl2p" {
		t.Errorf("second l = section %d, selected %q; want claimed and its sandbox", next.section, next.selected)
	}
	if plain := ansi.Strip(strings.Join(next.tableView(next.layout()), "\n")); strings.Contains(plain, "agent-pool-5ss7w") {
		t.Error("the claimed section still lists a warm sandbox")
	}
	next, _ = next.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if next.section != sectionWarm {
		t.Errorf("h = section %d, want warm", next.section)
	}

	// A click on a section tab opens it.
	s := m.sectionSpans()[sectionClaimed]
	if got := m.clickTable(s.from+1, 0); got.section != sectionClaimed {
		t.Errorf("clicking the claimed tab left section %d", got.section)
	}

	empty := baseModel()
	empty.boxes = empty.boxes[:2]
	empty.setSection(sectionClaimed)
	if plain := ansi.Strip(strings.Join(empty.tableView(empty.layout()), "\n")); !strings.Contains(plain, "No claimed sandboxes") {
		t.Errorf("empty section = %q", plain)
	}
}

func TestTreesHandleEmptyAndOtherGroups(t *testing.T) {
	m := baseModel()
	m.boxes = []cluster.Sandbox{
		{Name: "unknown", OwnerKind: "Other", NodeName: "node-a"},
		{Name: "claimed", OwnerKind: "SandboxClaim", NodeName: "node-a"},
	}
	m.selected = "claimed"
	if got := m.sectionLabel(sectionWarm); got != "Warm (0)" {
		t.Errorf("warm label = %q", got)
	}
	m.moveSelection(1)
	if m.selected != "unknown" {
		t.Errorf("navigation selected %q, want the other sandbox after the claim", m.selected)
	}
}

func TestNodeTreeScrollKeepsSelectedLeafVisible(t *testing.T) {
	m := baseModel()
	m.height = 16
	m.boxes = make([]cluster.Sandbox, 8)
	for i := range m.boxes {
		letter := string(rune('a' + i))
		m.boxes[i] = cluster.Sandbox{Name: "sandbox-" + letter, NodeName: "node-" + letter, OwnerKind: "SandboxWarmPool"}
	}
	m.selected = "sandbox-h"
	y := rowOf(t, m, "sandbox-h")
	l := m.layout()
	if y < l.bodyTop+tableHeaderH || y >= l.bodyTop+l.tableH {
		t.Fatalf("selected sandbox drawn at row %d, outside the table", y)
	}
	if !strings.Contains(ansi.Strip(m.View()), "sandbox-h") {
		t.Error("the selected sandbox scrolled off")
	}
	if m = m.clickTable(5, y+1); m.selected != "sandbox-h" {
		t.Errorf("clicking the visible selection changed it to %q", m.selected)
	}
}

func TestMouseClickSelectsEntry(t *testing.T) {
	m := baseModel()
	m = m.clickTable(5, rowOf(t, m, "agent-pool-lqbmx"))
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("selected = %q after clicking it, want agent-pool-lqbmx", m.selected)
	}
	if m = m.clickTable(5, rowOf(t, m, "agent-pool-lqbmx")+2); m.selected != "agent-pool-lqbmx" {
		t.Errorf("a click on the separator moved the selection to %q", m.selected)
	}
	x, y := inMain(m)
	if m = m.clickTable(x, y); m.selected != "agent-pool-lqbmx" {
		t.Errorf("a sidebar click moved the selection to %q", m.selected)
	}
	if m = m.clickTable(5, m.height-3); m.selected != "agent-pool-lqbmx" {
		t.Errorf("an empty-row click moved the selection to %q", m.selected)
	}
}

func TestMouseWheelMoves(t *testing.T) {
	m := baseModel()
	newM, _ := m.updateMouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if newM.selected != "agent-pool-lqbmx" {
		t.Errorf("wheel down moved selection to %q, want agent-pool-lqbmx", newM.selected)
	}
	newM, _ = newM.updateMouse(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	if newM.selected != "agent-pool-5ss7w" {
		t.Errorf("wheel up moved selection to %q, want agent-pool-5ss7w", newM.selected)
	}
}

func TestStackedClickUsesY(t *testing.T) {
	m := baseModel()
	m.width, m.height = 80, 40
	if !m.layout().stacked {
		t.Fatal("80 columns did not stack the sidebar under the table")
	}
	m = m.clickTable(5, rowOf(t, m, "agent-pool-lqbmx"))
	if m.selected != "agent-pool-lqbmx" {
		t.Errorf("stacked click selected %q, want agent-pool-lqbmx", m.selected)
	}
}

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

func TestUsageCells(t *testing.T) {
	plain := func(c [2]string) string { return ansi.Strip(c[0] + "|" + c[1]) }
	same := func(s lipgloss.Style) lipgloss.Style { return s }
	if got := plain(usageCell(true, 250, 500, "m", []float64{0, 0.5, 1}, same)); !strings.Contains(got, "250m/500m") || !strings.Contains(got, "50%") {
		t.Errorf("usage cell = %q", got)
	}
	if got := plain(usageCell(false, 0, 256, "Mi", nil, same)); !strings.Contains(got, "--/256Mi") || !strings.Contains(got, "·") {
		t.Errorf("unknown usage cell = %q", got)
	}
	if got := plain(usageCell(true, 4, 0, "m", nil, same)); !strings.Contains(got, "no limit") {
		t.Errorf("limitless cell = %q", got)
	}
}

// The sidebar's usage is a table: columns stay put as the numbers change
// width, and CPU and MEM line up with each other.
func TestUsageTableHoldsItsColumns(t *testing.T) {
	render := func(cpu, mem int64) []string {
		rows := usageTable([]usageRow{
			{"CPU", cpu, 500, "m", []float64{0.1, 0.9}},
			{"MEM", mem, 256, "Mi", []float64{0.01}},
		}, true, 60)
		for i := range rows {
			rows[i] = ansi.Strip(rows[i])
		}
		return rows
	}
	a, b := render(7, 3), render(499, 128)
	if !strings.Contains(a[0], "7m/500m") || !strings.Contains(b[1], "128Mi/256Mi") {
		t.Fatalf("usage table = %q / %q", a, b)
	}
	for i := range a {
		if len(a[i]) != len(b[i]) || strings.Index(a[i], "/") != strings.Index(b[i], "/") || strings.Index(a[i], "%") != strings.Index(b[i], "%") {
			t.Errorf("row %d moved:\n%q\n%q", i, a[i], b[i])
		}
	}
	if strings.Index(a[0], "/") != strings.Index(a[1], "/") || strings.Index(a[0], "%") != strings.Index(a[1], "%") {
		t.Errorf("CPU and MEM columns misaligned:\n%q\n%q", a[0], a[1])
	}
}

func TestKeyboardNavigationAndActions(t *testing.T) {
	m := baseModel()

	newM, _ := m.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.selected != "agent-pool-lqbmx" {
		t.Errorf("j moved selection to %q, want agent-pool-lqbmx", newM.selected)
	}

	// Destroy on a warm sandbox is refused.
	newM, _ = m.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if newM.status != "only a claimed sandbox can be destroyed" {
		t.Errorf("x on warm = %q", newM.status)
	}

	// Enter on a warm sandbox opens its config viewer; b still refuses
	// because a warm sandbox has no bash session.
	warm := m
	newM, cmd := warm.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || newM.focus != focusInspect {
		t.Errorf("enter on warm = focus %v, cmd %v; want focusInspect and no command", newM.focus, cmd != nil)
	}
	newM, _ = warm.updateKeyboard(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if newM.status != "warm pool sandbox — claim it with c, bash starts with the claim" {
		t.Errorf("b on warm = %q", newM.status)
	}

	// Enter on a pod that is not running is refused with a reason.
	claim := m
	claim.boxes[2].Phase = "Pending"
	claim.selected = "agent-pool-bkl2p"
	newM, _ = claim.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyEnter})
	if newM.status != "pod is Pending, no bash to enter" {
		t.Errorf("enter on pending = %q", newM.status)
	}

	// Enter on a running claimed pod returns the attach command, which
	// the caller executes (not here: it would start a real Herdr session).
	running := baseModel()
	running.selected = "agent-pool-bkl2p"
	_, cmd = running.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Error("enter on a running pod returned no command")
	}

	// q quits.
	_, cmd = m.updateKeyboard(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Error("q returned no quit command")
	}
}

func TestClaimShortcuts(t *testing.T) {
	msg := tea.KeyPressMsg{Code: 'c', Text: "c"}
	for _, tc := range []struct{ selected, pool string }{
		{"agent-pool-5ss7w", "agent-pool"},
		{"agent-pool-bkl2p", "pi-pool"},
	} {
		nav := baseModel()
		nav.selected = tc.selected
		newM, cmd := nav.Update(msg)
		if cmd == nil || !strings.Contains(newM.status, "claiming from "+tc.pool) {
			t.Errorf("c on %s did not claim %s", tc.selected, tc.pool)
		}
	}

	inspect := baseModel()
	inspect.focus = focusInspect
	newM, cmd := inspect.Update(msg)
	if cmd == nil || newM.focus != focusNav || !strings.Contains(newM.status, "claiming from agent-pool") {
		t.Error("c in config view did not start a claim")
	}

	other := baseModel()
	other.boxes[0].Owner = "codex-pool"
	for _, focus := range []focusPane{focusNav, focusInspect} {
		other.focus = focus
		got, cmd := other.Update(msg)
		if cmd == nil || !strings.Contains(got.status, "claiming from codex-pool") || other.claimPool() != "codex-pool" {
			t.Errorf("c in focus %v did not select codex-pool: %q", focus, got.status)
		}
	}

	uppercase := tea.KeyPressMsg{Code: 'C', Text: "C"}
	for _, focus := range []focusPane{focusNav, focusInspect} {
		m := baseModel()
		m.focus = focus
		newM, cmd := m.Update(uppercase)
		if cmd != nil || newM.focus != focus || newM.status != "" {
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
	got := newM
	if cmd != nil || got.focus != focusShell || len(got.pending) != 1 || got.pending[0].Text != "C" {
		t.Errorf("uppercase C in bash = focus %v pending %v cmd=%v, want queued text", got.focus, got.pending, cmd != nil)
	}
}

// Destroy needs two presses on a claimed sandbox, and any other key
// disarms it.
func TestDestroyConfirmation(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p" // the claimed sandbox, owner my-agent

	newM, _ := m.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	armed := newM
	if !armed.confirmDestroy || !strings.Contains(armed.status, "press x again") {
		t.Errorf("first x = %v %q", armed.confirmDestroy, armed.status)
	}
	if _, cmd := armed.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Error("navigation while armed must not destroy anything")
	}

	disarmed, _ := armed.updateKeyboard(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if disarmed.confirmDestroy {
		t.Error("a navigation key must disarm the confirmation")
	}

	// Arming again and pressing x returns the delete command.
	armed2, _ := m.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	_, cmd := armed2.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
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
	if newM.selected != "agent-pool-lqbmx" {
		t.Errorf("selection after shrink = %q, want the neighbor agent-pool-lqbmx", newM.selected)
	}

	newM, _ = m.Update(claimMsg{err: errFixture})
	if !strings.Contains(newM.status, "claim failed") {
		t.Errorf("claim failure status = %q", newM.status)
	}

	newM, _ = m.Update(claimMsg{name: "dash-claim-1"})
	if !strings.Contains(newM.status, "dash-claim-1") {
		t.Errorf("claim status = %q", newM.status)
	}

	newM, _ = m.Update(attachReadyMsg{pod: "agent-pool-5ss7w", err: errFixture})
	if !strings.Contains(newM.status, "session failed") {
		t.Errorf("attach failure status = %q", newM.status)
	}

	newM, _ = m.Update(resumeMsg{})
	if !strings.Contains(newM.status, "session still running") {
		t.Errorf("detach status = %q", newM.status)
	}
}

func TestViewFrame(t *testing.T) {
	m := baseModel()
	m.sessions["agent-pool-5ss7w"] = true
	got := m.plainView()
	for _, want := range []string{
		"All (3)", "Warm (2)", "Claimed (1)", "Sandbox", "CPU", "MEM",
		"sandbox-demo · agent-pool", "agent-pool-5ss7w", "Warm", "● Running", "herdr live",
		"debian:12-slim", "cpu 100/500m", "mem 128Mi/256Mi", "Configs",
		"agent-pool-lqbmx", "agent-pool-bkl2p", "Sandbox 1/3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q", want)
		}
	}
	rows := strings.Split(got, "\n")
	if len(rows) != m.height {
		t.Fatalf("view is %d rows, want %d", len(rows), m.height)
	}
	l := m.layout()
	for _, r := range rows[l.bodyTop : l.bodyTop+l.bodyH] {
		if []rune(r)[l.sideX] != '│' {
			t.Fatalf("row %q does not carry the sidebar's rule at column %d", r, l.sideX)
		}
	}

	m.selected = "agent-pool-bkl2p"
	g := m.plainView()
	for _, want := range []string{"sandbox-demo · my-agent", "Claimed", "herdr off", "2m/500m", "Shell",
		"no session — claim with c, or enter on a claimed sandbox"} {
		if !strings.Contains(g, want) {
			t.Errorf("claimed view missing %q", want)
		}
	}
	m.selected = "agent-pool-5ss7w"
	if g := m.plainView(); strings.Contains(g, "no session — claim with") {
		t.Error("a warm sandbox must not show the shell")
	}
}

func TestViewStates(t *testing.T) {
	m := baseModel()
	m.metricsOK = false
	if got := m.plainView(); !strings.Contains(got, "live usage unavailable") {
		t.Errorf("metrics-off status = missing the warning")
	}

	m.err = errFixture
	if got := m.plainView(); !strings.Contains(got, "fetch error") {
		t.Errorf("error view missing the error banner, got %q", got)
	}

	m = baseModel()
	m.boxes = nil
	if got := m.plainView(); !strings.Contains(got, "No sandboxes") {
		t.Errorf("empty view = %q", got)
	}

	if got := (model{sessions: map[string]bool{}}).plainView(); !strings.Contains(got, "loading") {
		t.Errorf("initial view = %q", got)
	}

	// A narrow terminal puts the preview under the table.
	m = baseModel()
	m.width = 80
	l := m.layout()
	rows := strings.Split(m.plainView(), "\n")
	if !l.stacked || !strings.Contains(rows[l.sideTop+1], "sandbox-demo") {
		t.Errorf("stacked layout did not put the preview under the table: %q", rows[l.sideTop+1])
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
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func drag(x, y int) tea.MouseMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func release(x, y int) tea.MouseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func TestLayoutDefaultsAndDragOverride(t *testing.T) {
	m := baseModel() // 140x30
	l := m.layout()
	if l.sideW != 70 || l.tableW != 70 || l.bodyTop != tabsH || l.bodyH != 27 || l.stacked {
		t.Errorf("default layout = %+v", l)
	}
	if l.docW != l.sideW-4 || l.docTop != l.bodyTop+infoRows {
		t.Errorf("sidebar body = %+v", l)
	}
	m.sideWidth = 50
	if got := m.layout().sideW; got != 50 {
		t.Errorf("sideW override = %d, want 50", got)
	}
	m.sideWidth = 5
	if got := m.layout().sideW; got != minSideW {
		t.Errorf("sideW below minimum = %d, want %d", got, minSideW)
	}
	m.sideWidth = 135
	if got := m.layout().sideW; got != m.width-minTableW {
		t.Errorf("sideW above maximum = %d, want %d", got, m.width-minTableW)
	}
}

func TestDragResizesLeftPane(t *testing.T) {
	m := baseModel()
	divX := m.layout().sideX

	newM, _ := m.updateMouse(press(divX, 10))
	if !newM.dragging {
		t.Fatal("press on the divider did not arm dragging")
	}
	newM, _ = newM.updateMouse(drag(80, 10))
	if got := newM.layout().sideX; got != 80 {
		t.Errorf("divider after drag = %d, want 80", got)
	}
	newM, _ = newM.updateMouse(release(80, 10))
	if newM.dragging {
		t.Error("release did not clear dragging")
	}
	newM, _ = m.updateMouse(press(5, rowOf(t, m, "agent-pool-5ss7w")))
	if newM.dragging {
		t.Error("click on a row armed dragging")
	}
}

func TestStackedDragResizesLeftHeight(t *testing.T) {
	m := baseModel()
	m.width, m.height = 80, 40
	l := m.layout()
	newM, _ := m.updateMouse(press(20, l.sideTop))
	if !newM.dragging {
		t.Fatal("press on the stacked divider did not arm dragging")
	}
	newM, _ = newM.updateMouse(drag(20, 20))
	if got := newM.layout().tableH; got != 20-l.bodyTop {
		t.Errorf("tableH after drag = %d, want %d", got, 20-l.bodyTop)
	}
}

func TestClickFocusesShellPane(t *testing.T) {
	// Clicking the lower-right pane on a claimed sandbox with a live
	// session focuses bash; clicking the sidebar returns to navigation.
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true

	newM, _ := m.updateMouse(press(inMain(m)))
	if newM.focus != focusShell {
		t.Error("click in the shell pane of a claimed sandbox did not focus bash")
	}

	newM, _ = newM.updateMouse(press(5, rowOf(t, newM, "agent-pool-lqbmx")))
	if newM.focus != focusNav {
		t.Error("click on a left-pane entry must return to navigation")
	}
	if newM.selected != "agent-pool-lqbmx" {
		t.Errorf("sidebar click selected %q, want agent-pool-lqbmx", newM.selected)
	}

	// Clicking the lower-right pane on a warm sandbox must NOT focus
	// bash or show a "leaves bash" prompt; it focuses the config viewer.
	warm := baseModel()
	newM, _ = warm.updateMouse(press(inMain(m)))
	got := newM
	if got.focus != focusInspect {
		t.Errorf("click on warm lower pane focus = %v, want focusInspect", got.focus)
	}
	if strings.Contains(got.plainStatus(), "bash") && strings.Contains(got.plainStatus(), "leaves") {
		t.Errorf("warm inspect status line must not mention leaving bash: %q", got.plainStatus())
	}
	if strings.Contains(got.plainView(), "leaves bash") {
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
	newM, cmd := m.updateShellKeys(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil || newM.focus != focusShell {
		t.Error("esc must go to the session, not leave the shell")
	}

	// q alone is typed into bash, it does not quit anything.
	newM, cmd = m.updateShellKeys(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil || newM.focus != focusShell {
		t.Error("q must go to the session, not quit")
	}

	// C-b arms the prefix and is not sent to bash.
	newM, cmd = m.updateShellKeys(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if cmd != nil || !newM.prefix {
		t.Error("C-b did not arm the prefix chord")
	}

	// C-b then q leaves the shell.
	newM, _ = newM.updateShellKeys(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if newM.focus != focusNav || newM.prefix {
		t.Error("C-b q did not leave the shell")
	}

	// An armed prefix with any other key cancels and swallows the key.
	armed := m
	armed.prefix = true
	newM, cmd = armed.updateShellKeys(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil || newM.prefix || len(newM.pending) != 0 {
		t.Error("an unbound prefix chord must do nothing")
	}

	// Ctrl+c interrupts the bash process instead of quitting the app.
	newM, cmd = m.updateShellKeys(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || newM.focus != focusShell {
		t.Error("ctrl+c must go to the session")
	}

	// Unmapped keys are dropped, not sent garbage.
	newM, cmd = m.updateShellKeys(tea.KeyPressMsg{Code: tea.KeyF5})
	if cmd != nil || len(newM.pending) != 0 {
		t.Error("unmapped key produced a send command")
	}

	// Without a live session the focus falls back to navigation.
	bare := baseModel()
	bare.focus = focusShell
	newM, _ = bare.updateShellKeys(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if newM.focus != focusNav {
		t.Error("keys with no session must not stay in the shell")
	}
}

func TestMouseEscapeFragmentsDroppedFromShell(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	leaked := []tea.KeyPressMsg{
		{Code: '[', Mod: tea.ModAlt},
		{Code: '[', Text: "[<35;111;7M"},
		{Code: '<', Text: "<35;132;7M"},
		{Code: '[', Text: "[<35;111;7M[<35;132;7M"},
		{Code: '[', Text: "[<32;40;10m"},
	}
	for _, msg := range leaked {
		newM, cmd := m.updateShellKeys(msg)
		if cmd != nil || len(newM.pending) != 0 {
			t.Errorf("leaked mouse fragment %+v was sent to shell", msg)
		}
	}

	// Normal typing of brackets or angle brackets still works.
	for _, r := range []rune{'[', '<', 'a'} {
		_, cmd := m.updateShellKeys(tea.KeyPressMsg{Code: r, Text: string(r)})
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
	if got := newM.shellLines; len(got) != 2 {
		t.Fatalf("shellLines = %v, want the captured two lines", got)
	}
	if g := newM.plainView(); !strings.Contains(g, "$ echo hi") {
		t.Errorf("view missing the captured shell content")
	}

	// A capture for another pod is not shown.
	newM, _ = newM.Update(shellMsg{pod: "agent-pool-5ss7w", lines: []string{"other"}})
	if got := newM.plainView(); strings.Contains(got, "other") {
		t.Errorf("view showed a capture from a pod that is not selected")
	}

	// An idle watch keeps the previous screen and re-arms the watcher.
	newM, cmd := newM.Update(shellMsg{pod: "agent-pool-bkl2p"})
	if len(newM.shellLines) != 2 || cmd == nil || !newM.watchingShell {
		t.Fatal("idle watch discarded the screen or did not re-arm")
	}
	if newM.watchShell() != nil {
		t.Fatal("started a second watch while one was in flight")
	}

	// A failed capture clears the stale content.
	newM, _ = newM.Update(shellMsg{pod: "agent-pool-bkl2p", err: errFixture})
	if got := newM.shellLines; got != nil {
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
	if newM.shellW == 0 {
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
// sends the rest. The screen watcher handles echo independently.
func TestShellInputQueue(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	// A drain in flight: the key queues instead of racing it.
	m.sending = true
	newM, cmd := m.updateShellKeys(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd != nil {
		t.Error("key typed during a drain started a second drain")
	}
	if len(newM.pending) != 1 {
		t.Fatalf("pending = %v, want the queued key", newM.pending)
	}

	// The drain finishing sends the queued keys.
	newM, cmd = newM.Update(sentMsg{pod: "agent-pool-bkl2p", count: 3})
	if cmd == nil {
		t.Fatal("finished drain did not send the queued input")
	}
	if !newM.sending {
		t.Error("the next drain did not start")
	}
	if len(newM.pending) != 0 {
		t.Errorf("pending = %v, want it drained", newM.pending)
	}

	// A failed drain drops the queue instead of replaying stale keys.
	failed := newM
	failed.sending = true
	failed.pending = []session.Key{{Text: "z"}}
	newM, cmd = failed.Update(sentMsg{pod: "agent-pool-bkl2p", count: 1, err: errFixture})
	if cmd != nil {
		t.Error("failed drain started another drain")
	}
	if len(newM.pending) != 0 || newM.sending {
		t.Errorf("after a failed drain: pending %v, sending %v", newM.pending, newM.sending)
	}
}

// The shell watcher sizes Herdr to the embedded pane.
func TestShellResizeFollowsPaneSize(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	newM, _ := m.Update(shellTickMsg{})
	w, h := newM.shellW, newM.shellH
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
	_, cmd := m.updateKeyboard(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	newM, _ := m.Update(attachReadyMsg{pod: "agent-pool-bkl2p"})
	if newM.focus != focusShell {
		t.Error("a ready session did not focus the embedded shell")
	}

	// F still reaches the fullscreen path.
	_, cmd = m.updateKeyboard(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if cmd == nil {
		t.Error("F returned no fullscreen command")
	}
}

var errFixture = errors.New("cluster unreachable")

// rowOf is the frame row of the first line of the named sandbox's table row.
func rowOf(t *testing.T, m model, name string) int {
	t.Helper()
	l := m.layout()
	for pos, i := range m.navigationIndices() {
		if m.boxes[i].Name == name {
			return l.bodyTop + tableHeaderH + (pos-m.tableOffset(l))*itemH
		}
	}
	t.Fatalf("%s is not in the table", name)
	return -1
}

// inMain is a point inside the sidebar's body, the shell or the configs.
func inMain(m model) (int, int) {
	l := m.layout()
	return l.sideX + 5, l.docTop + 1
}

func (m model) plainView() string   { return ansi.Strip(m.View()) }
func (m model) plainStatus() string { return ansi.Strip(m.statusLine()) }

// The claim flow pre-creates bash: claiming waits for adoption, the
// adopted pod is remembered, and the fleet that shows it selects it
// and drops straight into the shell.
func TestClaimCreatesBashImmediately(t *testing.T) {
	m := baseModel()

	newM, cmd := m.Update(claimMsg{name: "dash-claim-1"})
	if cmd == nil {
		t.Fatal("claim returned no wait command")
	}
	if !strings.Contains(newM.status, "waiting") {
		t.Errorf("claim status = %q", newM.status)
	}

	adopted := newM
	adopted.boxes = append(adopted.boxes, cluster.Sandbox{
		Name: "agent-pool-new1", OwnerKind: "SandboxClaim", Owner: "dash-claim-1",
		Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256})
	adopted.sessions["agent-pool-new1"] = true
	newM, cmd = adopted.Update(claimAdoptedMsg{pod: "agent-pool-new1"})
	if cmd == nil {
		t.Fatal("adoption returned no fetch command")
	}
	if newM.pendingPod != "agent-pool-new1" {
		t.Fatalf("pendingPod = %q", newM.pendingPod)
	}

	loaded := newM
	loaded.hist = map[string]usageHistory{}
	newM, _ = loaded.Update(loadedMsg{boxes: loaded.boxes, sessions: loaded.sessions, metricsOK: true})
	if newM.selected != "agent-pool-new1" {
		t.Errorf("selected = %q, want the new pod", newM.selected)
	}
	if newM.focus != focusNav {
		t.Error("the claim must pre-create the session, not jump into the shell")
	}
	if !strings.Contains(newM.status, "enter attaches") {
		t.Errorf("claim status = %q, want the enter-attaches hint", newM.status)
	}
	if newM.pendingPod != "" {
		t.Errorf("pendingPod = %q, want cleared", newM.pendingPod)
	}

	// A failed claim reports and refreshes.
	newM, cmd = m.Update(claimAdoptedMsg{err: errFixture})
	if cmd == nil || !strings.Contains(newM.status, "claim failed") {
		t.Errorf("failed claim = %q cmd=%v", newM.status, cmd != nil)
	}
}

func TestNarrowFramesStack(t *testing.T) {
	m := baseModel()
	m.width = 80
	l := m.layout()
	if !l.stacked || l.tableW != 80 || l.sideTop != l.bodyTop+l.tableH {
		t.Errorf("stacked layout = %+v", l)
	}
	rows := strings.Split(m.plainView(), "\n")
	if !strings.HasPrefix(rows[l.sideTop], "━") {
		t.Errorf("row %d = %q, want the preview's rule", l.sideTop, rows[l.sideTop])
	}
}

// The sidebar heads the sandbox as gh-dash heads a pull request.
func TestHeaderAndRail(t *testing.T) {
	m := baseModel()
	m.sessions["agent-pool-5ss7w"] = true
	info := m.infoLines(60)
	if len(info) != infoRows {
		t.Fatalf("info block is %d rows, want %d", len(info), infoRows)
	}
	head := ansi.Strip(strings.Join(info, "\n"))
	for _, want := range []string{"sandbox-demo · agent-pool", "agent-pool-5ss7w", "Warm", "● Running", "node unassigned",
		"debian:12-slim", "● herdr live", "Configs", "⏎ or click to focus"} {
		if !strings.Contains(head, want) {
			t.Errorf("info missing %q: %q", want, head)
		}
	}
	m.focus = focusInspect
	if head := ansi.Strip(strings.Join(m.infoLines(60), "\n")); !strings.Contains(head, "esc leaves") {
		t.Errorf("focused configs heading = %q", head)
	}

	claimed := baseModel()
	claimed.selected = "agent-pool-bkl2p"
	claimed.sessions["agent-pool-bkl2p"] = true
	claimed.focus = focusShell
	claimed.hist = map[string]usageHistory{"agent-pool-bkl2p": {cpu: []float64{1}, mem: []float64{1}}}
	head = ansi.Strip(strings.Join(claimed.infoLines(80), "\n"))
	for _, want := range []string{"Claimed", "2m/500m", "6Mi/256Mi", "⡇", "Shell", "C-b q leaves"} {
		if !strings.Contains(head, want) {
			t.Errorf("claimed info missing %q: %q", want, head)
		}
	}
}

func TestStatusLineModes(t *testing.T) {
	m := baseModel()
	if got := m.plainStatus(); !strings.Contains(got, "j/k move") {
		t.Errorf("nav status = %q", got)
	}

	m.focus = focusShell
	got := m.plainStatus()
	if !strings.Contains(got, "C-b q leaves bash") {
		t.Errorf("shell status = %q, want the C-b q tip", got)
	}

	m.prefix = true
	if got := m.plainStatus(); !strings.Contains(got, "C-b armed") {
		t.Errorf("prefix status = %q", got)
	}

	m.focus = focusInspect
	m.prefix = false
	if got := m.plainStatus(); !strings.Contains(got, "scroll") || strings.Contains(got, "leaves bash") {
		t.Errorf("inspect status = %q, want scroll tips without leaves bash", got)
	}

	m = baseModel()
	m.status = "something happened"
	if got := m.plainStatus(); !strings.Contains(got, "something happened") {
		t.Errorf("message status = %q", got)
	}
}

func TestWarmSandboxRefusesFullscreenToo(t *testing.T) {
	m := baseModel() // selected 0 is warm
	newM, cmd := m.updateKeyboard(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if cmd != nil {
		t.Error("F on a warm sandbox returned a command")
	}
	if !strings.Contains(newM.status, "warm pool sandbox") {
		t.Errorf("F on warm = %q", newM.status)
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
	if newM.selected != "agent-pool-bkl2p" {
		t.Errorf("selection after refill = %q, want the same pod by name", newM.selected)
	}
	if got := newM.selectedBox().OwnerKind; got != "SandboxClaim" {
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
	if got := newM.selected; got != "agent-pool-5ss7w" {
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
	if levelColor(0.4) != midGreenColor {
		t.Errorf("levelColor(0.4) = %v, want light green", levelColor(0.4))
	}
	if levelColor(0.1) != warmColor {
		t.Errorf("levelColor(0.1) = %v, want green", levelColor(0.1))
	}
}

// Focus moves between the table and the sidebar's body: the table's
// selection rule follows navigation, and the sidebar rule brightens when the
// shell or configs hold focus.
func TestOneAccentPane(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	l := m.layout()
	dimRule := lipgloss.NewStyle().Foreground(th.PrimaryBorder).Render("│")
	litRule := lipgloss.NewStyle().Foreground(th.SecondaryBorder).Render("│")
	if !strings.Contains(m.sidebarView(l)[0], dimRule) {
		t.Error("navigation: the sidebar rule is not the quiet border")
	}
	m.focus = focusShell
	if !strings.Contains(m.sidebarView(l)[0], litRule) {
		t.Error("shell focus: the sidebar rule did not brighten")
	}
	if head := ansi.Strip(strings.Join(m.infoLines(60), "")); !strings.Contains(head, "C-b q leaves") {
		t.Error("shell focus: the heading does not say how to leave")
	}
}

// While the shell has focus, wheel events do not leak into navigation and
// the splitter drag keeps working, while a table click always returns to
// navigation.
func TestNoInputLeaksFromShell(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.focus = focusShell

	newM, _ := m.updateMouse(tea.MouseWheelMsg{X: 5, Y: 8, Button: tea.MouseWheelDown})
	if newM.selected != "agent-pool-bkl2p" {
		t.Errorf("wheel leaked into navigation: %q", newM.selected)
	}
	x, y := inMain(m)
	if newM, _ = m.updateMouse(press(x, y)); newM.focus != focusShell {
		t.Error("a sidebar click stole shell focus")
	}

	divX := m.layout().sideX
	newM, _ = m.updateMouse(press(divX, 10))
	if !newM.dragging {
		t.Error("the splitter drag must keep working in shell mode")
	}
	newM, _ = newM.updateMouse(drag(80, 10))
	if got := newM.layout().sideX; got != 80 || newM.focus != focusShell {
		t.Errorf("drag in shell mode = %d, focus %v", got, newM.focus)
	}

	newM, _ = m.updateMouse(press(5, rowOf(t, m, "agent-pool-lqbmx")))
	if newM.focus != focusNav || newM.selected != "agent-pool-lqbmx" {
		t.Errorf("table click = focus %v, selected %q; want navigation on lqbmx", newM.focus, newM.selected)
	}
}

// Every row of the frame is exactly the tab's width and the frame exactly
// its height, at any width, so nothing wraps.
func TestFrameFillsItsSize(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120, 160} {
		for _, sel := range []string{"agent-pool-5ss7w", "agent-pool-bkl2p"} {
			m := baseModel()
			m.width, m.height = width, 30
			m.boxes[2].Owner = "dash-claim-1795090340263037564"
			m.selected = sel
			m.sessions["agent-pool-bkl2p"] = true

			rows := strings.Split(m.View(), "\n")
			if len(rows) != m.height {
				t.Errorf("w=%d sel=%s: %d rows, want %d", width, sel, len(rows), m.height)
			}
			for i, r := range rows {
				if got := ansi.StringWidth(r); got != width {
					t.Errorf("w=%d sel=%s: row %d is %d wide", width, sel, i, got)
				}
			}
		}
	}
}

func TestWarmInspectScrollableMarkdownAndYAML(t *testing.T) {

	m := baseModel()
	m.height = 16 // compact viewport so scrolling has room
	p := m.pools["agent-pool"]
	p.TemplateYAML = "apiVersion: extensions.agents.x-k8s.io/v1beta1\nkind: SandboxTemplate\nspec:\n  replicas: 2\n  enabled: true\n  image: debian:12-slim"
	p.ManifestYAML = "apiVersion: extensions.agents.x-k8s.io/v1beta1\nkind: SandboxWarmPool\nmetadata:\n  name: agent-pool"
	m.pools["agent-pool"] = p
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
	entered, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	focusedByEnter := entered
	if cmd != nil || focusedByEnter.focus != focusInspect {
		t.Fatalf("enter focus = %v cmd=%v, want focusInspect and no command", focusedByEnter.focus, cmd != nil)
	}
	selected := focusedByEnter.selected
	entered, _ = focusedByEnter.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if entered.inspectScroll <= focusedByEnter.inspectScroll {
		t.Errorf("j did not scroll config: offset=%d", entered.inspectScroll)
	}
	entered, _ = entered.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if entered.inspectScroll != focusedByEnter.inspectScroll {
		t.Errorf("k returned offset %d, want %d", entered.inspectScroll, focusedByEnter.inspectScroll)
	}
	if entered.selected != selected {
		t.Errorf("j/k changed sandbox selection to %q, want %q", entered.selected, selected)
	}

	// Wheel down over the lower pane scrolls without changing selection.
	mx, my := inMain(m)
	newM, _ := m.updateMouse(tea.MouseWheelMsg{X: mx, Y: my, Button: tea.MouseWheelDown})
	scrolled := newM
	if scrolled.selected != "agent-pool-5ss7w" {
		t.Errorf("wheel over warm viewer changed selection to %q", scrolled.selected)
	}
	if scrolled.inspectScroll <= 0 {
		t.Errorf("wheel down did not scroll warm viewer: offset=%d", scrolled.inspectScroll)
	}

	// Clicking the warm lower pane focuses inspect (not bash) and j/k scroll.
	newM, _ = scrolled.updateMouse(press(inMain(scrolled)))
	focused := newM
	if focused.focus != focusInspect {
		t.Fatalf("click on warm viewer focus = %v, want focusInspect", focused.focus)
	}
	before := focused.inspectScroll
	newM, _ = focused.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if newM.inspectScroll != before+1 {
		t.Errorf("j in focusInspect offset = %d, want %d", newM.inspectScroll, before+1)
	}
	newM, _ = newM.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if newM.focus != focusNav {
		t.Error("esc in focusInspect did not return to focusNav")
	}

	// Moving to another sandbox resets scroll back to top.
	newM, _ = newM.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if newM.inspectScroll != 0 {
		t.Errorf("changing selection left inspectScroll = %d, want 0", newM.inspectScroll)
	}
}

// Transient status messages expire on their own: a destroyed-claim
// notification disappears after its TTL instead of sitting on the
// status line forever. Sticky messages, errors and armed
// confirmations, stay until replaced.
func TestStatusExpiry(t *testing.T) {
	m := baseModel()

	newM, _ := m.Update(deletedMsg{claim: "dash-claim-1"})
	if newM.statusTTL <= 0 {
		t.Error("destroy notification is not transient")
	}
	// Three fleet refreshes age it out (5s ttl, 2s tick).
	aged, _ := newM.Update(tickMsg{})
	aged, _ = aged.Update(tickMsg{})
	aged, _ = aged.Update(tickMsg{})
	if aged.status != "" {
		t.Errorf("transient status = %q, want expired", aged.status)
	}

	// Errors stay.
	newM, _ = m.Update(claimMsg{err: errFixture})
	if newM.statusTTL != 0 {
		t.Error("an error message must be sticky")
	}

	// The armed destroy confirmation stays until answered.
	armed := baseModel()
	armed.selected = "agent-pool-bkl2p"
	newM, _ = armed.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if newM.statusTTL != 0 {
		t.Error("the armed x confirmation must be sticky")
	}
}

// While bash holds focus the host must leave every key to it, tab included.
func TestShellFocusCapturesTab(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	if m.Capturing() {
		t.Fatal("navigation must not capture the keyboard")
	}
	m.focus = focusShell
	if !m.Capturing() {
		t.Fatal("the shell did not capture the keyboard")
	}
	got, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if cmd == nil || got.focus != focusShell {
		t.Error("tab in the shell was not sent to bash")
	}
}

// A hidden tab stops reading Herdr's screen but keeps its tick alive.
func TestHiddenTabSkipsShellCapture(t *testing.T) {
	m := baseModel()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	got, cmd := m.SetHidden(true).Update(shellTickMsg{})
	if cmd == nil {
		t.Fatal("the hidden tab dropped its shell tick")
	}
	if got.shellW != 0 {
		t.Error("the hidden tab still captured the shell")
	}
	got.watchingShell = true // a watch started before switching tabs
	got, cmd = got.Update(shellMsg{pod: got.selected, lines: []string{"hidden"}})
	if cmd != nil || got.watchingShell || got.shellLines != nil {
		t.Fatal("a hidden update applied a screen or restarted the watcher")
	}
}

// Destroying the last claim while the Claimed section shows empties the
// section; the reload must not index into it.
func TestReloadIntoEmptySection(t *testing.T) {
	m := baseModel()
	m.setSection(sectionClaimed)
	var warm []cluster.Sandbox
	for _, b := range m.boxes {
		if b.OwnerKind != "SandboxClaim" {
			warm = append(warm, b)
		}
	}
	m.selected = "gone"
	next, _ := m.Update(loadedMsg{boxes: warm, metricsOK: true})
	got := next
	if len(got.navigationIndices()) != 0 {
		t.Fatalf("claimed section should be empty")
	}
	_ = got.View()
}

// ? opens the full key list under the footer and the frame keeps its size.
func TestHelpToggles(t *testing.T) {
	m := baseModel()
	next, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = next
	if !m.showHelp {
		t.Fatal("? did not open help")
	}
	view := m.View()
	plain := ansi.Strip(view)
	for _, want := range []string{"first/last", "destroy claim", "leave bash", "resize sidebar"} {
		if !strings.Contains(plain, want) {
			t.Errorf("help missing %q", want)
		}
	}
	if rows := strings.Split(view, "\n"); len(rows) != m.height {
		t.Errorf("frame is %d rows with help, want %d", len(rows), m.height)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if next.showHelp {
		t.Fatal("? did not close help")
	}
}

// A claimed sandbox's own counters override metrics-server: memory from the
// first sample, CPU as the rate between two, even with metrics-server down.
func TestLiveUsageFromCgroups(t *testing.T) {
	m := baseModel()
	m.metricsOK = false
	pod := "agent-pool-bkl2p"
	load := func(up float64, usec, mem int64) {
		boxes := testBoxes()
		next, _ := m.Update(loadedMsg{boxes: boxes, sessions: m.sessions,
			cgroups: map[string]cluster.CgroupSample{pod: {Uptime: up, CPUUsec: usec, MemBytes: mem}}})
		m = next
	}
	box := func() cluster.Sandbox {
		for _, b := range m.boxes {
			if b.Name == pod {
				return b
			}
		}
		t.Fatal("pod missing")
		return cluster.Sandbox{}
	}
	load(100, 1_000_000, 40<<20)
	if b := box(); b.MemUsageMiB != 40 || m.usageKnown(pod) {
		t.Fatalf("first sample: mem %d, known %v", b.MemUsageMiB, m.usageKnown(pod))
	}
	load(102, 1_800_000, 41<<20) // 0.8 s CPU over 2 s = 400m
	if b := box(); b.CPUUsageMC != 400 || b.MemUsageMiB != 41 || !m.usageKnown(pod) {
		t.Fatalf("second sample: cpu %d mem %d known %v", b.CPUUsageMC, b.MemUsageMiB, m.usageKnown(pod))
	}
	if h := m.hist[pod]; len(h.cpu) == 0 || h.cpu[len(h.cpu)-1] != 400.0/500 {
		t.Fatalf("history did not record the live rate: %v", h.cpu)
	}
}
