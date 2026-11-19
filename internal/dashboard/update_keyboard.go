package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateKeyboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key but x disarms the armed destroy confirmation.
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
		if len(m.boxes) > 0 {
			m.selectPod(m.boxes[0].Name)
		}
	case "G":
		if len(m.boxes) > 0 {
			m.selectPod(m.boxes[len(m.boxes)-1].Name)
		}
	case "tab":
		if m.isWarmSelected() {
			m.focus = focusInspect
		}
	case "r":
		m.status, m.statusTTL = "", 0
		return m, fetchCmd()
	case "c":
		m.flash("claiming from the warm pool…")
		return m, claimCmd()
	case "enter", "b":
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
		// Fullscreen escape hatch for full-screen programs.
		b := m.selectedBox()
		if b == nil || b.Phase != "Running" {
			return m, nil
		}
		if b.OwnerKind != "SandboxClaim" {
			m.flash("warm pool sandbox — claim it with c first")
			return m, nil
		}
		m.flash("opening fullscreen bash, detach with C-b d")
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
