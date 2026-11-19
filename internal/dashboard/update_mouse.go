package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Row 0 is the title line and row 1 the left pane's top border, so the
// left pane's content starts at row 2 in both layouts.
const contentStartY = 2

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// While the shell has focus, wheel events and right-pane clicks
	// stay out of navigation. Clicking the sidebar always drops shell
	// focus and selects the entry; dragging the splitter keeps working
	// without disturbing shell focus.
	if m.focus == focusShell && !m.dragging {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.overDivider(msg.X, msg.Y) {
				m.dragging = true
			} else if m.inLeftPane(msg.X, msg.Y) {
				m = m.clickLeftPane(msg.X, msg.Y)
			}
		}
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.isWarmSelected() && (m.inLowerPane(msg.X, msg.Y) || (m.focus == focusInspect && !m.inLeftPane(msg.X, msg.Y))) {
				m.scrollInspect(-3)
			} else {
				m.moveSelection(-1)
			}
		case tea.MouseButtonWheelDown:
			if m.isWarmSelected() && (m.inLowerPane(msg.X, msg.Y) || (m.focus == focusInspect && !m.inLeftPane(msg.X, msg.Y))) {
				m.scrollInspect(3)
			} else {
				m.moveSelection(1)
			}
		case tea.MouseButtonLeft:
			if m.overDivider(msg.X, msg.Y) {
				m.dragging = true
			} else {
				return m.applyClick(msg.X, msg.Y)
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

// applyClick routes by region: a click inside the lower-right pane
// focuses the warm config viewer for a warm sandbox or bash for a
// claimed sandbox with a live session (never entering bash on a warm
// sandbox). A sidebar click always returns focus to navigation.
func (m model) applyClick(x, y int) (tea.Model, tea.Cmd) {
	if m.inLowerPane(x, y) {
		if m.isWarmSelected() {
			m.focus = focusInspect
			m.prefix = false
			return m, nil
		}
		if m.canFocusShell() {
			m.focus = focusShell
			m.prefix = false
			return m, nil
		}
		if b := m.selectedBox(); b != nil && b.OwnerKind == "SandboxClaim" && b.Phase == "Running" {
			m.flash("starting bash in " + b.Name)
			return m, attachCmd(b.Name)
		}
		return m, nil
	}
	return m.clickLeftPane(x, y), nil
}

// inLeftPane reports whether a point sits inside the sidebar pane.
func (m model) inLeftPane(x, y int) bool {
	l := m.layout()
	if y < 1 || y >= m.height-1 {
		return false
	}
	if l.stacked {
		return y < 1+l.leftH
	}
	return x >= 0 && x < l.leftW
}

// inLowerPane reports whether a point sits inside the lower-right pane
// (embedded bash for claimed sandboxes, configs/templates for warm).
func (m model) inLowerPane(x, y int) bool {
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
	if !m.inLeftPane(x, y) {
		return m
	}
	// A click that lands on the sidebar always returns focus to
	// navigation.
	m.focus = focusNav
	m.prefix = false
	_, targets := m.leftLines()
	if idx, ok := targets[y-contentStartY]; ok && idx < len(m.boxes) {
		m.selectPod(m.boxes[idx].Name)
	}
	return m
}
