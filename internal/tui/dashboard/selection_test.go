package dashboard

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func shellModel(width int) model {
	m := baseModel()
	m.width = width
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.shellPod = "agent-pool-bkl2p"
	m.shellLines = []string{"$ echo \x1b[32mhello\x1b[0m world   ", "second line", "third"}
	return m
}

// cellOf finds where text is drawn, so the test shares no arithmetic with the
// code under test.
func cellOf(t *testing.T, m model, text string) (x, y int) {
	t.Helper()
	for y, row := range strings.Split(ansi.Strip(m.View()), "\n") {
		if before, _, ok := strings.Cut(row, text); ok {
			return ansi.StringWidth(before), y
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(m.View()))
	return 0, 0
}

// A drag over the shell selects only the shell's text and copies it.
func TestDragInShellCopiesItsText(t *testing.T) {
	wide, stacked, zoomed := shellModel(140), shellModel(80), shellModel(140)
	stacked.height = 40
	zoomed.zoom, zoomed.focus = true, focusShell
	for name, m := range map[string]model{"wide": wide, "stacked": stacked, "zoomed": zoomed} {
		x, y := cellOf(t, m, "hello")
		endX, endY := cellOf(t, m, "second")
		m, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m, _ = m.Update(tea.MouseMotionMsg{X: endX + 5, Y: endY, Button: tea.MouseLeft})
		// Output arriving mid drag must not move the text from under it.
		m.shellLines = []string{"scrolled away"}
		if view := m.View(); !strings.Contains(view, "\x1b[7mhello world") {
			t.Errorf("%s: drag is not highlighted:\n%q", name, view)
		}
		m, cmd := m.Update(tea.MouseReleaseMsg{X: endX + 5, Y: endY, Button: tea.MouseLeft})
		if cmd == nil {
			t.Errorf("%s: release copied nothing", name)
			continue
		}
		if got, want := fmt.Sprint(cmd()), "hello world\nsecond"; got != want {
			t.Errorf("%s: copied %q, want %q", name, got, want)
		}
		if m.focus != focusShell || len(m.pending) != 0 {
			t.Errorf("%s: focus %v, pending %v, want the shell and nothing typed", name, m.focus, m.pending)
		}
		if strings.Contains(m.View(), "\x1b[7m") {
			t.Errorf("%s: highlight stayed over a screen that changed", name)
		}
	}
}

// A click that does not move only focuses the shell.
func TestClickInShellCopiesNothing(t *testing.T) {
	m := shellModel(140)
	x, y := cellOf(t, m, "hello")
	m, _ = m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m, cmd := m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if cmd != nil || m.selection != nil || m.focus != focusShell {
		t.Errorf("click: cmd %v, selection %v, focus %v", cmd != nil, m.selection, m.focus)
	}
}
