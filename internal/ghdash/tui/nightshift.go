package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
)

// OpenPullRequestMsg asks the host to open a pull request in its own view.
// nightshift sends it from enter on a pull request row and shows the pull
// request in zen-octo's view; gh-dash itself does nothing with it.
type OpenPullRequestMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
}

// SandboxPullRequestMsg asks the host to check a pull request out in a new
// sandbox. nightshift sends it from S on a pull request row, asks which warm
// pool to claim from, and checks the pull request out in the claim's shell.
type SandboxPullRequestMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
	Branch string // the head branch; empty when gh-dash does not know it
}

// SandboxIssueMsg asks the host to check out a branch for an issue in a new
// sandbox. nightshift sends it from S on an issue row, asks for a warm pool
// and branch name, and checks that branch out in the sandbox's shell.
type SandboxIssueMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
	Title  string
}

// OpenRepoViewMsg asks the host to open its repository view.
// nightshift opens its Repo view on it; gh-dash itself does nothing with it.
type OpenRepoViewMsg struct{}

// SwitchView switches gh-dash directly to view so the host can return from
// an external view (such as nightshift's Repo view) straight into any
// gh-dash view.
func (m *Model) SwitchView(view config.ViewType) tea.Cmd {
	if m.ctx == nil {
		return nil
	}
	if m.ctx.Config == nil {
		m.ctx.View = view
		return nil
	}
	return m.switchToView(view)
}
