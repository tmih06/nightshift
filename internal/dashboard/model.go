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
	selected  string          // pod name under the cursor, stable across refreshes
	sessions  map[string]bool // pod name → live tmux session
	metricsOK bool
	err       error
	loaded    bool
	status    string
	statusTTL time.Duration // 0 means the message stays until replaced
	// confirmDestroy arms the x key so one stray press cannot tear a
	// claimed sandbox down. The second press fires.
	confirmDestroy bool

	// Pane state. leftWidth and leftHeight are drag overrides of the
	// default split; zero means default. dragging is armed by a press
	// on the splitter and cleared on release.
	leftWidth  int
	leftHeight int
	dragging   bool

	// Embedded shell. shellFocus routes every key to the tmux session
	// instead of the navigation: no keybind escapes. prefix holds a C-b
	// seen while focused, so only C-b followed by q leaves the shell.
	// shellLines and shellPod hold the last capture, shown only while it
	// matches the selection. shellW and shellH are the pane size last
	// sent to tmux. pending is the queued input, drained one batch at a
	// time so keystrokes keep their order; sending marks a drain in
	// flight. pendingPod is the pod of a claim whose bash is starting,
	// selected and focused as soon as the fleet shows it. hist keeps the
	// usage samples behind the header charts.
	shellFocus     bool
	prefix         bool
	shellLines     []string
	shellPod       string
	shellW, shellH int
	pending        []session.Key
	sending        bool
	pendingPod     string
	hist           map[string]usageHistory

	width  int
	height int
}

// usageHistory is the sampled cpu and memory ratios of one sandbox,
// the data behind the btop-style charts in the header.
type usageHistory struct {
	cpu, mem []float64
}

// appendHistory records the latest usage ratios, capped at a minute of
// samples at the refresh rate.
func (m *model) appendHistory() {
	if !m.metricsOK {
		return
	}
	if m.hist == nil {
		m.hist = map[string]usageHistory{}
	}
	for _, b := range m.boxes {
		if b.CPULimitMC <= 0 || b.MemLimitMiB <= 0 {
			continue
		}
		h := m.hist[b.Name]
		h.cpu = append(h.cpu, float64(b.CPUUsageMC)/float64(b.CPULimitMC))
		h.mem = append(h.mem, float64(b.MemUsageMiB)/float64(b.MemLimitMiB))
		// Two minutes of samples fill the widest braille graph the
		// header can show.
		if len(h.cpu) > 120 {
			h.cpu = h.cpu[len(h.cpu)-120:]
			h.mem = h.mem[len(h.mem)-120:]
		}
		m.hist[b.Name] = h
	}
}

// flash sets a transient status message that expires on its own, so
// notifications like "destroyed claim, pool is restocking" do not sit
// on screen forever.
func (m *model) flash(msg string) {
	m.status = msg
	m.statusTTL = 5 * time.Second
}

// sticky sets a status message that stays until replaced: errors and
// armed confirmations must not vanish while the user reads them.
func (m *model) sticky(msg string) {
	m.status = msg
	m.statusTTL = 0
}

// expireStatus drops an elapsed transient message, called on each
// update so the status line is honest without a timer goroutine.
func (m *model) expireStatus() {
	if m.statusTTL > 0 {
		m.statusTTL -= refreshEvery
		if m.statusTTL <= 0 {
			m.status, m.statusTTL = "", 0
		}
	}
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

// claimAdoptedMsg: the claim took a sandbox and its bash session exists.
type claimAdoptedMsg struct {
	pod string
	err error
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

// sentMsg reports a drained input batch, so the next one can start.
type sentMsg struct {
	pod   string
	count int
	err   error
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

// waitClaimCmd blocks until the claim adopts a sandbox, then creates
// its bash session, so the attach is immediate when the pod appears.
func waitClaimCmd(claim string) tea.Cmd {
	return func() tea.Msg {
		pod, err := cluster.WaitForClaim(claim, 30*time.Second)
		if err != nil {
			return claimAdoptedMsg{err: err}
		}
		if err := session.Ensure(pod); err != nil {
			return claimAdoptedMsg{err: err}
		}
		return claimAdoptedMsg{pod: pod}
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

// drainCmd delivers a batch of queued input in one serialized pass.
func drainCmd(pod string, keys []session.Key) tea.Cmd {
	return func() tea.Msg {
		err := session.SendBatch(pod, keys)
		return sentMsg{pod: pod, count: len(keys), err: err}
	}
}

// startDrain begins sending the queued input if no drain is in flight.
// Batches keep keystrokes in order.
func (m *model) startDrain() tea.Cmd {
	b := m.selectedBox()
	if m.sending || len(m.pending) == 0 || b == nil || !m.sessions[b.Name] {
		return nil
	}
	m.sending = true
	keys := m.pending
	m.pending = nil
	return drainCmd(b.Name, keys)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), tickCmd(), shellTickCmd())
}

// selectedBox returns the sandbox under the cursor, or nil. The
// selection is a pod name, not an index, because the fleet is resorted
// on every refresh and positions move when the pool refills.
func (m model) selectedBox() *cluster.Sandbox {
	for i := range m.boxes {
		if m.boxes[i].Name == m.selected {
			return &m.boxes[i]
		}
	}
	return nil
}

// selectedIndex maps the selection back to a position, 0 when the name
// is gone.
func (m model) selectedIndex() int {
	for i, b := range m.boxes {
		if b.Name == m.selected {
			return i
		}
	}
	return 0
}

// moveSelection clamps the cursor, so j/k and the wheel cannot leave
// the list.
func (m *model) moveSelection(delta int) {
	if len(m.boxes) == 0 {
		return
	}
	i := clamp(m.selectedIndex()+delta, 0, len(m.boxes)-1)
	m.selected = m.boxes[i].Name
}
