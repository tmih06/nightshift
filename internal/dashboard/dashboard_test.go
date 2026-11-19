package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

func testBoxes() []cluster.Sandbox {
	created := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	return []cluster.Sandbox{
		{Name: "agent-pool-5ss7w", OwnerKind: "SandboxWarmPool", Owner: "agent-pool",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256, CPUUsageMC: 2, MemUsageMiB: 6},
		{Name: "agent-pool-lqbmx", OwnerKind: "SandboxWarmPool", Owner: "agent-pool",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256},
		{Name: "agent-pool-bkl2p", OwnerKind: "SandboxClaim", Owner: "my-agent",
			Phase: "Running", Created: created, CPULimitMC: 500, MemLimitMiB: 256},
	}
}

func baseModel() model {
	return model{boxes: testBoxes(), sessions: map[string]bool{}, metricsOK: true,
		loaded: true, width: 100, height: 30}
}

// The left pane is the contract: warm and claimed sandboxes live in
// separate sections, warm first, and every entry line maps back to its
// sandbox for the mouse.
func TestLeftLinesSections(t *testing.T) {
	m := baseModel()
	lines, targets := m.leftLines()

	if !strings.Contains(lines[0], "WARM POOL (2)") {
		t.Errorf("first line = %q, want the warm pool section header", lines[0])
	}
	warm0 := targets[1]
	if warm0 != 0 || !strings.Contains(lines[1], "agent-pool-5ss7w") {
		t.Errorf("first entry row = %q, targets %v", lines[1], targets)
	}
	claimedHeaderIdx := 4 // header, entry, entry, blank, header
	if !strings.Contains(lines[claimedHeaderIdx], "CLAIMED (1)") {
		t.Errorf("line %d = %q, want the claimed section header", claimedHeaderIdx, lines[claimedHeaderIdx])
	}
	if got := targets[claimedHeaderIdx+1]; got != 2 || !strings.Contains(lines[claimedHeaderIdx+1], "agent-pool-bkl2p") {
		t.Errorf("claimed entry = %q, targets %v", lines[claimedHeaderIdx+1], targets)
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

func TestMouseClickSelectsEntry(t *testing.T) {
	m := baseModel()

	// Click the second warm entry: row 1 is the first entry, row 2 the second.
	m = m.clickLeftPane(5, contentStartY+2)
	if m.selected != 1 {
		t.Errorf("selected = %d after clicking the second entry, want 1", m.selected)
	}

	// A click in the right pane selects nothing.
	m = m.clickLeftPane(90, contentStartY+2)
	if m.selected != 1 {
		t.Errorf("right-pane click moved the selection to %d, want no change", m.selected)
	}

	// Below the left pane's content changes nothing.
	m = m.clickLeftPane(5, contentStartY+20)
	if m.selected != 1 {
		t.Errorf("empty-row click moved the selection to %d, want no change", m.selected)
	}
}

func TestMouseWheelMoves(t *testing.T) {
	m := baseModel()
	newM, _ := m.updateMouse(tea.MouseMsg{X: 5, Y: 5,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if newM.(model).selected != 1 {
		t.Errorf("wheel down moved selection to %d, want 1", newM.(model).selected)
	}
	newM, _ = newM.(model).updateMouse(tea.MouseMsg{X: 5, Y: 5,
		Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if newM.(model).selected != 0 {
		t.Errorf("wheel up moved selection to %d, want 0", newM.(model).selected)
	}
}

func TestStackedClickUsesY(t *testing.T) {
	m := baseModel()
	m.width = 40 // below stackedBelow
	// The left pane is the top half; clicking its rows selects entries.
	m = m.clickLeftPane(5, contentStartY+2)
	if m.selected != 1 {
		t.Errorf("stacked click selected %d, want 1", m.selected)
	}
}

// The usage line is exactly innerW wide, so golden rule 2 holds: no
// auto-wrap inside the bordered pane.
func TestResourceLineWidth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		known      bool
		usage, lim int64
		unit       string
	}{
		{"known", true, 2, 500, "m"},
		{"unknown", false, 0, 256, "Mi"},
	} {
		got := lipgloss.Width(resourceLine("CPU", tc.known, tc.usage, tc.lim, tc.unit, 60))
		if got != 60 {
			t.Errorf("%s: width = %d, want 60", tc.name, got)
		}
	}
	if got := resourceLine("CPU", true, 4, 0, "m", 60); !strings.Contains(got, "no limit") {
		t.Errorf("no-limit line = %q", got)
	}
}

func TestKeyboardNavigationAndActions(t *testing.T) {
	m := baseModel()

	newM, _ := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyDown})
	if newM.(model).selected != 1 {
		t.Errorf("j moved selection to %d, want 1", newM.(model).selected)
	}

	// Destroy on a warm sandbox is refused.
	newM, _ = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if newM.(model).status != "only a claimed sandbox can be destroyed" {
		t.Errorf("x on warm = %q", newM.(model).status)
	}

	// Enter on a pod that is not running is refused with a reason.
	claim := m
	claim.boxes[2].Phase = "Pending"
	claim.selected = 2
	newM, _ = claim.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if newM.(model).status != "pod is Pending, no bash to enter" {
		t.Errorf("enter on pending = %q", newM.(model).status)
	}

	// Enter on a running pod returns the attach command, which the
	// caller executes (not here: it would start a real tmux session).
	running := m
	running.selected = 0
	_, cmd := running.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("enter on a running pod returned no command")
	}

	// q quits.
	_, cmd = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("q returned no quit command")
	}
}

// Destroy needs two presses on a claimed sandbox, and any other key
// disarms it.
func TestDestroyConfirmation(t *testing.T) {
	m := baseModel()
	m.selected = 2 // the claimed sandbox, owner my-agent

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

	// A loaded batch clamps the selection to the fleet size.
	short := m
	short.selected = 9
	newM, _ := short.Update(loadedMsg{boxes: m.boxes[:1], sessions: map[string]bool{}})
	if newM.(model).selected != 0 {
		t.Errorf("selection after shrink = %d, want 0", newM.(model).selected)
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
		"agent-pool-5ss7w",
		"agent-pool-lqbmx",
		"agent-pool-bkl2p",
		"▶",
		"· warm ·",
		"owner agent-pool",
		"Running",
		"tmux",
		"2m/500m",
		"6Mi/256Mi",
		"live",
		"[enter] bash",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q", want)
		}
	}

	// Both pane bottom borders close on the same row: the panes are the
	// same height, so nothing wrapped inside them.
	rows := strings.Split(got, "\n")
	var closing string
	for _, r := range rows {
		if strings.Contains(r, "╰") {
			closing = r
		}
	}
	if strings.Count(closing, "╰") != 2 {
		t.Errorf("closing row = %q, want both panes to close on it", closing)
	}

	// The inspect pane follows the selection: the claimed sandbox shows
	// its own owner, and the untracked warm pod shows no session.
	m.selected = 2
	g := m.View()
	for _, want := range []string{"· claimed ·", "my-agent", "tmux off", "[x] destroy claim", "0m/500m"} {
		if !strings.Contains(g, want) {
			t.Errorf("claimed inspect missing %q", want)
		}
	}
	m.selected = 1
	if g := m.View(); !strings.Contains(g, "tmux off") {
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
	if m.selected != 0 {
		t.Errorf("selected = %d above the top, want 0", m.selected)
	}
	m.moveSelection(99)
	if m.selected != len(m.boxes)-1 {
		t.Errorf("selected = %d below the bottom, want %d", m.selected, len(m.boxes)-1)
	}
	empty := model{}
	empty.moveSelection(3)
	if empty.selected != 0 {
		t.Errorf("empty fleet selection = %d, want 0", empty.selected)
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
	if l.leftW != 33 || l.headerH != 7 || l.shellH != 21 || l.stacked {
		t.Errorf("default layout = %+v, want leftW 33, header 7, shell 21", l)
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
	m := baseModel() // divider sits at x=33

	newM, _ := m.updateMouse(press(33, 10))
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
	clamped, _ := m.updateMouse(press(33, 10))
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
	m := baseModel() // shellTop = 2 + headerH(14) = 16

	newM, _ := m.updateMouse(press(70, 20))
	if !newM.(model).shellFocus {
		t.Error("click in the shell pane did not focus it")
	}

	newM, _ = m.updateMouse(press(5, 4))
	if newM.(model).shellFocus {
		t.Error("click on a left-pane entry must return to navigation")
	}
}

func TestShellKeyRouting(t *testing.T) {
	m := baseModel()
	m.selected = 0
	m.sessions["agent-pool-5ss7w"] = true
	m.shellFocus = true

	// Esc is the dashboard's own key: it leaves the shell.
	newM, _ := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if newM.(model).shellFocus {
		t.Error("esc did not leave the shell focus")
	}

	// Letters, including q, go to the session instead of quitting.
	_, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("a rune in shell focus produced no send command")
	}

	// Ctrl+c interrupts the bash process instead of quitting the app.
	_, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("ctrl+c in shell focus produced no send command")
	}

	// Unmapped keys are dropped, not sent garbage.
	_, cmd = m.updateShellKeys(tea.KeyMsg{Type: tea.KeyF5})
	if cmd != nil {
		t.Error("unmapped key produced a send command")
	}

	// Without a live session the focus falls back to navigation.
	bare := baseModel()
	bare.shellFocus = true
	newM, _ = bare.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if newM.(model).shellFocus {
		t.Error("keys with no session must not stay in the shell")
	}
}

func TestTmuxKeyName(t *testing.T) {
	cases := map[string]string{
		"enter": "Enter", "up": "Up", "backspace": "BSpace",
		"ctrl+c": "C-c", "ctrl+d": "C-d", "tab": "Tab",
	}
	for in, want := range cases {
		got, ok := tmuxKeyName(in)
		if !ok || got != want {
			t.Errorf("tmuxKeyName(%q) = %q, %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := tmuxKeyName("f5"); ok {
		t.Error("tmuxKeyName mapped an unknown key")
	}
}

func TestShellCaptureDisplay(t *testing.T) {
	m := baseModel()
	m.selected = 0
	m.sessions["agent-pool-5ss7w"] = true

	newM, _ := m.Update(shellMsg{pod: "agent-pool-5ss7w", lines: []string{"$ echo hi", "hi"}})
	if got := newM.(model).shellLines; len(got) != 2 {
		t.Fatalf("shellLines = %v, want the captured two lines", got)
	}
	if g := newM.(model).View(); !strings.Contains(g, "$ echo hi") {
		t.Errorf("view missing the captured shell content")
	}

	// A capture for another pod is not shown.
	newM, _ = newM.(model).Update(shellMsg{pod: "agent-pool-lqbmx", lines: []string{"other"}})
	if got := newM.(model).View(); strings.Contains(got, "other") {
		t.Errorf("view showed a capture from a pod that is not selected")
	}

	// A failed capture clears the stale content.
	newM, _ = newM.(model).Update(shellMsg{pod: "agent-pool-5ss7w", err: errFixture})
	if got := newM.(model).shellLines; got != nil {
		t.Errorf("failed capture left stale lines: %v", got)
	}
}

func TestShellTick(t *testing.T) {
	m := baseModel()
	m.selected = 0
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
	m.selected = 0
	m.sessions["agent-pool-5ss7w"] = true
	m.shellFocus = true

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
	newM, cmd = newM.(model).Update(sentMsg{pod: "agent-pool-5ss7w", count: 3})
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
	newM, cmd = failed.Update(sentMsg{pod: "agent-pool-5ss7w", count: 1, err: errFixture})
	if cmd != nil {
		t.Error("failed drain started another drain")
	}
	if len(newM.(model).pending) != 0 || newM.(model).sending {
		t.Errorf("after a failed drain: pending %v, sending %v", newM.(model).pending, newM.(model).sending)
	}
}

// The shell tick and the post-send capture both size the tmux window to
// the pane, so the embedded output wraps where it is shown.
func TestShellResizeFollowsPaneSize(t *testing.T) {
	m := baseModel()
	m.selected = 0
	m.sessions["agent-pool-5ss7w"] = true
	m.shellFocus = true

	_, cmd := m.updateShellKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("first key produced no drain command")
	}
	newM, _ := m.Update(sentMsg{pod: "agent-pool-5ss7w", count: 1})
	w, h := newM.(model).shellW, newM.(model).shellH
	wantW, wantH := m.shellPaneSize()
	if w != wantW || h != wantH {
		t.Errorf("shell pane size = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}

func TestAttachFocusesShellInsteadOfFullscreen(t *testing.T) {
	m := baseModel()
	m.selected = 0
	m.sessions["agent-pool-5ss7w"] = true

	// enter issues the ensure command, not a fullscreen exec.
	_, cmd := m.updateKeyboard(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}

	newM, _ := m.Update(attachReadyMsg{pod: "agent-pool-5ss7w"})
	if !newM.(model).shellFocus {
		t.Error("a ready session did not focus the embedded shell")
	}

	// F still reaches the fullscreen path.
	_, cmd = m.updateKeyboard(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if cmd == nil {
		t.Error("F returned no fullscreen command")
	}
}

var errFixture = errors.New("cluster unreachable")
