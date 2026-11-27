package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/api/client"
	v1 "github.com/tmih06/nightshift/api/v1"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/tui/fleet"
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

type sidebarMode int

const (
	paneAuto sidebarMode = iota
	paneShell
	paneLogs
	paneConfigs
)

type inspectContent struct {
	text, logErr    string
	loaded, loading bool
	role            fleet.PodRole
}

type inspectKey struct {
	pod  string
	mode sidebarMode
}

type inspectCache struct {
	content inspectContent
	width   int
	theme   uint64
	rev     int // Configs content need only be rebuilt after a fleet refresh
	lines   []string
}

type usageHistory struct {
	cpu, mem []float64
}

type model struct {
	api            API
	boxes          []fleet.Sandbox
	pools          map[string]fleet.Pool
	selected       string // pod name, stable across fleet resorts
	sessions       map[string]bool
	windows        map[string][]v1.Terminal
	activeWindow   map[string]string // pod -> active terminal address
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
	sidebarMode    sidebarMode
	inspectScroll  int
	logFollow      bool
	dataRev        int
	inspectDocs    map[inspectKey]inspectCache // shared by every copy of the model
	logs           map[string]string           // pod name -> raw container logs
	logErr         map[string]string           // pod name -> last log fetch error
	loadingLogs    string                      // pod currently fetching logs
	shellLines     []string
	shellPod       string
	shellTerm      string
	shellW, shellH int
	// selection is the mouse selection on the shell's screen, if any.
	selection *shellSelection
	// zoom gives the shell the whole window.
	zoom          bool
	watchingShell bool
	pending       []v1.Key
	pendingTerm   string
	sending       bool
	pendingPod    string
	// checkout is the pull request checkout in flight, shown as a card.
	checkout *checkoutRun
	// destroy, while open, asks whether a sandbox's volume goes with it.
	destroy *destroyDialog
	// pendingCheckout is the pull request checked out in pendingPod, whose
	// shell takes the keyboard once the fleet shows it.
	pendingCheckout *Checkout
	hist            map[string]usageHistory

	// hidden stops shell screen renders while another tab is on screen.
	hidden bool

	width  int
	height int
}

// Model is the sandbox tab: the fleet tree, the bento header, and the
// embedded shell or warm-pool inspector.
type Model = model

// New returns the pods tab painted from gh-dash's theme t.
func New(t ghtheme.Theme, api API) Model {
	useTheme(t)
	return model{
		api:          api,
		sessions:     map[string]bool{},
		windows:      map[string][]v1.Terminal{},
		activeWindow: map[string]string{},
		rowCache:     &rowCacheEntry{},
		inspectDocs:  map[inspectKey]inspectCache{},
		logs:         map[string]string{},
		logErr:       map[string]string{},
		logFollow:    true,
	}
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
func (m model) Capturing() bool {
	return m.destroy != nil || (m.focus == focusShell && m.canFocusShell())
}

// Boxes returns a shallow copy of the current sandbox fleet for read-only
// inspection.
func (m model) Boxes() []fleet.Sandbox {
	if len(m.boxes) == 0 {
		return nil
	}
	out := make([]fleet.Sandbox, len(m.boxes))
	copy(out, m.boxes)
	return out
}

// FocusPod selects pod (switching to All if the current section hides it) and
// attaches to its shell when it is claimed and running. It is a no-op if pod
// is not in the fleet.
func (m model) FocusPod(pod string) (Model, tea.Cmd) {
	var target *fleet.Sandbox
	for i := range m.boxes {
		if m.boxes[i].Name == pod {
			target = &m.boxes[i]
			break
		}
	}
	if target == nil {
		return m, nil
	}
	visible := false
	for _, idx := range m.navigationIndices() {
		if m.boxes[idx].Name == target.Name {
			visible = true
			break
		}
	}
	if !visible {
		m.section = sectionAll
	}
	m.confirmDestroy = false
	m.selectPod(target.Name)
	if target.Claimed() && target.Phase == fleet.PodPhaseRunning {
		m.sidebarMode = paneShell
		if m.hasSession(target.Name) {
			m.focus = focusShell
			m.prefix = false
			return m, m.watchShell()
		}
		return m, attachCmd(m.api, target.Name)
	}
	cmd := m.maybeFetchLogs()
	return m, cmd
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(m.api), tickCmd(), shellTickCmd())
}

type tickMsg struct{}
type shellTickMsg struct{}

type loadedMsg struct {
	boxes     []fleet.Sandbox
	pools     map[string]fleet.Pool
	sessions  map[string]bool
	windows   map[string][]v1.Terminal
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

// deletedMsg reports a destroyed claim, or a sandbox on a volume and
// whether the volume went too.
type deletedMsg struct {
	claim      string
	sandbox    string
	volume     string
	volumeGone bool
	err        error
}

type attachReadyMsg struct {
	pod   string
	terms []v1.Terminal
	err   error
}

type windowCreatedMsg struct {
	pod  string
	term v1.Terminal
	err  error
}

type windowKilledMsg struct {
	pod  string
	term string
	err  error
}

type shellMsg struct {
	pod   string
	term  string
	lines []string
	err   error
}

type sentMsg struct {
	pod   string
	term  string
	count int
	err   error
}

// Owns is whether msg is one of the sandbox tab's own, so a host need not
// offer it to anything else. The shell refresh sends several a second.
func Owns(msg tea.Msg) bool {
	switch msg.(type) {
	case tickMsg, shellTickMsg, loadedMsg, logsMsg, claimMsg, claimAdoptedMsg, deletedMsg,
		attachReadyMsg, windowCreatedMsg, windowKilledMsg,
		shellMsg, sentMsg, checkoutMsg:
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

func (m model) hasSession(pod string) bool {
	return len(m.windows[pod]) > 0 || m.sessions[pod]
}

// podWindows returns the live windows for pod. If only the boolean sessions
// map marks pod live (as in legacy unit tests), it synthesizes one bash window.
func (m model) podWindows(pod string) []v1.Terminal {
	if wins := m.windows[pod]; len(wins) > 0 {
		return wins
	}
	if m.sessions[pod] {
		return []v1.Terminal{{Address: defaultTerminal, Command: "bash"}}
	}
	return nil
}

// activeTerm returns the selected Herdr terminal address for pod, falling back
// to its first live window.
func (m model) activeTerm(pod string) string {
	wins := m.podWindows(pod)
	if len(wins) == 0 {
		return ""
	}
	if cur := m.activeWindow[pod]; cur != "" {
		for _, w := range wins {
			if w.Address == cur {
				return cur
			}
		}
	}
	return wins[0].Address
}

func (m model) activeWindowIndex(pod string) int {
	wins := m.podWindows(pod)
	cur := m.activeTerm(pod)
	for i, w := range wins {
		if w.Address == cur {
			return i
		}
	}
	return 0
}

func (m *model) selectWindow(pod, addr string) tea.Cmd {
	if m.activeWindow == nil {
		m.activeWindow = map[string]string{}
	}
	if m.activeWindow[pod] == addr && m.shellPod == pod && m.shellTerm == addr {
		return nil
	}
	m.activeWindow[pod] = addr
	if m.selected == pod {
		m.shellLines, m.shellPod, m.shellTerm = nil, "", ""
		m.shellW, m.shellH = 0, 0
		m.pending, m.pendingTerm = nil, ""
		return m.watchShell()
	}
	return nil
}

func (m *model) stepWindow(delta int) tea.Cmd {
	b := m.selectedBox()
	if b == nil {
		return nil
	}
	wins := m.podWindows(b.Name)
	if len(wins) <= 1 {
		return nil
	}
	idx := (m.activeWindowIndex(b.Name) + delta) % len(wins)
	if idx < 0 {
		idx += len(wins)
	}
	return m.selectWindow(b.Name, wins[idx].Address)
}

func (m *model) selectWindowIndex(idx int) tea.Cmd {
	b := m.selectedBox()
	if b == nil {
		return nil
	}
	wins := m.podWindows(b.Name)
	if idx < 0 || idx >= len(wins) {
		return nil
	}
	return m.selectWindow(b.Name, wins[idx].Address)
}

func (m *model) watchShell() tea.Cmd {
	b := m.selectedBox()
	if m.watchingShell || m.hidden || b == nil || !m.hasSession(b.Name) || b.Phase != "Running" {
		return nil
	}
	term := m.activeTerm(b.Name)
	w, h := m.shellPaneSize()
	resize := w != m.shellW || h != m.shellH || m.shellPod != b.Name || m.shellTerm != term
	m.shellW, m.shellH = w, h
	m.watchingShell = true
	return watchShellCmd(m.api, client.ScreenRequest{Sandbox: b.Name, Address: term, Cols: w, Rows: h, Resize: resize})
}

func (m *model) startDrain() tea.Cmd {
	b := m.selectedBox()
	if m.sending || len(m.pending) == 0 || b == nil || !m.hasSession(b.Name) {
		return nil
	}
	term := m.pendingTerm
	if term == "" {
		term = m.activeTerm(b.Name)
	}
	m.sending = true
	keys := m.pending
	m.pending = nil
	m.pendingTerm = ""
	return drainCmd(m.api, b.Name, term, keys)
}

func (m *model) appendHistory() {
	if m.hist == nil {
		m.hist = map[string]usageHistory{}
	}
	for i := range m.boxes {
		b := &m.boxes[i]
		if !b.UsageKnown || b.CPULimitMC <= 0 || b.MemLimitMiB <= 0 {
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

func (m model) selectedBox() *fleet.Sandbox {
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

func (m model) effectivePane() sidebarMode {
	b := m.selectedBox()
	if b == nil {
		return paneConfigs
	}
	mode := m.sidebarMode
	if mode == paneAuto {
		switch b.PodRole() {
		case fleet.RoleClaimed:
			return paneShell
		case fleet.RoleWarm:
			return paneConfigs
		default:
			return paneLogs
		}
	}
	if mode == paneShell && !b.Claimed() {
		if b.Warm() {
			return paneConfigs
		}
		return paneLogs
	}
	return mode
}

func availablePanes(b *fleet.Sandbox) []sidebarMode {
	if b == nil {
		return nil
	}
	switch b.PodRole() {
	case fleet.RoleClaimed:
		return []sidebarMode{paneShell, paneLogs, paneConfigs}
	case fleet.RoleWarm:
		return []sidebarMode{paneConfigs, paneLogs}
	default:
		return []sidebarMode{paneLogs, paneConfigs}
	}
}

func (m model) showingDocPane() bool {
	p := m.effectivePane()
	return p == paneConfigs || p == paneLogs
}

func (m model) canFocusShell() bool {
	b := m.selectedBox()
	return b != nil && b.Claimed() && m.effectivePane() == paneShell && m.hasSession(b.Name)
}

func (m *model) focusDocPane() {
	m.focus = focusInspect
}

func (m *model) syncFocusForMode(mode sidebarMode) {
	if mode != paneShell && m.focus == focusShell {
		m.focus = focusNav
		m.prefix = false
	}
	if mode == paneShell && m.focus == focusInspect {
		m.focus = focusNav
	}
}

func (m *model) setSidebarMode(mode sidebarMode) tea.Cmd {
	b := m.selectedBox()
	if b == nil || (mode == paneShell && !b.Claimed()) {
		return nil
	}
	if m.effectivePane() == mode && m.sidebarMode == mode {
		if mode == paneLogs {
			return m.maybeFetchLogs()
		}
		return nil
	}
	m.sidebarMode = mode
	m.inspectScroll = 0
	m.logFollow = true
	m.syncFocusForMode(mode)
	if mode == paneLogs {
		return m.maybeFetchLogs()
	}
	if mode == paneShell {
		m.shellW, m.shellH = 0, 0
		return m.watchShell()
	}
	return nil
}

func (m *model) cycleSidebarMode() tea.Cmd {
	b := m.selectedBox()
	panes := availablePanes(b)
	if len(panes) <= 1 {
		return nil
	}
	cur := m.effectivePane()
	for i, p := range panes {
		if p == cur {
			return m.setSidebarMode(panes[(i+1)%len(panes)])
		}
	}
	return m.setSidebarMode(panes[0])
}

func (m *model) maybeFetchLogs() tea.Cmd {
	if m.hidden {
		return nil
	}
	b := m.selectedBox()
	if b == nil || m.effectivePane() != paneLogs || m.loadingLogs == b.Name {
		return nil
	}
	m.loadingLogs = b.Name
	return fetchLogsCmd(m.api, b.EffectiveNamespace(), b.Name)
}

func (m *model) selectPod(name string) {
	if m.selected != name {
		m.selected = name
		m.sidebarMode = paneAuto
		m.inspectScroll = 0
		m.logFollow = true
		m.shellW, m.shellH = 0, 0
		m.shellTerm = ""
	}
	if m.focus == focusShell && !m.canFocusShell() {
		m.focus = focusNav
		m.prefix = false
	}
	if m.focus == focusInspect && !m.showingDocPane() {
		m.focus = focusNav
	}
}

// setSection shows section, keeping the selection when it is in it.
func (m *model) setSection(section int) {
	m.section = (section + sectionCount) % sectionCount
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
