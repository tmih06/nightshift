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
	case "down", "j":
		m.moveSelection(1)
	case "h", "left", "[":
		m.setSection(m.section - 1)
	case "l", "right", "]":
		m.setSection(m.section + 1)
	case "g":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[0]].Name)
		}
	case "G":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[len(order)-1]].Name)
		}
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd(m.agentVersions)
	case "c":
		pool := m.claimPool()
		m.flash("claiming from " + pool + "…")
		return m, claimCmd(pool)
	case "enter":
		if m.isWarmSelected() {
			m.focusWarmInspect()
			return m, nil
		}
		fallthrough
	case "b":
		b := m.selectedBox()
		if b == nil {
			return m, nil
		}
		if !b.Claimed() {
			m.flash("warm pool sandbox — claim it with c, bash starts with the claim")
			return m, nil
		}
		if b.Phase != "Running" {
			m.flash("pod is " + b.Phase + ", no bash to enter")
			return m, nil
		}
		m.flash("starting bash in " + b.Name)
		return m, attachCmd(b.Name)
	case "F":
		b := m.selectedBox()
		if b == nil || b.Phase != "Running" {
			return m, nil
		}
		if !b.Claimed() {
			m.flash("warm pool sandbox — claim it with c first")
			return m, nil
		}
		m.flash("opening fullscreen bash, detach with Ctrl-\\")
		return m, fullCmd(b.Name, m.activeTerm(b.Name))
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
		return m, deleteClaimCmd(b.Owner)
	}
	return m, nil
}

func (m model) updateInspectKeys(msg tea.KeyPressMsg) (model, tea.Cmd) {
	if !m.isWarmSelected() {
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
		m.inspectScroll = 0
	case "G", "end":
		m.scrollInspect(1 << 20)
	case "c":
		m.focus = focusNav
		pool := m.claimPool()
		m.flash("claiming from " + pool + "…")
		return m, claimCmd(pool)
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd(m.agentVersions)
	}
	return m, nil
}
