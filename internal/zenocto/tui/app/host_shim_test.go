package app_test

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
	"github.com/tmih06/nightshift/internal/zenocto/tui/syntax"
	"github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

// listShim stands in for zen-octo's pull request list, which nightshift
// does not carry: gh-dash is the list. One host lives behind it, as in
// nightshift, and enter opens the PR under the cursor through StartOn, so
// the host's own tests still run against the path nightshift uses.
type listShim struct {
	host   app.Model
	open   bool
	prs    []gh.PullRequest
	cursor int
}

// openMsg is what zen-octo's list sent on enter; the open runs a beat later.
type openMsg struct{ pr gh.PullRequest }

type testConfigT struct{}

func testConfig() *testConfigT { return &testConfigT{} }

func newApp(_ any, client app.GitHub, _ any, _ any) tea.Model {
	res, _ := client.SearchPullRequests(context.Background(), "", 20)
	return listShim{host: newHost(client), prs: res.PullRequests}
}

func newHost(client app.GitHub) app.Model {
	syn, _ := syntax.New(theme.SyntaxDark)
	return app.New(client, testTheme, syn)
}

func (s listShim) Init() tea.Cmd { return s.host.Init() }

func (s listShim) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(app.ClosedMsg); ok {
		s.open = false
		return s, nil
	}
	if o, ok := msg.(openMsg); ok {
		s.host = s.host.StartOn(o.pr)
		s.open = true
		return s, s.host.Opening()
	}
	if k, ok := msg.(tea.KeyPressMsg); ok && !s.open {
		switch k.String() {
		case "j", "down":
			s.cursor = min(s.cursor+1, max(len(s.prs)-1, 0))
		case "k", "up":
			s.cursor = max(s.cursor-1, 0)
		case "enter":
			if len(s.prs) == 0 {
				return s, nil
			}
			pr := s.prs[s.cursor]
			return s, func() tea.Msg { return openMsg{pr} }
		}
		return s, nil
	}
	next, cmd := s.host.Update(msg)
	s.host = next.(app.Model)
	return s, cmd
}

func (s listShim) View() tea.View { return s.host.View() }
