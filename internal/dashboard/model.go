package dashboard

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/session"
)

const (
	refreshEvery      = 2 * time.Second
	shellRefreshEvery = 200 * time.Millisecond
)

type focusPane int

const (
	focusNav focusPane = iota
	focusShell
	focusInspect
)

type inspectCache struct {
	pod   string
	width int
	rev   int
	lines []string
}

type usageHistory struct {
	cpu, mem []float64
}

type model struct {
	boxes          []cluster.Sandbox
	pool           cluster.Pool
	selected       string // pod name, stable across fleet resorts
	sessions       map[string]bool
	metricsOK      bool
	err            error
	loaded         bool
	status         string
	statusTTL      time.Duration // 0 = sticky until replaced
	confirmDestroy bool

	leftWidth  int
	leftHeight int
	dragging   bool

	focus          focusPane
	prefix         bool
	inspectScroll  int
	dataRev        int
	inspect        inspectCache
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

func New() tea.Model {
	return model{sessions: map[string]bool{}}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), tickCmd(), shellTickCmd())
}

type tickMsg struct{}
type shellTickMsg struct{}

type loadedMsg struct {
	boxes     []cluster.Sandbox
	pool      cluster.Pool
	sessions  map[string]bool
	metricsOK bool
	err       error
}

type claimMsg struct {
	name string
	err  error
}

type claimAdoptedMsg struct {
	pod string
	err error
}

type deletedMsg struct {
	claim string
	err   error
}

type attachReadyMsg struct {
	pod string
	err error
}

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
		var (
			boxes     []cluster.Sandbox
			pool      cluster.Pool
			metricsOK bool
			err       error
			sessions  map[string]bool
			wg        sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			boxes, pool, metricsOK, err = cluster.Fetch()
		}()
		go func() {
			defer wg.Done()
			sessions = session.Live()
		}()
		wg.Wait()
		return loadedMsg{boxes: boxes, pool: pool, sessions: sessions, metricsOK: metricsOK, err: err}
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
		return deletedMsg{claim: claim, err: cluster.DeleteClaim(claim)}
	}
}

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

func attachCmd(pod string) tea.Cmd {
	return func() tea.Msg {
		return attachReadyMsg{pod: pod, err: session.Ensure(pod)}
	}
}

func fullCmd(pod string) tea.Cmd {
	return func() tea.Msg {
		return fullReadyMsg{pod: pod, err: session.Ensure(pod)}
	}
}

func captureCmd(pod string, w, h int, resize bool) tea.Cmd {
	return func() tea.Msg {
		if resize {
			_ = session.Resize(pod, w, h)
		}
		lines, err := session.Capture(pod)
		return shellMsg{pod: pod, lines: lines, err: err}
	}
}

func drainCmd(pod string, keys []session.Key) tea.Cmd {
	return func() tea.Msg {
		return sentMsg{pod: pod, count: len(keys), err: session.SendBatch(pod, keys)}
	}
}

func (m *model) captureSelected(pod string) tea.Cmd {
	w, h := m.shellPaneSize()
	resize := w != m.shellW || h != m.shellH
	m.shellW, m.shellH = w, h
	return captureCmd(pod, w, h, resize)
}

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
		if len(h.cpu) > 120 {
			h.cpu = h.cpu[len(h.cpu)-120:]
			h.mem = h.mem[len(h.mem)-120:]
		}
		m.hist[b.Name] = h
	}
}

func (m *model) flash(msg string) {
	m.status = msg
	m.statusTTL = 5 * time.Second
}

func (m *model) sticky(msg string) {
	m.status = msg
	m.statusTTL = 0
}

func (m *model) expireStatus() {
	if m.statusTTL > 0 {
		m.statusTTL -= refreshEvery
		if m.statusTTL <= 0 {
			m.status, m.statusTTL = "", 0
		}
	}
}

func (m model) selectedBox() *cluster.Sandbox {
	for i := range m.boxes {
		if m.boxes[i].Name == m.selected {
			return &m.boxes[i]
		}
	}
	return nil
}

func (m model) selectedIndex() int {
	for i, b := range m.boxes {
		if b.Name == m.selected {
			return i
		}
	}
	return 0
}

func (m model) isWarmSelected() bool {
	b := m.selectedBox()
	return b != nil && b.OwnerKind != "SandboxClaim"
}

func (m model) canFocusShell() bool {
	b := m.selectedBox()
	return b != nil && b.OwnerKind == "SandboxClaim" && m.sessions[b.Name]
}

func (m *model) focusWarmInspect() {
	m.focus = focusInspect
	m.refreshInspectCache()
}

func (m *model) selectPod(name string) {
	if m.selected != name {
		m.selected = name
		m.inspectScroll = 0
	}
	if m.focus == focusShell && !m.canFocusShell() {
		m.focus = focusNav
		m.prefix = false
	}
	if m.focus == focusInspect && !m.isWarmSelected() {
		m.focus = focusNav
	}
	m.refreshInspectCache()
}

func (m *model) moveSelection(delta int) {
	if len(m.boxes) == 0 {
		return
	}
	i := clamp(m.selectedIndex()+delta, 0, len(m.boxes)-1)
	m.selectPod(m.boxes[i].Name)
}
