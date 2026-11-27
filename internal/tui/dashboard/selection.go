package dashboard

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The TUI captures the mouse, so the host terminal cannot select the shell's
// text, and its own shift-drag takes whole rows of the TUI. A drag in the
// shell selects there instead and copies on release.

// shellIndent is the blank margin before the sidebar's body.
const shellIndent = 2

// cellPos is a cell of the shell's screen as drawn.
type cellPos struct{ row, col int }

// shellSelection is a mouse selection over one terminal's screen. lines is
// the screen as it was pressed on, which stays up while the button is held, so
// output cannot move the text from under the drag.
type shellSelection struct {
	pod, term    string
	lines        []string
	anchor, head cellPos
	dragging     bool
	moved        bool
}

// onScreen is whether the shell lines held are the selected pod and window's.
func (m model) onScreen() bool {
	b := m.selectedBox()
	return b != nil && m.shellPod == b.Name && (m.shellTerm == "" || m.shellTerm == m.activeTerm(b.Name))
}

// span is the selection's first and last cell in reading order.
func (s *shellSelection) span() (first, last cellPos) {
	first, last = s.anchor, s.head
	if last.row < first.row || (last.row == first.row && last.col < first.col) {
		first, last = last, first
	}
	return first, last
}

// cols is the half open range of columns selected on row.
func (s *shellSelection) cols(row, width int) (from, to int, ok bool) {
	a, b := s.span()
	if !s.moved || row < a.row || row > b.row {
		return 0, 0, false
	}
	from, to = 0, width
	if row == a.row {
		from = a.col
	}
	if row == b.row {
		to = min(b.col+1, width)
	}
	return from, to, from < to
}

// text is what the selection copies: plain, without the screen's padding.
func (s *shellSelection) text() string {
	a, b := s.span()
	var out strings.Builder
	for row := a.row; row <= b.row; row++ {
		if row > a.row {
			out.WriteByte('\n')
		}
		if row >= len(s.lines) {
			continue
		}
		from, to, ok := s.cols(row, ansi.StringWidth(s.lines[row]))
		if ok {
			out.WriteString(strings.TrimRight(ansi.Strip(ansi.Cut(s.lines[row], from, to)), " "))
		}
	}
	return out.String()
}

// highlight draws line, the screen's row, with its selected cells reversed.
func (s *shellSelection) highlight(line string, row int) string {
	width := ansi.StringWidth(line)
	from, to, ok := s.cols(row, width)
	if !ok {
		return line
	}
	return ansi.Cut(line, 0, from) + "\x1b[0m\x1b[7m" + ansi.Strip(ansi.Cut(line, from, to)) +
		"\x1b[0m" + ansi.Cut(line, to, width)
}

// shellScreen is the live screen's rows that fit the pane, cut to its width.
func (m model) shellScreen(l layout) []string {
	content := m.shellLines
	if len(content) > l.docH {
		content = content[len(content)-l.docH:]
	}
	lines := make([]string, 0, len(content))
	for _, ln := range content {
		lines = append(lines, truncateANSI(ln, l.docW))
	}
	return lines
}

// shownSelection is the selection to draw over screen: the one being dragged,
// or a finished one whose text is still where it was.
func (m model) shownSelection(screen []string) *shellSelection {
	s := m.selection
	if s == nil || s.pod != m.shellPod || s.term != m.shellTerm {
		return nil
	}
	if !s.dragging && !slices.Equal(s.lines, screen) {
		return nil
	}
	return s
}

// shellOrigin is the window cell of the shell's top left corner.
func shellOrigin(l layout) (x, y int) {
	if l.stacked {
		return shellIndent, l.docTop
	}
	return l.sideX + 1 + shellIndent, l.docTop // past the sidebar's rule
}

// pressShell starts a selection when (x, y) is on the shell's screen.
func (m model) pressShell(x, y int) (model, bool) {
	if !m.canFocusShell() || !m.onScreen() {
		return m, false
	}
	l := m.layout()
	left, top := shellOrigin(l)
	screen := m.shellScreen(l)
	at := cellPos{row: y - top, col: x - left}
	if at.row < 0 || at.row >= len(screen) || at.col < 0 || at.col >= l.docW {
		return m, false
	}
	m.focus = focusShell
	m.selection = &shellSelection{pod: m.shellPod, term: m.shellTerm, lines: screen, anchor: at, head: at, dragging: true}
	return m, true
}

// dragShell moves the selection's end to (x, y), kept on the screen.
func (m model) dragShell(x, y int) model {
	l := m.layout()
	left, top := shellOrigin(l)
	s := *m.selection
	at := cellPos{row: y - top, col: clamp(x-left, 0, l.docW-1)}
	if at.row < 0 {
		at = cellPos{}
	}
	if at.row >= len(s.lines) {
		at = cellPos{row: len(s.lines) - 1, col: l.docW - 1}
	}
	s.head = at
	s.moved = s.moved || at != s.anchor
	m.selection = &s
	return m
}

// releaseShell ends the drag and copies what it covered. The copy goes out as
// OSC 52, which reaches the clipboard of the terminal even over ssh.
func (m model) releaseShell() (model, tea.Cmd) {
	s := *m.selection
	s.dragging = false
	if !s.moved {
		m.selection = nil
		return m, nil
	}
	m.selection = &s
	text := s.text()
	if text == "" {
		return m, nil
	}
	m.flash(fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)))
	return m, tea.SetClipboard(text)
}
