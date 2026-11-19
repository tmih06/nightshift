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
		m.selected = 0
	case "G":
		m.selected = max(len(m.boxes)-1, 0)
	case "r":
		m.status = ""
		return m, fetchCmd()
	case "c":
		m.status = "claiming from the warm pool…"
		return m, claimCmd()
	case "enter", "b":
		b := m.selectedBox()
		if b == nil {
			return m, nil
		}
		if b.Phase != "Running" {
			m.status = "pod is " + b.Phase + ", no bash to enter"
			return m, nil
		}
		m.status = "starting bash in " + b.Name
		return m, attachCmd(b.Name)
	case "F":
		// Fullscreen escape hatch for full-screen programs.
		b := m.selectedBox()
		if b == nil || b.Phase != "Running" {
			return m, nil
		}
		m.status = "opening fullscreen bash, detach with C-b d"
		return m, fullCmd(b.Name)
	case "x":
		b := m.selectedBox()
		if b == nil || b.OwnerKind != "SandboxClaim" {
			m.status = "only a claimed sandbox can be destroyed"
			return m, nil
		}
		if !m.confirmDestroy {
			m.confirmDestroy = true
			m.status = "press x again to destroy claim " + b.Owner
			return m, nil
		}
		return m, deleteClaimCmd(b.Owner)
	}
	return m, nil
}
