package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/session"
)

func (m model) Update(msg tea.Msg) (model, tea.Cmd) {
	if _, transient := msg.(tickMsg); transient {
		m.expireStatus()
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.isWarmSelected() {
			m.refreshInspectCache()
			m.scrollInspect(0)
		}
	case tea.PasteMsg:
		if m.focus == focusShell {
			return m.pasteIntoShell(msg.Content)
		}
	case tea.KeyPressMsg:
		switch m.focus {
		case focusShell:
			return m.updateShellKeys(msg)
		case focusInspect:
			return m.updateInspectKeys(msg)
		default:
			return m.updateKeyboard(msg)
		}
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tickMsg:
		return m, tea.Batch(fetchCmd(), tickCmd())
	case shellTickMsg:
		cmds := []tea.Cmd{shellTickCmd()}
		if b := m.selectedBox(); !m.hidden && b != nil && m.sessions[b.Name] && b.Phase == "Running" {
			cmds = append(cmds, m.captureSelected(b.Name))
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
		m.dataRev++
		m.updatedAt = time.Now()
		m.applyLive(msg.cgroups)
		m.appendHistory()
		if order := m.navigationIndices(); m.selectedBox() == nil && len(order) > 0 {
			// The selection is gone (destroyed, say): take its neighbour in
			// the section, which may itself now be empty.
			m.selectPod(m.boxes[order[clamp(prev, 0, len(order)-1)]].Name)
		} else {
			m.selectPod(m.selected)
		}
		if m.pendingPod != "" {
			for _, b := range m.boxes {
				if b.Name != m.pendingPod {
					continue
				}
				m.selectPod(b.Name)
				m.pendingPod = ""
				if m.sessions[b.Name] {
					m.flash("bash ready in " + b.Name + " — enter attaches")
					return m, fetchCmd()
				}
				break
			}
		}
	case claimMsg:
		if msg.err != nil {
			m.fail("claim failed: " + msg.err.Error())
			return m, nil
		}
		m.flash("claiming " + msg.name + ", waiting for a warm sandbox…")
		return m, waitClaimCmd(msg.name)
	case claimAdoptedMsg:
		if msg.err != nil {
			m.fail("claim failed: " + msg.err.Error())
			return m, fetchCmd()
		}
		m.pendingPod = msg.pod
		m.flash("bash ready in " + msg.pod)
		return m, fetchCmd()
	case deletedMsg:
		if msg.err != nil {
			m.fail("destroy failed: " + msg.err.Error())
			return m, nil
		}
		m.flash("destroyed claim " + msg.claim + ", pool is restocking")
		m.confirmDestroy = false
		return m, fetchCmd()
	case attachReadyMsg:
		if msg.err != nil {
			m.fail("session failed: " + msg.err.Error())
			return m, nil
		}
		if m.sessions == nil {
			m.sessions = map[string]bool{}
		}
		m.sessions[msg.pod] = true
		if b := m.selectedBox(); b != nil && b.Name == msg.pod && b.OwnerKind == "SandboxClaim" {
			m.focus = focusShell
			m.prefix = false
			m.flash("bash ready — C-b q or click sidebar to leave")
			return m, m.captureSelected(b.Name)
		}
		return m, nil
	case fullReadyMsg:
		if msg.err != nil {
			m.fail("session failed: " + msg.err.Error())
			return m, nil
		}
		return m, tea.ExecProcess(session.Attach(msg.pod), func(err error) tea.Msg {
			return resumeMsg{err: err}
		})
	case resumeMsg:
		// Fullscreen attach changes the terminal size; restore the embedded size.
		m.shellW, m.shellH = 0, 0
		if msg.err != nil {
			m.fail("bash ended: " + msg.err.Error())
		} else {
			m.flash("back from fullscreen, session still running")
		}
		return m, fetchCmd()
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.fail("send failed: " + msg.err.Error())
			m.pending = nil
			return m, nil
		}
		var cmds []tea.Cmd
		if cmd := m.startDrain(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if b := m.selectedBox(); b != nil && m.sessions[b.Name] && b.Phase == "Running" {
			cmds = append(cmds, m.captureSelected(b.Name))
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	}
	return m, nil
}
