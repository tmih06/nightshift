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
		if m.destroy != nil {
			return m, nil
		}
		if m.focus == focusShell {
			return m.pasteIntoShell(msg.Content)
		}
	case tea.KeyPressMsg:
		if m.destroy != nil {
			return m.updateDestroyKeys(msg)
		}
		switch m.focus {
		case focusShell:
			return m.updateShellKeys(msg)
		case focusInspect:
			return m.updateInspectKeys(msg)
		default:
			return m.updateKeyboard(msg)
		}
	case tea.MouseMsg:
		if m.destroy != nil {
			return m.destroyMouse(msg)
		}
		return m.updateMouse(msg)
	case tickMsg:
		return m, tea.Batch(fetchCmd(m.agentVersions), tickCmd())
	case shellTickMsg:
		return m, tea.Batch(shellTickCmd(), m.watchShell())
	case shellMsg:
		m.watchingShell = false
		if m.selected == msg.pod && (msg.term == "" || msg.term == m.activeTerm(msg.pod)) {
			if msg.err != nil {
				m.shellLines, m.shellPod, m.shellTerm = nil, "", ""
				m.shellW, m.shellH = 0, 0
				return m, nil // retry on the next tick
			}
			if !m.hidden && msg.lines != nil {
				m.shellLines, m.shellPod, m.shellTerm = msg.lines, msg.pod, msg.term
			}
		}
		return m, m.watchShell()
	case loadedMsg:
		prev := m.selectedIndex()
		m.boxes, m.pools = msg.boxes, msg.pools
		m.windows = msg.windows
		if msg.sessions != nil {
			m.sessions = msg.sessions
		} else {
			m.sessions = map[string]bool{}
		}
		for pod, terms := range m.windows {
			if len(terms) > 0 {
				m.sessions[pod] = true
			}
		}
		if m.activeWindow == nil {
			m.activeWindow = map[string]string{}
		}
		for pod, cur := range m.activeWindow {
			wins := m.podWindows(pod)
			if len(wins) == 0 {
				delete(m.activeWindow, pod)
				continue
			}
			found := false
			for _, w := range wins {
				if w.Address == cur {
					found = true
					break
				}
			}
			if !found {
				m.activeWindow[pod] = wins[0].Address
				if pod == m.selected {
					m.shellLines, m.shellTerm = nil, ""
					m.shellW, m.shellH = 0, 0
				}
			}
		}
		m.metricsOK, m.err, m.loaded = msg.metricsOK, msg.err, true
		m.dataRev++
		m.updatedAt = time.Now()
		m.applyLive(msg.cgroups)
		m.appendHistory()
		m.mergeAgentVersions(msg.agentVersions)
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
				if co := m.pendingCheckout; co != nil {
					m.pendingCheckout = nil
					if m.hasSession(b.Name) {
						return m.focusCheckout(b.Name, *co)
					}
				}
				if m.hasSession(b.Name) {
					m.flash("bash ready in " + b.Name + " — enter attaches")
					return m, fetchCmd(m.agentVersions)
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
			return m, fetchCmd(m.agentVersions)
		}
		m.pendingPod = msg.pod
		m.flash("bash ready in " + msg.pod)
		return m, fetchCmd(m.agentVersions)
	case deletedMsg:
		if msg.err != nil {
			m.fail("destroy failed: " + msg.err.Error())
			return m, nil
		}
		switch {
		case msg.volumeGone:
			m.flash("destroyed sandbox " + msg.sandbox + " and volume " + msg.volume)
		case msg.volume != "":
			m.flash("destroyed sandbox " + msg.sandbox + ", volume " + msg.volume + " kept")
		default:
			m.flash("destroyed claim " + msg.claim + ", pool is restocking")
		}
		m.confirmDestroy = false
		return m, fetchCmd(m.agentVersions)
	case attachReadyMsg:
		if msg.err != nil {
			m.fail("session failed: " + msg.err.Error())
			return m, nil
		}
		if m.sessions == nil {
			m.sessions = map[string]bool{}
		}
		m.sessions[msg.pod] = true
		if len(msg.terms) > 0 {
			if m.windows == nil {
				m.windows = map[string][]session.Terminal{}
			}
			m.windows[msg.pod] = msg.terms
		}
		if b := m.selectedBox(); b != nil && b.Name == msg.pod && b.Claimed() {
			m.focus = focusShell
			m.prefix = false
			m.flash("bash ready — C-b q or click sidebar to leave")
			return m, m.watchShell()
		}
		return m, nil
	case windowCreatedMsg:
		if msg.err != nil {
			m.fail("new window failed: " + msg.err.Error())
			return m, nil
		}
		if m.sessions == nil {
			m.sessions = map[string]bool{}
		}
		if m.windows == nil {
			m.windows = map[string][]session.Terminal{}
		}
		if m.activeWindow == nil {
			m.activeWindow = map[string]string{}
		}
		m.sessions[msg.pod] = true
		wins := append([]session.Terminal(nil), m.podWindows(msg.pod)...)
		exists := false
		for _, w := range wins {
			if w.Address == msg.term.Address {
				exists = true
				break
			}
		}
		if !exists {
			wins = append(wins, msg.term)
		}
		m.windows[msg.pod] = wins
		m.dataRev++
		m.flash("opened window " + msg.term.Label())
		cmd := m.selectWindow(msg.pod, msg.term.Address)
		if b := m.selectedBox(); b != nil && b.Name == msg.pod && b.Claimed() {
			m.focus = focusShell
			m.prefix = false
		}
		return m, tea.Batch(cmd, fetchCmd(m.agentVersions))
	case windowKilledMsg:
		if msg.err != nil {
			m.fail("close window failed: " + msg.err.Error())
			return m, nil
		}
		wins := m.podWindows(msg.pod)
		next := make([]session.Terminal, 0, len(wins))
		removedIdx := 0
		for i, w := range wins {
			if w.Address == msg.term {
				removedIdx = i
				continue
			}
			next = append(next, w)
		}
		if m.windows == nil {
			m.windows = map[string][]session.Terminal{}
		}
		if len(next) == 0 {
			delete(m.windows, msg.pod)
			delete(m.sessions, msg.pod)
			delete(m.activeWindow, msg.pod)
			if m.selected == msg.pod {
				m.shellLines, m.shellPod, m.shellTerm = nil, "", ""
				m.shellW, m.shellH = 0, 0
				if m.focus == focusShell {
					m.focus = focusNav
					m.prefix = false
				}
			}
			m.dataRev++
			m.flash("closed last window in " + msg.pod)
			return m, fetchCmd(m.agentVersions)
		}
		m.windows[msg.pod] = next
		m.dataRev++
		m.flash("closed window")
		pick := next[clamp(removedIdx, 0, len(next)-1)].Address
		if m.activeWindow[msg.pod] == msg.term || m.activeWindow[msg.pod] == "" {
			return m, tea.Batch(m.selectWindow(msg.pod, pick), fetchCmd(m.agentVersions))
		}
		return m, fetchCmd(m.agentVersions)
	case fullReadyMsg:
		if msg.err != nil {
			m.fail("session failed: " + msg.err.Error())
			return m, nil
		}
		return m, tea.ExecProcess(session.Attach(msg.pod, msg.term), func(err error) tea.Msg {
			// Fullscreen may have resized the shell behind the embedded viewer.
			session.DisconnectViewer(msg.pod, msg.term)
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
		return m, fetchCmd(m.agentVersions)
	case checkoutMsg:
		return m.checkoutEvent(msg)
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.fail("send failed: " + msg.err.Error())
			m.pending = nil
			m.pendingTerm = ""
			return m, nil
		}
		return m, m.startDrain()
	}
	return m, nil
}
