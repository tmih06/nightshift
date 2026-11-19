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
		if m.shellFocus {
			return m.updateShellKeys(msg)
		}
		return m.updateKeyboard(msg)
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tickMsg:
		return m, tea.Batch(fetchCmd(), tickCmd())
	case shellTickMsg:
		cmds := []tea.Cmd{shellTickCmd()}
		if b := m.selectedBox(); b != nil && m.sessions[b.Name] && b.Phase == "Running" {
			w, h := m.shellPaneSize()
			resize := w != m.shellW || h != m.shellH
			m.shellW, m.shellH = w, h
			cmds = append(cmds, captureCmd(b.Name, w, h, resize))
		}
		return m, tea.Batch(cmds...)
	case shellMsg:
		if msg.err != nil {
			m.shellLines, m.shellPod = nil, ""
			return m, nil
		}
		if b := m.selectedBox(); b != nil && b.Name == msg.pod {
			m.shellLines, m.shellPod = msg.lines, msg.pod
		}
	case loadedMsg:
		prev := m.selectedIndex()
		m.boxes, m.sessions = msg.boxes, msg.sessions
		m.metricsOK, m.err, m.loaded = msg.metricsOK, msg.err, true
		m.appendHistory()
		// The selected pod may be gone (destroyed) or the fleet may
		// have arrived for the first time: snap to the nearest entry.
		if m.selectedBox() == nil && len(m.boxes) > 0 {
			m.selected = m.boxes[clamp(prev, 0, len(m.boxes)-1)].Name
		}
		// A claim whose bash was pre-created: select it and drop straight
		// into the shell.
		if m.pendingPod != "" {
			for _, b := range m.boxes {
				if b.Name != m.pendingPod {
					continue
				}
				m.selected = b.Name
				m.pendingPod = ""
				if m.sessions[b.Name] {
					// No status message: the filled status bar and the
					// shell title are the mode indicators.
					m.shellFocus = true
					m.status = ""
					w, h := m.shellPaneSize()
					resize := w != m.shellW || h != m.shellH
					m.shellW, m.shellH = w, h
					return m, tea.Batch(fetchCmd(), captureCmd(b.Name, w, h, resize))
				}
				break
			}
		}
	case claimMsg:
		if msg.err != nil {
			m.status = "claim failed: " + msg.err.Error()
			return m, nil
		}
		m.status = "claiming " + msg.name + ", waiting for a warm sandbox…"
		return m, waitClaimCmd(msg.name)
	case claimAdoptedMsg:
		if msg.err != nil {
			m.status = "claim failed: " + msg.err.Error()
			return m, fetchCmd()
		}
		m.pendingPod = msg.pod
		m.status = "bash ready in " + msg.pod
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
		m.shellFocus = true
		m.status = "bash runs in the shell pane, esc returns to navigation"
		if b := m.selectedBox(); b != nil && m.sessions[b.Name] {
			w, h := m.shellPaneSize()
			resize := w != m.shellW || h != m.shellH
			m.shellW, m.shellH = w, h
			return m, captureCmd(b.Name, w, h, resize)
		}
		return m, nil
	case fullReadyMsg:
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
			m.status = "back from fullscreen, session still running"
		}
		return m, fetchCmd()
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.status = "send failed: " + msg.err.Error()
			m.pending = nil
			return m, nil
		}
		cmds := []tea.Cmd{}
		if cmd := m.startDrain(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Capture right after the input landed, so the echo comes back
		// in milliseconds instead of waiting for the next shell tick.
		if b := m.selectedBox(); b != nil && m.sessions[b.Name] && b.Phase == "Running" {
			w, h := m.shellPaneSize()
			resize := w != m.shellW || h != m.shellH
			m.shellW, m.shellH = w, h
			cmds = append(cmds, captureCmd(b.Name, w, h, resize))
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}
