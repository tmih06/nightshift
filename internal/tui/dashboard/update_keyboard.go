package dashboard

import tea "charm.land/bubbletea/v2"

func (m model) updateKeyboard(msg tea.KeyPressMsg) (model, tea.Cmd) {
	if m.confirmDestroy && msg.String() != "x" {
		m.confirmDestroy = false
	}
	switch msg.String() {
	case "?":
		m.showHelp = !m.showHelp
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
		cmd := m.maybeFetchLogs()
		return m, cmd
	case "down", "j":
		m.moveSelection(1)
		cmd := m.maybeFetchLogs()
		return m, cmd
	case "h", "left", "[":
		m.setSection(m.section - 1)
		cmd := m.maybeFetchLogs()
		return m, cmd
	case "l", "right", "]":
		m.setSection(m.section + 1)
		cmd := m.maybeFetchLogs()
		return m, cmd
	case "g":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[0]].Name)
			cmd := m.maybeFetchLogs()
			return m, cmd
		}
	case "G":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[len(order)-1]].Name)
			cmd := m.maybeFetchLogs()
			return m, cmd
		}
	case "v":
		cmd := m.cycleSidebarMode()
		return m, cmd
	case "L":
		cmd := m.setSidebarMode(paneLogs)
		return m, cmd
	case "r":
		m.status, m.statusTTL = "", 0
		cmd := m.refresh()
		return m, cmd
	case "c":
		cmd := m.claim()
		return m, cmd
	case "enter":
		if m.showingDocPane() {
			m.focusDocPane()
			cmd := m.maybeFetchLogs()
			return m, cmd
		}
		fallthrough
	case "b":
		b := m.selectedBox()
		if b == nil {
			return m, nil
		}
		if b.Warm() {
			m.flash("warm pool sandbox — claim it with c, bash starts with the claim")
			return m, nil
		}
		if !b.Claimed() {
			m.flash("infra pod — view logs or configs with enter")
			return m, nil
		}
		if b.Phase != "Running" {
			m.flash("pod is " + b.Phase + ", no bash to enter")
			return m, nil
		}
		m.sidebarMode = paneShell
		m.flash("starting bash in " + b.Name)
		return m, attachCmd(m.api, b.Name)
	case "F":
		b := m.selectedBox()
		if b == nil || b.Phase != "Running" {
			return m, nil
		}
		if b.Warm() {
			m.flash("warm pool sandbox — claim it with c first")
			return m, nil
		}
		if !b.Claimed() {
			m.flash("only a claimed sandbox can open fullscreen bash")
			return m, nil
		}
		return m.zoomShell(b)
	case "x":
		b := m.selectedBox()
		if b == nil || !b.Claimed() {
			m.flash("only a claimed sandbox can be destroyed")
			return m, nil
		}
		if b.OwnerKind != "SandboxClaim" {
			m.confirmDestroy = false
			m.destroy = &destroyDialog{pod: b.Name, volume: b.Volume}
			return m, nil
		}
		if !m.confirmDestroy {
			m.confirmDestroy = true
			m.sticky("press x again to destroy claim " + b.Owner)
			return m, nil
		}
		return m, deleteClaimCmd(m.api, b.Owner)
	}
	return m, nil
}

func (m model) updateInspectKeys(msg tea.KeyPressMsg) (model, tea.Cmd) {
	if !m.showingDocPane() {
		m.focus = focusNav
		return m, nil
	}
	page := max(m.inspectPageSize()/2, 1)
	switch msg.String() {
	case "esc":
		m.focus = focusNav
	case "?":
		m.showHelp = !m.showHelp
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.scrollInspect(-1)
	case "down", "j":
		m.scrollInspect(1)
	case "pgup", "ctrl+u":
		m.scrollInspect(-page)
	case "pgdown", "ctrl+d":
		m.scrollInspect(page)
	case "g", "home":
		m.logFollow = false
		m.inspectScroll = 0
	case "G", "end":
		m.scrollInspect(1 << 20)
	case "v":
		cmd := m.cycleSidebarMode()
		return m, cmd
	case "L":
		cmd := m.setSidebarMode(paneLogs)
		return m, cmd
	case "c":
		m.focus = focusNav
		cmd := m.claim()
		return m, cmd
	case "r":
		m.status, m.statusTTL = "", 0
		cmd := m.refresh()
		return m, cmd
	}
	return m, nil
}

// refresh reloads the fleet, and the logs when they are shown.
func (m *model) refresh() tea.Cmd {
	return tea.Batch(fetchCmd(m.api), m.maybeFetchLogs())
}

func (m *model) claim() tea.Cmd {
	pool := m.claimPool()
	m.flash("claiming from " + pool + "…")
	return claimCmd(m.api, pool)
}
