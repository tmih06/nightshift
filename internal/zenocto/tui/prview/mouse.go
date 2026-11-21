package prview

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/zenocto/tui/comp"
)

const (
	mouseWheelLines = 3
	minMainWidth    = 30
)

func (m Model) handleMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.merging.open || m.reviewing.open || m.picking.open() || m.check.searching {
		return m, nil
	}

	mouse := msg.Mouse()
	x, y := mouse.X, mouse.Y

	switch msg.(type) {
	case tea.MouseReleaseMsg:
		m.dragging = false
		return m, nil

	case tea.MouseMotionMsg:
		if m.dragging && mouse.Button == tea.MouseLeft {
			m.applyDividerDrag(x)
		}
		return m, nil

	case tea.MouseWheelMsg:
		delta := 0
		switch mouse.Button {
		case tea.MouseWheelUp:
			delta = -1
		case tea.MouseWheelDown:
			delta = 1
		default:
			return m, nil
		}
		return m.applyMouseWheel(x, y, delta)

	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		if m.overDivider(x, y) {
			m.dragging = true
			return m, nil
		}
		if m.Composing() {
			return m, nil
		}
		return m.applyMouseClick(x, y)
	}

	return m, nil
}

func (m Model) dividerX() int {
	switch {
	case m.sideVisible():
		return m.sideColumn()
	case m.railVisible() && m.railColumn():
		return m.railWidth()
	}
	return 0
}

func (m Model) overDivider(x, y int) bool {
	divX := m.dividerX()
	if divX <= 0 {
		return false
	}
	lead := m.headLead()
	return y >= lead && y < m.height && x >= divX-1 && x <= divX+1
}

func (m *Model) applyDividerDrag(x int) {
	w := min(max(x, sideMin), max(sideMin, m.width-minMainWidth))
	switch {
	case m.sideVisible():
		m.sideWidthOverride = w
	case m.railVisible() && m.railColumn():
		m.railWidthOverride = w
	default:
		return
	}
	m.layout()
}

func (m Model) paneAt(x, y int) pane {
	lead := m.headLead()
	if y < lead || y >= m.height || x < 0 || x >= m.width {
		return paneNone
	}
	if m.railVisible() {
		railW := m.railWidth()
		if !m.railColumn() {
			railW = columnWidth
		}
		if x < railW {
			return paneRail
		}
		return paneMain
	}
	if m.sideVisible() && x < m.sideColumn() {
		return paneSide
	}
	return paneMain
}

func (m Model) applyMouseWheel(x, y, delta int) (Model, tea.Cmd) {
	target := m.paneAt(x, y)
	switch target {
	case paneRail:
		if delta < 0 {
			m.railView.ScrollUp(mouseWheelLines)
		} else {
			m.railView.ScrollDown(mouseWheelLines)
		}
		return m, nil

	case paneSide:
		if m.Composing() {
			return m, nil
		}
		m.moveSide(delta)
		return m, tea.Batch(m.armCommit(), m.armJob(), m.armJobRender())

	case paneMain:
		if delta < 0 {
			m.view.ScrollUp(mouseWheelLines)
		} else {
			m.view.ScrollDown(mouseWheelLines)
		}
		return m, nil
	}
	return m, nil
}

func (m Model) applyMouseClick(x, y int) (Model, tea.Cmd) {
	lead := m.headLead()

	// Tab strip sits on row 3 of frameHead when the full header fits.
	if lead >= 4 && y == 3 {
		if idx, ok := m.tabAt(x); ok {
			if idx != m.tab {
				return m, m.goToTab(idx)
			}
			return m, nil
		}
		return m, nil
	}

	target := m.paneAt(x, y)
	switch target {
	case paneRail:
		m.focusPane(paneRail)
		return m, nil

	case paneMain:
		m.focusPane(paneMain)
		return m, nil

	case paneSide:
		m.focusPane(paneSide)
		return m.clickSideRow(y - lead - m.side.Above())
	}

	return m, nil
}

func (m Model) clickSideRow(bodyY int) (Model, tea.Cmd) {
	if bodyY < 0 || bodyY >= m.sideView.Height() {
		return m, nil
	}
	line := m.sideView.YOffset() + bodyY
	switch m.tab {
	case tabFiles:
		if line < 0 || line >= len(m.rows) {
			return m, nil
		}
		if m.rows[line].dir {
			m.cursor = line
			m.toggleFold()
			return m, nil
		}
		m.moveCursor(line - m.cursor)
		return m, nil

	case tabCommits:
		idx := line / commitRowHeight
		if idx < 0 || idx >= len(m.detail.Detail.Commits) {
			return m, nil
		}
		m.moveCommit(idx - m.commit.cursor)
		return m, m.armCommit()

	case tabChecks:
		if line < 0 || line >= len(m.check.rows) {
			return m, nil
		}
		if m.check.rows[line].parent {
			m.check.cursor = line
			m.toggleCheckFold()
			return m, nil
		}
		m.moveCheck(line - m.check.cursor)
		return m, tea.Batch(m.armJob(), m.armJobRender())
	}
	return m, nil
}

// tabAt maps a column x on the tab row to the tab drawn there.
func (m Model) tabAt(x int) (int, bool) {
	inner := m.headWidth()
	list := m.tabCounts()
	if lipgloss.Width(m.renderTabs(list)) > inner {
		list = tabs
	}
	return tabHit(list, x, headGutter, headGutter+inner)
}

func tabHit(list []comp.Tab, x, start, limit int) (int, bool) {
	if x < start || x >= limit {
		return 0, false
	}
	const sepWidth = 3 // " | "
	pos := start
	for i, tab := range list {
		w := 1 + lipgloss.Width(tab.Label) + 1
		if tab.Badge != "" {
			w += 1 + lipgloss.Width(tab.Badge)
		}
		end := min(pos+w, limit)
		if x >= pos && x < end {
			return i, true
		}
		pos += w + sepWidth
		if pos >= limit {
			break
		}
	}
	return 0, false
}
