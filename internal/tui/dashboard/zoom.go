package dashboard

import (
	"github.com/tmih06/nightshift/internal/tui/fleet"

	tea "charm.land/bubbletea/v2"
)

// zoomShell gives the shell the whole window, hiding the table and the
// sidebar's chrome, or takes it back. It draws what the terminal shows with
// the services' screen API at the window's size, so it works the same against
// a server on another machine.
func (m model) zoomShell(b *fleet.Sandbox) (model, tea.Cmd) {
	if m.zoom {
		m.zoom = false
		m.shellW, m.shellH = 0, 0
		m.flash("back from the full window, session still running")
		watch := m.watchShell()
		return m, watch
	}
	m.zoom = true
	m.sidebarMode = paneShell
	m.focus = focusShell
	m.prefix = false
	m.shellW, m.shellH = 0, 0
	m.flash("the whole window is the shell · C-b f gives it back")
	watch := m.watchShell()
	return m, tea.Batch(attachCmd(m.api, b.Name), watch)
}

// zoomLayout is the layout while the shell has the whole window.
func (m model) zoomLayout(l layout, rows int) layout {
	l.sideW, l.tableW, l.tableH = m.width, 0, 0
	l.sideX, l.sideTop, l.sideH = 0, l.bodyTop, l.bodyH
	l.docTop = l.bodyTop + rows
	l.docW = max(m.width-4, 1)
	l.docH = max(l.bodyH-rows, 1)
	return l
}
