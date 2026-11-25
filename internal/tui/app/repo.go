package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/tui/repoview"
)

// WithCurrentRepo sets the initial repository ("owner/name") highlighted when
// the Repo view opens.
func (m Model) WithCurrentRepo(repo string) Model {
	m.currentRepo = repo
	return m
}

// StartOnRepos opens nightshift on the GitHub tab in the Repo view.
func (m Model) StartOnRepos() Model {
	m.active = TabGitHub
	m.sandboxes = m.sandboxes.SetHidden(true)
	m.startRepos = true
	return m
}

// openRepos switches the GitHub tab to the 3-pane repository workspace,
// creating it on first use so nightshift starts without loading repo data
// until requested.
func (m Model) openRepos() (Model, tea.Cmd) {
	if m.repos == nil {
		if m.newRepos != nil {
			m.repos = m.newRepos()
		} else {
			m.repos = repoview.New(m.theme, m.styles, repoview.Options{
				CurrentRepo: m.currentRepo,
			})
		}
	}
	m.ghScreen = ghScreenRepos
	m.syncRepoSandboxes()
	resizeCmd := m.updateRepos(tea.WindowSizeMsg{Width: m.width, Height: max(0, m.height-barHeight)})
	loadCmd := m.repos.EnsureLoaded()
	return m, tea.Batch(resizeCmd, loadCmd)
}

func (m *Model) updateRepos(msg tea.Msg) tea.Cmd {
	if m.repos == nil || !repoview.Wants(msg) {
		return nil
	}
	next, cmd := m.repos.Update(msg)
	if rv, ok := next.(*repoview.Model); ok {
		m.repos = rv
	}
	return cmd
}

func (m *Model) syncRepoSandboxes() {
	if m.repos == nil {
		return
	}
	boxes := m.sandboxes.Boxes()
	active := make([]repoview.ActiveSandbox, 0, len(boxes))
	for _, b := range boxes {
		if b.Volume == "" {
			continue
		}
		active = append(active, repoview.ActiveSandbox{
			Pod:    b.Name,
			Volume: b.Volume,
			Phase:  b.Phase,
		})
	}
	m.repos.SetSandboxes(active)
}
