package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/session"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		return m.updateKeyboard(msg)
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tickMsg:
		return m, tea.Batch(fetchCmd(), tickCmd())
	case loadedMsg:
		m.boxes, m.sessions = msg.boxes, msg.sessions
		m.metricsOK, m.err, m.loaded = msg.metricsOK, msg.err, true
		if m.selected > len(m.boxes)-1 {
			m.selected = max(len(m.boxes)-1, 0)
		}
	case claimMsg:
		if msg.err != nil {
			m.status = "claim failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "claimed " + msg.name + ", adopting a warm sandbox"
		return m, fetchCmd()
	case deletedMsg:
		if msg.err != nil {
			m.status = "destroy failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "destroyed claim " + msg.claim + ", pool is restocking"
		m.confirmDestroy = false
		return m, fetchCmd()
	case attachReadyMsg:
		if msg.err != nil {
			m.status = "session failed: " + msg.err.Error()
			return m, nil
		}
		return m, tea.ExecProcess(session.Attach(msg.pod), func(err error) tea.Msg {
			return resumeMsg{err: err}
		})
	case resumeMsg:
		if msg.err != nil {
			m.status = "bash ended: " + msg.err.Error()
		} else {
			m.status = "detached, session still running"
		}
		return m, fetchCmd()
	}
	return m, nil
}
