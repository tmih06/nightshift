package app_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

// nightshift's contract with the host: esc and q leave the pull request
// with ClosedMsg rather than quitting the program.
func TestEscAndQCloseRatherThanQuit(t *testing.T) {
	for _, k := range []string{"esc", "q"} {
		client := &fakeSearcher{prs: samplePRs()}
		client.serveDetail("PR_412", "Caps the backoff at 30s.")
		h := newHost(client).StartOn(samplePRs()[0])
		var m tea.Model = settle(h, append(immediate(h.Init()), tea.WindowSizeMsg{Width: 160, Height: 40})...)

		_, cmd := m.Update(keyMsg(k))
		var closed bool
		queue := immediate(cmd)
		for i := 0; i < 16 && len(queue) > 0; i++ {
			msg := queue[0]
			queue = queue[1:]
			switch msg.(type) {
			case app.ClosedMsg:
				closed = true
			case tea.QuitMsg:
				t.Fatalf("%s quit the program", k)
			default:
				var next tea.Cmd
				m, next = m.Update(msg)
				queue = append(queue, immediate(next)...)
			}
		}
		if !closed {
			t.Errorf("%s did not close the pull request", k)
		}
	}
}
