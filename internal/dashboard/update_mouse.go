package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Row 0 is the title line and row 1 the left pane's top border, so the
// left pane's content starts at row 2 in both layouts.
const contentStartY = 2

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveSelection(-1)
		case tea.MouseButtonWheelDown:
			m.moveSelection(1)
		case tea.MouseButtonLeft:
			if m.overDivider(msg.X, msg.Y) {
				m.dragging = true
			} else {
				m = m.applyClick(msg.X, msg.Y)
			}
		}
	case tea.MouseActionMotion:
		if m.dragging && msg.Button == tea.MouseButtonLeft {
			m = m.applyDrag(msg.X, msg.Y)
		}
	case tea.MouseActionRelease:
		m.dragging = false
	}
	return m, nil
}

// overDivider reports whether a press grabs the splitter between the
// panes: the left pane's right border in the side-by-side layout, its
// bottom border when stacked.
func (m model) overDivider(x, y int) bool {
	l := m.layout()
	if l.stacked {
		d := contentStartY + l.leftH - 1
		return y >= d-1 && y <= d+1
	}
	return x >= l.leftW-2 && x <= l.leftW+1
}

// applyDrag stores the splitter position under the pointer. layout
// clamps it, so the navigation pane can never swallow the right column
// or shrink to nothing.
func (m model) applyDrag(x, y int) model {
	if m.width < stackedBelow {
		m.leftHeight = y - contentStartY + 1
		return m
	}
	m.leftWidth = x
	return m
}

// applyClick routes focus by region: clicking the shell pane focuses
// it. Clicks elsewhere select entries but never steal the shell focus,
// matching the no-escaping rule: only C-b q leaves the shell.
func (m model) applyClick(x, y int) model {
	if m.inShellPane(x, y) {
		m.shellFocus = true
		m.prefix = false
		return m
	}
	return m.clickLeftPane(x, y)
}

// inShellPane reports whether a point sits inside the embedded shell
// pane.
func (m model) inShellPane(x, y int) bool {
	l := m.layout()
	if y < l.shellTop || y >= m.height-1 {
		return false
	}
	return l.stacked || x >= l.leftW
}

// clickLeftPane maps a click to the entry on that line. X selects the
// pane in the side-by-side layout, Y in the stacked one, and the
// line-to-entry map comes from the same leftLines the view renders, so
// clicks cannot drift from what is on screen.
func (m model) clickLeftPane(x, y int) model {
	l := m.layout()
	if l.stacked {
		if y < contentStartY || y >= contentStartY+l.leftH-1 {
			return m
		}
	} else {
		if x < 0 || x >= l.leftW {
			return m
		}
	}
	_, targets := m.leftLines()
	if idx, ok := targets[y-contentStartY]; ok && idx < len(m.boxes) {
		m.selected = m.boxes[idx].Name
	}
	return m
}
