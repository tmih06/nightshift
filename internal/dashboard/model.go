package dashboard

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/cluster"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
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

// liveUsage is a claimed sandbox's usage from its own cgroup counters:
// base is the sample the next CPU rate is measured from.
type liveUsage struct {
	base  cluster.CgroupSample
	cpu   int64
	cpuOK bool
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
	statusBad      bool
	confirmDestroy bool

	section     int
	sideWidth   int // 0 picks half the width
	tableHeight int // stacked layout only; 0 picks 40%
	dragging    bool
	showHelp    bool
	updatedAt   time.Time
	rowCache    *rowCacheEntry

	focus          focusPane
	prefix         bool
	inspectScroll  int
	dataRev        int
	inspect        inspectCache
	inspectDocs    map[string]inspectCache // shared by every copy of the model
	shellLines     []string
	shellPod       string
	shellW, shellH int
	pending        []session.Key
	sending        bool
	pendingPod     string
	hist           map[string]usageHistory
	live           map[string]liveUsage

	// hidden is set while another tab is on screen, so the shell stops
	// polling Herdr for a pane nobody sees.
	hidden bool

	width  int
	height int
}

// Model is the sandbox tab: the fleet tree, the bento header, and the
// embedded shell or warm-pool inspector.
type Model = model

// New returns the sandbox tab painted from gh-dash's theme t.
func New(t ghtheme.Theme) Model {
	useTheme(t)
	return model{sessions: map[string]bool{}, rowCache: &rowCacheEntry{}, inspectDocs: map[string]inspectCache{}}
}

// SetHidden tells the tab whether another tab covers it.
func (m model) SetHidden(hidden bool) Model {
	m.hidden = hidden
	if !hidden {
		m.shellW, m.shellH = 0, 0
	}
	return m
}

// Capturing is whether every key belongs to the embedded shell, so the host
// must not take tab or any other key for itself.
func (m model) Capturing() bool { return m.focus == focusShell && m.canFocusShell() }

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
	cgroups   map[string]cluster.CgroupSample
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

// Owns is whether msg is one of the sandbox tab's own, so a host need not
// offer it to anything else. The shell refresh sends several a second.
func Owns(msg tea.Msg) bool {
	switch msg.(type) {
	case tickMsg, shellTickMsg, loadedMsg, claimMsg, claimAdoptedMsg, deletedMsg,
		attachReadyMsg, fullReadyMsg, resumeMsg, shellMsg, sentMsg:
		return true
	}
	return false
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func shellTickCmd() tea.Cmd {
	return tea.Tick(shellRefreshEvery, func(time.Time) tea.Msg { return shellTickMsg{} })
}

func fetchCmd() tea.Cmd {
	return func() tea.Msg {
		boxes, pool, metricsOK, err := cluster.Fetch()
		var claimed []string
		for _, b := range boxes {
			if b.OwnerKind == "SandboxClaim" && b.Phase == "Running" {
				claimed = append(claimed, b.Name)
			}
		}
		var (
			sessions map[string]bool
			cgroups  map[string]cluster.CgroupSample
			wg       sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); sessions = session.Live(boxes) }()
		go func() { defer wg.Done(); cgroups = cluster.ReadCgroups(claimed) }()
		wg.Wait()
		return loadedMsg{boxes: boxes, pool: pool, sessions: sessions, metricsOK: metricsOK, cgroups: cgroups, err: err}
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

// applyLive puts claimed sandboxes' own counters over metrics-server's
// numbers. Memory is live from the first sample; CPU from the second, as it
// is a rate between two.
func (m *model) applyLive(samples map[string]cluster.CgroupSample) {
	next := make(map[string]liveUsage, len(samples))
	for i := range m.boxes {
		b := &m.boxes[i]
		s, ok := samples[b.Name]
		if !ok {
			continue
		}
		st, seen := m.live[b.Name]
		switch {
		case !seen:
			st = liveUsage{base: s}
		case s.Uptime < st.base.Uptime || s.CPUUsec < st.base.CPUUsec:
			st = liveUsage{base: s} // restarted: start over
		default:
			if mc, ok := cluster.CPURate(st.base, s); ok {
				st.cpu, st.cpuOK, st.base = mc, true, s
			}
		}
		b.MemUsageMiB = s.MemMiB()
		if st.cpuOK {
			b.CPUUsageMC = st.cpu
		}
		next[b.Name] = st
	}
	m.live = next
}

// usageKnown is whether b's numbers are real: metrics-server answered, or
// the sandbox's own counters did.
func (m model) usageKnown(name string) bool {
	return m.metricsOK || m.live[name].cpuOK
}

func (m *model) appendHistory() {
	if m.hist == nil {
		m.hist = map[string]usageHistory{}
	}
	for i := range m.boxes {
		b := &m.boxes[i]
		if !m.usageKnown(b.Name) || b.CPULimitMC <= 0 || b.MemLimitMiB <= 0 {
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
	m.statusBad = false
}

func (m *model) sticky(msg string) {
	m.status = msg
	m.statusTTL = 0
	m.statusBad = false
}

// fail is sticky and painted as an error.
func (m *model) fail(msg string) {
	m.sticky(msg)
	m.statusBad = true
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
	for position, index := range m.navigationIndices() {
		if m.boxes[index].Name == m.selected {
			return position
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
		m.shellW, m.shellH = 0, 0
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

// setSection shows section, keeping the selection when it is in it.
func (m *model) setSection(section int) {
	m.section = (section + len(sectionTitles)) % len(sectionTitles)
	order := m.navigationIndices()
	for _, i := range order {
		if m.boxes[i].Name == m.selected {
			return
		}
	}
	if len(order) > 0 {
		m.selectPod(m.boxes[order[0]].Name)
	}
}

func (m *model) moveSelection(delta int) {
	order := m.navigationIndices()
	if len(order) == 0 {
		return
	}
	current := 0
	for position, index := range order {
		if m.boxes[index].Name == m.selected {
			current = position
			break
		}
	}
	m.selectPod(m.boxes[order[clamp(current+delta, 0, len(order)-1)]].Name)
}
