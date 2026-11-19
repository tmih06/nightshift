// The dashboard model. Navigation lives in the left pane (warm and
// claimed sandboxes as separate sections), the selected sandbox's detail
// in the right. The session map mirrors which pods have a live tmux
// session, so bash survives navigation and quitting.
package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

const refreshEvery = 2 * time.Second

type model struct {
	boxes     []cluster.Sandbox
	selected  int
	sessions  map[string]bool // pod name → live tmux session
	metricsOK bool
	err       error
	loaded    bool
	status    string
	// confirmDestroy arms the x key so one stray press cannot tear a
	// claimed sandbox down. The second press fires.
	confirmDestroy bool
	width          int
	height         int
}

// New returns the model the program runs.
func New() tea.Model {
	return model{sessions: map[string]bool{}}
}

type tickMsg struct{}

type loadedMsg struct {
	boxes     []cluster.Sandbox
	sessions  map[string]bool
	metricsOK bool
	err       error
}

type claimMsg struct {
	name string
	err  error
}

type deletedMsg struct {
	claim string
	err   error
}

type attachReadyMsg struct {
	pod string
	err error
}

type resumeMsg struct {
	err error
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func fetchCmd() tea.Cmd {
	return func() tea.Msg {
		boxes, metricsOK, err := cluster.Fetch()
		pods := make([]string, len(boxes))
		for i, b := range boxes {
			pods[i] = b.Name
		}
		return loadedMsg{boxes: boxes, sessions: session.Existing(pods),
			metricsOK: metricsOK, err: err}
	}
}

func claimCmd() tea.Cmd {
	return func() tea.Msg {
		name, err := cluster.Claim()
		return claimMsg{name: name, err: err}
	}
}

func deleteClaimCmd(claim string) tea.Cmd {
	return func() tea.Msg {
		err := cluster.DeleteClaim(claim)
		return deletedMsg{claim: claim, err: err}
	}
}

// attachCmd ensures the tmux session exists off the render loop, then
// the attachReadyMsg handler suspends into it.
func attachCmd(pod string) tea.Cmd {
	return func() tea.Msg {
		if err := session.Ensure(pod); err != nil {
			return attachReadyMsg{pod: pod, err: err}
		}
		return attachReadyMsg{pod: pod}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), tickCmd())
}

// selectedBox returns the sandbox under the cursor, or nil.
func (m model) selectedBox() *cluster.Sandbox {
	if m.selected < 0 || m.selected >= len(m.boxes) {
		return nil
	}
	return &m.boxes[m.selected]
}

// moveSelection clamps the cursor, so j/k and the wheel cannot leave
// the list.
func (m *model) moveSelection(delta int) {
	if len(m.boxes) == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected > len(m.boxes)-1 {
		m.selected = len(m.boxes) - 1
	}
}
