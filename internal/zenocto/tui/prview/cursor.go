package prview

import (
	"strings"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
)

// The unlit rows stay in the viewport. Paint the cursor and range only on
// screen, so moving inside a run never resets or measures the whole diff.
type cursorRun struct {
	key                  focusKey
	run                  run
	start, count, gutter int
	column               gh.DiffSide
}

func (m Model) paintDiffCursor(view string) string {
	c := m.diff.cursorRun
	if !m.lit(c.key) || !m.walkedInto(c.key) {
		return view
	}
	lit := c.run.rowAt(min(m.diffCursor, c.count), c.column)
	if lit < 0 {
		return view
	}
	from, to := m.litSpan(c.run, c.key, c.count, c.column)
	offset := contentLead + c.start - m.view.YOffset()
	rows := strings.Split(view, "\n")
	width := m.bodyWidth()
	for at := max(0, -offset); at < min(len(c.run.rows), len(rows)-offset); at++ {
		if at == lit || from <= at && at <= to {
			rows[offset+at] = m.litRow(c.run.rows[at], at == lit, c.gutter, width, c.column)
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) diffDriving() bool {
	return m.tab == tabFiles && m.focus == paneMain && m.files.Loaded && m.pageRing.stops() > 0
}

func (m Model) diffRows(key focusKey) int { return m.diff.rows[key] }

func (m Model) walkedColumn() gh.DiffSide { return m.diff.columns[m.pageRing.on] }

func (m Model) cursorColumn(split bool) gh.DiffSide {
	if !split {
		return ""
	}
	return m.column
}

func (m Model) diffAt() int {
	if m.tab != tabFiles || m.pageRing.on != m.diffOn {
		return 0
	}
	return min(m.diffCursor, m.diffRows(m.pageRing.on))
}

// A render asks this rather than diffAt, whose row counts that render is still measuring.
func (m Model) walkedInto(key focusKey) bool {
	return m.tab == tabFiles && m.diffOn == key && m.diffCursor > 0
}

func (m *Model) point(rows int) {
	if m.pageRing.on != m.selection.on {
		m.selection = span{}
	}
	m.diffCursor, m.diffOn = rows, m.pageRing.on
}

func (m *Model) unpoint() { m.diffOn, m.selection = focusKey{}, span{} }

func (m *Model) moveDiffCursor(delta int) bool {
	if !m.diffDriving() {
		return false
	}

	if !m.cursorShown() {
		m.unpoint()
		return m.stepFocus(delta)
	}

	on, at := m.pageRing.on, m.diffAt()
	switch {
	case delta > 0 && at < m.diffRows(m.pageRing.on):
		m.point(at + 1)
	case delta > 0:
		if !m.advanceFocus(1) {
			return false
		}
		m.unpoint()
	case at > 0:
		m.point(at - 1)
	default:
		if !m.advanceFocus(-1) {
			return false
		}
		m.point(m.diffRows(m.pageRing.on))
	}

	c := m.diff.cursorRun
	if at > 0 && m.diffCursor > 0 && on == m.pageRing.on && c.key == on && m.walkedInto(on) {
		m.diff.cursorLine = c.start + c.run.rowAt(min(m.diffCursor, c.count), c.column)
	} else {
		m.syncContent()
	}
	m.showDiffCursor()
	return true
}

func (m *Model) advanceFocus(delta int) bool {
	top := bodyTop(&m.view)
	if !m.pageRing.advance(delta) {
		return false
	}

	m.syncContent()
	m.showFocus(&m.pageRing, &m.view, top)
	return true
}

func (m Model) cursorShown() bool {
	if m.diff.cursorLine < 0 {
		return false
	}

	line, top := contentLead+m.diff.cursorLine, m.view.YOffset()
	return line >= top && line < top+max(1, m.view.Height())
}

// Shortest scroll on purpose: a one-row step is not a journey, unlike a ring step.
func (m *Model) showDiffCursor() {
	if m.diff.cursorLine < 0 {
		return
	}

	line := contentLead + m.diff.cursorLine
	top, height := m.view.YOffset(), max(1, m.view.Height())
	switch {
	case line < top:
		m.view.SetYOffset(line)
	case line >= top+height:
		m.view.SetYOffset(line - height + 1)
	}
}
