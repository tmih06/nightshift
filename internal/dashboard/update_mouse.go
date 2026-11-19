package dashboard

import tea "github.com/charmbracelet/bubbletea"

// Row 0 is the title line and row 1 the top border, so sidebar content starts at row 2.
const contentStartY = 2

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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

func (m model) overDivider(x, y int) bool {
	l := m.layout()
	if l.stacked {
		d := contentStartY + l.leftH - 1
		return y >= d-1 && y <= d+1
	}
	return x >= l.leftW-2 && x <= l.leftW+1
}

func (m model) applyDrag(x, y int) model {
	if m.width < stackedBelow {
		m.leftHeight = y - contentStartY + 1
	} else {
		m.leftWidth = x
	}
	m.refreshInspectCache()
	return m
}

func (m model) applyClick(x, y int) (tea.Model, tea.Cmd) {
	if m.inLowerPane(x, y) {
		if m.isWarmSelected() {
			m.focusWarmInspect()
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

func (m model) inLowerPane(x, y int) bool {
	l := m.layout()
	if y < l.shellTop || y >= m.height-1 {
		return false
	}
	return l.stacked || x >= l.leftW
}

func (m model) clickLeftPane(x, y int) model {
	if !m.inLeftPane(x, y) {
		return m
	}
	m.focus = focusNav
	m.prefix = false
	_, targets := m.leftLines()
	if idx, ok := targets[y-contentStartY]; ok && idx < len(m.boxes) {
		m.selectPod(m.boxes[idx].Name)
	}
	return m
}
