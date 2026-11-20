package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/dashboard"
	"github.com/tmih06/nightshift/internal/dialog"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
)

// picker asks which pool's template a pull request's sandbox starts from:
// the choice of agent. The sandbox starts cold on the pull request's volume,
// as a running pod cannot take a new volume.
type picker struct {
	pr     ghtui.SandboxPullRequestMsg
	pools  []cluster.Pool
	cursor int
}

// openPicker answers S on a pull request with the pools the fleet shows.
func (m Model) openPicker(msg ghtui.SandboxPullRequestMsg) (Model, tea.Cmd) {
	if _, name, ok := strings.Cut(msg.Repo, "/"); !ok || name == "" || msg.Number <= 0 {
		return m.noticeErr(fmt.Sprintf("cannot check out %q", msg.URL))
	}
	pools := m.sandboxes.WarmPools()
	if len(pools) == 0 {
		return m.noticeErr("no warm pools to claim from: is the cluster up?")
	}
	m.picker = &picker{pr: msg, pools: pools}
	return m, nil
}

func (m Model) updatePicker(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
	menu := m.pickerMenu()
	switch s := msg.String(); s {
	case "esc", "q", "ctrl+c":
		m.picker = nil
		return m, nil
	case "up", "k", "shift+tab":
		menu.Move(-1)
	case "down", "j", "tab":
		menu.Move(1)
	case "enter":
		return m.pickPool(p.cursor)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.pools) {
			return m.pickPool(int(s[0] - '1'))
		}
	}
	p.cursor = menu.Cursor
	m.picker = &p
	return m, nil
}

// pickPool starts the checkout from the chosen pool and moves to the
// sandbox tab, which shows its progress.
func (m Model) pickPool(i int) (Model, tea.Cmd) {
	p := m.picker
	m.picker = nil
	m.active = TabSandboxes
	m.sandboxes = m.sandboxes.SetHidden(false)
	var cmd tea.Cmd
	m.sandboxes, cmd = m.sandboxes.CheckOut(p.pools[i].Name,
		dashboard.Checkout{Repo: p.pr.Repo, Number: p.pr.Number, Branch: p.pr.Branch})
	return m, cmd
}

// pickerMouse picks a pool on a click on its row, moves the choice with the
// wheel, and closes the picker on a click outside it.
func (m Model) pickerMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	placed := m.pickerPlaced()
	mouse := msg.Mouse()
	y := mouse.Y - barHeight
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		p := *m.picker
		if msg.Button == tea.MouseWheelUp {
			p.cursor = max(0, p.cursor-1)
		} else if msg.Button == tea.MouseWheelDown {
			p.cursor = min(len(p.pools)-1, p.cursor+1)
		}
		m.picker = &p
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if !placed.Inside(mouse.X, y) {
			m.picker = nil
			return m, nil
		}
		if i, ok := placed.OptionAt(mouse.X, y); ok {
			return m.pickPool(i)
		}
	}
	return m, nil
}

func (m Model) pickerMenu() dialog.Menu {
	p := m.picker
	nameW, tmplW := 0, 0
	for _, pool := range p.pools {
		nameW = max(nameW, lipgloss.Width(pool.Name))
		tmplW = max(tmplW, lipgloss.Width(pool.Template))
	}
	opts := make([]dialog.Option, len(p.pools))
	for i, pool := range p.pools {
		opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s  %-*s ", i+1, nameW, pool.Name, tmplW, pool.Template)}
	}
	branch := p.pr.Branch
	if branch == "" {
		branch = fmt.Sprintf("pr-%d", p.pr.Number)
	}
	return dialog.Menu{
		Title:   fmt.Sprintf("Check out %s#%d in a sandbox", p.pr.Repo, p.pr.Number),
		Lines:   []string{"branch " + branch + " · volume " + cluster.VolumeName(p.pr.Repo, p.pr.Number)},
		Options: opts,
		Cursor:  p.cursor,
		Hint:    "↑/↓ choose agent · enter start · esc cancel",
	}
}

func (m Model) pickerPlaced() dialog.Placed {
	return m.pickerMenu().Place(m.theme, m.width, max(0, m.height-barHeight))
}

// overPicker draws the picker over the body.
func (m Model) overPicker(body string) string {
	p := m.pickerPlaced()
	return dialog.Overlay(body, p.Box, p.X, p.Y, max(0, m.height-barHeight))
}
