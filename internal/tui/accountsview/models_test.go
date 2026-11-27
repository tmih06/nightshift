package accountsview

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func TestTheSelectedAccountListsItsModels(t *testing.T) {
	snap := chainSnapshot()
	snap.Accounts[0].Quota = &v1.AccountQuota{Models: []v1.AccountModel{{ID: "claude-x-1", Name: "Claude X"}, {ID: "only-an-id"}}}
	snap.Accounts[1].Quota = &v1.AccountQuota{ModelsError: "HTTP 500"}
	m := shown(t, &fakeService{snap: snap})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 90})
	view := plain(m)
	for _, want := range []string{"Models", "2 available", "Claude X", "claude-x-1", "only-an-id"} {
		if !strings.Contains(view, want) {
			t.Errorf("the details lack %q:\n%s", want, view)
		}
	}
	m = press(t, m, "j")
	if view := plain(m); strings.Contains(view, "Claude X") || !strings.Contains(view, "could not list them: HTTP 500") {
		t.Errorf("each account shows its own list or why it has none:\n%s", view)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 90})
	for _, line := range m.modelsBlock(snap.Accounts[0], 12) {
		if w := lipgloss.Width(line); strings.Contains(line, "claude-x-1") {
			t.Errorf("a narrow pane must leave the id out, got %d cells: %q", w, line)
		}
	}
}
