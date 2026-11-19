// The dashboard model. Navigation lives in the left pane (warm and
// claimed sandboxes as separate sections), the right column holds the
// header pane with the selection's data and the shell pane embedding
// its tmux session. The session map mirrors which pods have a live
// tmux session, so bash survives navigation and quitting.
package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

const (
	refreshEvery      = 2 * time.Second
	shellRefreshEvery = 200 * time.Millisecond
)

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

	// Pane state. leftWidth and leftHeight are drag overrides of the
	// default split; zero means default. dragging is armed by a press
	// on the splitter and cleared on release.
	leftWidth  int
	leftHeight int
	dragging   bool

	// Embedded shell. shellFocus routes keys to the tmux session
	// instead of the navigation. shellLines and shellPod hold the last
	// capture, shown only while it matches the selection. shellW and
	// shellH are the pane size last sent to tmux.
	shellFocus     bool
	shellLines     []string
	shellPod       string
	shellW, shellH int

	width  int
	height int
}

// New returns the model the program runs.
func New() tea.Model {
	return model{sessions: map[string]bool{}}
}

type tickMsg struct{}
type shellTickMsg struct{}

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

// attachReadyMsg: the tmux session exists, focus the embedded shell.
type attachReadyMsg struct {
	pod string
	err error
}

// fullReadyMsg: the tmux session exists, take the whole terminal.
type fullReadyMsg struct {
	pod string
	err error
}

type resumeMsg struct {
	err error
}

type shellMsg struct {
	pod   string
	lines []string
	err   error
}

type sendErrMsg struct {
	err error
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func shellTickCmd() tea.Cmd {
	return tea.Tick(shellRefreshEvery, func(time.Time) tea.Msg { return shellTickMsg{} })
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

// attachCmd ensures the tmux session exists off the render loop; the
// attachReadyMsg handler then focuses the embedded shell pane.
func attachCmd(pod string) tea.Cmd {
	return func() tea.Msg {
		if err := session.Ensure(pod); err != nil {
			return attachReadyMsg{pod: pod, err: err}
		}
		return attachReadyMsg{pod: pod}
	}
}

// fullCmd ensures the session, then the fullReadyMsg handler takes the
// whole terminal for full-screen programs.
func fullCmd(pod string) tea.Cmd {
	return func() tea.Msg {
		if err := session.Ensure(pod); err != nil {
			return fullReadyMsg{pod: pod, err: err}
		}
		return fullReadyMsg{pod: pod}
	}
}

// captureCmd reads the session's pane, resizing it first when the shell
// pane changed size.
func captureCmd(pod string, w, h int, resize bool) tea.Cmd {
	return func() tea.Msg {
		if resize {
			_ = session.Resize(pod, w, h)
		}
		lines, err := session.Capture(pod)
		return shellMsg{pod: pod, lines: lines, err: err}
	}
}

// sendCmd delivers one keypress or a literal string to the session.
func sendCmd(pod, key, text string) tea.Cmd {
	return func() tea.Msg {
		var err error
		if text != "" {
			err = session.SendText(pod, text)
		} else {
			err = session.SendKey(pod, key)
		}
		if err != nil {
			return sendErrMsg{err: err}
		}
		return nil
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), tickCmd(), shellTickCmd())
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
