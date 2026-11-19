package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/session"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, transient := msg.(tickMsg); transient {
		m.expireStatus() // each fleet refresh ages transient messages
	}
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
		m.boxes, m.pool, m.sessions = msg.boxes, msg.pool, msg.sessions
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
					// The session is pre-created so enter attaches with no
					// wait, but the dashboard stays in navigation: only
					// enter drops into the shell.
					m.flash("bash ready in " + b.Name + " — enter attaches")
					return m, fetchCmd()
				}
				break
			}
		}
	case claimMsg:
		if msg.err != nil {
			m.sticky("claim failed: " + msg.err.Error())
			return m, nil
		}
		m.flash("claiming " + msg.name + ", waiting for a warm sandbox…")
		return m, waitClaimCmd(msg.name)
	case claimAdoptedMsg:
		if msg.err != nil {
			m.sticky("claim failed: " + msg.err.Error())
			return m, fetchCmd()
		}
		m.pendingPod = msg.pod
		m.flash("bash ready in " + msg.pod)
		return m, fetchCmd()
	case deletedMsg:
		if msg.err != nil {
			m.sticky("destroy failed: " + msg.err.Error())
			return m, nil
		}
		m.flash("destroyed claim " + msg.claim + ", pool is restocking")
		m.confirmDestroy = false
		return m, fetchCmd()
	case attachReadyMsg:
		if msg.err != nil {
			m.sticky("session failed: " + msg.err.Error())
			return m, nil
		}
		m.shellFocus = true
		m.flash("bash ready — enter in the shell pane to focus")
		if b := m.selectedBox(); b != nil && m.sessions[b.Name] {
			w, h := m.shellPaneSize()
			resize := w != m.shellW || h != m.shellH
			m.shellW, m.shellH = w, h
			return m, captureCmd(b.Name, w, h, resize)
		}
		return m, nil
	case fullReadyMsg:
		if msg.err != nil {
			m.sticky("session failed: " + msg.err.Error())
			return m, nil
		}
		return m, tea.ExecProcess(session.Attach(msg.pod), func(err error) tea.Msg {
			return resumeMsg{err: err}
		})
	case resumeMsg:
		if msg.err != nil {
			m.sticky("bash ended: " + msg.err.Error())
		} else {
			m.flash("back from fullscreen, session still running")
		}
		return m, fetchCmd()
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.sticky("send failed: " + msg.err.Error())
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
