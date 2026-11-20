package dashboard

import tea "github.com/charmbracelet/bubbletea"

func (m model) updateKeyboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmDestroy && msg.String() != "x" {
		m.confirmDestroy = false
	}
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
	case "down", "j":
		m.moveSelection(1)
	case "g":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[0]].Name)
		}
	case "G":
		if order := m.navigationIndices(); len(order) > 0 {
			m.selectPod(m.boxes[order[len(order)-1]].Name)
		}
	case "tab":
		if m.isWarmSelected() {
			m.focusWarmInspect()
		}
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd()
	case "c":
		m.flash("claiming from the warm pool…")
		return m, claimCmd()
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
		if b.OwnerKind != "SandboxClaim" {
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
		if b.OwnerKind != "SandboxClaim" {
			m.flash("warm pool sandbox — claim it with c first")
			return m, nil
		}
		m.flash("opening fullscreen bash, detach with Ctrl-\\")
		return m, fullCmd(b.Name)
	case "x":
		b := m.selectedBox()
		if b == nil || b.OwnerKind != "SandboxClaim" {
			m.flash("only a claimed sandbox can be destroyed")
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

func (m model) updateInspectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.isWarmSelected() {
		m.focus = focusNav
		return m, nil
	}
	page := max(m.inspectPageSize()/2, 1)
	switch msg.String() {
	case "esc", "tab":
		m.focus = focusNav
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
		m.flash("claiming from the warm pool…")
		return m, claimCmd()
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd()
	}
	return m, nil
}
