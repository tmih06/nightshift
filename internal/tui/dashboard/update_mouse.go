package dashboard

import tea "charm.land/bubbletea/v2"

func (m model) updateMouse(msg tea.MouseMsg) (model, tea.Cmd) {
	mouse := msg.Mouse()
	x, y := mouse.X, mouse.Y
	if m.focus == focusShell && !m.dragging {
		if click, ok := msg.(tea.MouseClickMsg); ok {
			if next, cmd, handled := m.clickSubTabs(x, y, click.Button); handled {
				return next, cmd
			}
			if next, cmd, handled := m.clickWindowBar(x, y, click.Button); handled {
				return next, cmd
			}
			if click.Button == tea.MouseLeft {
				switch {
				case m.overDivider(x, y):
					m.dragging = true
				case m.inTable(x, y) || m.inTabs(y):
					m = m.clickTable(x, y)
					cmd := m.maybeFetchLogs()
					return m, cmd
				}
			}
		}
		return m, nil
	}
	switch msg.(type) {
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
		if m.showingDocPane() && (m.inSidebar(x, y) || (m.focus == focusInspect && !m.inTable(x, y))) {
			m.scrollInspect(3 * delta)
		} else {
			m.moveSelection(delta)
			cmd := m.maybeFetchLogs()
			return m, cmd
		}
	case tea.MouseClickMsg:
		if next, cmd, handled := m.clickSubTabs(x, y, mouse.Button); handled {
			return next, cmd
		}
		if next, cmd, handled := m.clickWindowBar(x, y, mouse.Button); handled {
			return next, cmd
		}
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		if m.overDivider(x, y) {
			m.dragging = true
			return m, nil
		}
		return m.applyClick(x, y)
	case tea.MouseMotionMsg:
		if m.dragging && mouse.Button == tea.MouseLeft {
			m = m.applyDrag(x, y)
		}
	case tea.MouseReleaseMsg:
		m.dragging = false
	}
	return m, nil
}

func (m model) clickSubTabs(x, y int, button tea.MouseButton) (model, tea.Cmd, bool) {
	if button != tea.MouseLeft {
		return m, nil, false
	}
	row := m.subTabRow()
	if row < 0 || y != row {
		return m, nil, false
	}
	for _, s := range m.subTabSpans() {
		if x >= s.from && x < s.to {
			cmd := m.setSidebarMode(s.mode)
			return m, cmd, true
		}
	}
	return m, nil, false
}

// clickWindowBar handles clicks on the sandbox's Herdr window tab bar:
// left-click switches windows or triggers + / ×, middle-click closes a tab.
func (m model) clickWindowBar(x, y int, button tea.MouseButton) (model, tea.Cmd, bool) {
	row := m.windowTabRow()
	if row < 0 || y != row {
		return m, nil, false
	}
	b := m.selectedBox()
	if b == nil || !b.Claimed() {
		return m, nil, false
	}
	for _, s := range m.windowTabSpans() {
		if x < s.from || x >= s.to {
			continue
		}
		switch s.action {
		case winActionSelect:
			if button == tea.MouseMiddle {
				m.prefix = false
				m.flash("closing window…")
				return m, killWindowCmd(m.api, b.Name, s.address), true
			}
			if button != tea.MouseLeft {
				return m, nil, false
			}
			m.focus = focusShell
			m.prefix = false
			return m, m.selectWindow(b.Name, s.address), true
		case winActionNew:
			if button != tea.MouseLeft {
				return m, nil, false
			}
			if b.Phase != "Running" {
				m.flash("pod is " + b.Phase + ", cannot open window")
				return m, nil, true
			}
			m.prefix = false
			m.flash("opening new window…")
			return m, createWindowCmd(m.api, b.Name, m.activeTerm(b.Name)), true
		case winActionClose:
			if button != tea.MouseLeft {
				return m, nil, false
			}
			term := m.activeTerm(b.Name)
			if term == "" {
				return m, nil, true
			}
			m.prefix = false
			m.flash("closing window…")
			return m, killWindowCmd(m.api, b.Name, term), true
		}
	}
	return m, nil, false
}

// overDivider is whether (x, y) is on the line between the table and the
// sidebar, which a drag moves.
func (m model) overDivider(x, y int) bool {
	l := m.layout()
	if l.stacked {
		return y >= l.sideTop-1 && y <= l.sideTop+1
	}
	return y >= l.bodyTop && y < l.bodyTop+l.bodyH && x >= l.sideX-1 && x <= l.sideX+1
}

func (m model) applyDrag(x, y int) model {
	l := m.layout()
	if l.stacked {
		m.tableHeight = y - l.bodyTop
	} else {
		m.sideWidth = m.width - x
	}
	m.shellW, m.shellH = 0, 0
	return m
}

func (m model) applyClick(x, y int) (model, tea.Cmd) {
	if m.inSidebar(x, y) {
		if m.showingDocPane() {
			m.focusDocPane()
			m.prefix = false
			cmd := m.maybeFetchLogs()
			return m, cmd
		}
		if m.canFocusShell() {
			m.focus = focusShell
			m.prefix = false
			return m, nil
		}
		if b := m.selectedBox(); b != nil && b.Claimed() && b.Phase == "Running" {
			m.flash("starting bash in " + b.Name)
			return m, attachCmd(m.api, b.Name)
		}
		return m, nil
	}
	m = m.clickTable(x, y)
	cmd := m.maybeFetchLogs()
	return m, cmd
}

func (m model) inTabs(y int) bool { return y >= 0 && y < tabsH }

func (m model) inTable(x, y int) bool {
	l := m.layout()
	if y < l.bodyTop || y >= l.bodyTop+l.tableH {
		return false
	}
	return l.stacked || x < l.tableW
}

func (m model) inSidebar(x, y int) bool {
	l := m.layout()
	if y < l.sideTop || y >= l.sideTop+l.sideH {
		return false
	}
	return l.stacked || x >= l.sideX
}

// clickTable selects the section tab or the sandbox under (x, y) and returns
// to navigation.
func (m model) clickTable(x, y int) model {
	if m.inTabs(y) {
		for i, s := range m.sectionSpans() {
			if y == 0 && x >= s.from && x < s.to {
				m.focus, m.prefix = focusNav, false
				m.setSection(i)
			}
		}
		return m
	}
	if !m.inTable(x, y) {
		return m
	}
	m.focus, m.prefix = focusNav, false
	if i, ok := m.rowAt(y); ok {
		m.selectPod(m.boxes[i].Name)
	}
	return m
}
