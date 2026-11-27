package dashboard

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
	"github.com/tmih06/nightshift/internal/tui/dialog"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

const (
	tabsH        = 2 // gh-dash's section tabs and their rule
	footerH      = 1
	tableHeaderH = 2 // titles and a blank, as gh-dash heads its tables
	itemH        = 3 // two lines and a separator per pod
	infoRows     = 8
	stackedBelow = 90
	minTableW    = 56
	minSideW     = 36
	sparkW       = 5
)

// Glyphs from Nerd Fonts, which gh-dash already needs.
const (
	glyphSandbox = "\uf1b2" // fa-cube
	glyphShell   = "\uf120" // fa-terminal
	glyphClock   = "\uf017" // fa-clock-o
	glyphConfigs = "\uf15c" // fa-file-text
	glyphLogs    = "\uf1ea" // fa-newspaper-o
	glyphEgress  = "\uf132" // fa-shield
	glyphCore    = "\uf013" // fa-cog
	glyphJob     = "\uf085" // fa-cogs
)

// layout places gh-dash's chrome: section tabs on top, the pods table on the
// left, the preview sidebar on the right (below it when narrow), and the
// footer. docTop, docW, and docH are the sidebar's scrolling body: the shell,
// logs, or configs.
type layout struct {
	bodyTop, bodyH int
	tableW, tableH int
	sideX, sideW   int
	sideTop, sideH int
	docTop         int
	docW, docH     int
	stacked        bool
}

func (m model) infoRowCount() int {
	n := infoRows
	if b := m.selectedBox(); b != nil {
		if b.Claimed() && m.effectivePane() == paneShell {
			n++
		}
		n += startupLineCount(b.Startup)
	}
	return n
}

func (m model) layout() layout {
	l := layout{bodyTop: tabsH}
	l.bodyH = max(m.height-tabsH-m.footerHeight(), 4)
	rows := m.infoRowCount()
	if m.zoom {
		return m.zoomLayout(l, rows)
	}
	if m.width < stackedBelow {
		l.stacked = true
		l.tableW, l.sideW = m.width, m.width
		l.tableH = clamp(m.tableHeight, tableHeaderH+itemH, max(l.bodyH-rows-4, tableHeaderH+itemH))
		if m.tableHeight <= 0 {
			l.tableH = clamp(l.bodyH*2/5, tableHeaderH+itemH, max(l.bodyH-rows-4, tableHeaderH+itemH))
		}
		l.sideTop = l.bodyTop + l.tableH
		l.sideH = max(l.bodyH-l.tableH, 1)
		l.docTop = l.sideTop + 1 + rows
		l.docW = max(m.width-4, 1)
		l.docH = max(l.sideH-1-rows, 1)
		return l
	}
	side := m.sideWidth
	if side <= 0 {
		side = m.width / 2
	}
	l.sideW = clamp(side, minSideW, max(m.width-minTableW, minSideW))
	l.tableW = m.width - l.sideW
	l.tableH = l.bodyH
	l.sideX, l.sideTop, l.sideH = l.tableW, l.bodyTop, l.bodyH
	l.docTop = l.bodyTop + rows
	l.docW = max(l.sideW-4, 1)
	l.docH = max(l.bodyH-rows, 1)
	return l
}

func (m model) shellPaneSize() (int, int) {
	l := m.layout()
	return l.docW, l.docH
}

func cell(style lipgloss.Style, width int, text string, right bool) string {
	text = truncateANSI(text, max(width-2, 1))
	s := style.Width(width).MaxWidth(width)
	if right {
		s = s.Align(lipgloss.Right)
	}
	return s.Render(text)
}

func (m model) tableHeader(inner int) string {
	if m.rowCache != nil && m.rowCache.headerWidth == inner && m.rowCache.headerTheme == themeRevision && m.rowCache.header != "" {
		return m.rowCache.header
	}
	var b strings.Builder
	b.WriteByte(' ')
	for _, c := range tableColumns(inner) {
		b.WriteString(cell(styles.Table.TitleCellStyle, c.width, c.title, c.right))
	}
	header := b.String()
	if m.rowCache != nil {
		m.rowCache.headerWidth, m.rowCache.headerTheme, m.rowCache.header = inner, themeRevision, header
	}
	return header
}

type rowKey struct {
	rev   int
	width int
	theme uint64
}

type rowState struct {
	index    int
	selected bool
}

// rowCacheEntry is shared by every copy of the model, so a View on a value
// receiver can still fill it.
type rowCacheEntry struct {
	key         rowKey
	rows        map[rowState][]string
	header      string
	headerWidth int
	headerTheme uint64
}

// ---- sidebar ------------------------------------------------------------

func phaseColor(phase string) color.Color {
	switch phase {
	case fleet.PodPhaseRunning, "Succeeded":
		return th.SuccessText
	case "Pending", "":
		return th.WarningText
	default:
		return th.ErrorText
	}
}

type subTabSpan struct {
	from, to int
	mode     sidebarMode
}

func (m model) subTabRow() int {
	if m.selectedBox() == nil {
		return -1
	}
	return m.layout().docTop - 1
}

func (m model) subTabSpans() []subTabSpan {
	l := m.layout()
	inner := l.sideW - 1
	startX := 1
	if !l.stacked {
		startX = l.sideX + 2
	}
	_, spans := m.subTabBar(inner-1, startX)
	return spans
}

func subTabLabel(mode sidebarMode) string {
	switch mode {
	case paneShell:
		return glyphShell + " Shell"
	case paneLogs:
		return glyphLogs + " Logs"
	default:
		return glyphConfigs + " Configs"
	}
}

func (m model) subTabBar(width, startX int) (string, []subTabSpan) {
	b := m.selectedBox()
	panes := availablePanes(b)
	if len(panes) == 0 {
		return "", nil
	}
	active := m.effectivePane()
	focused := (active == paneShell && m.focus == focusShell) || (active != paneShell && m.focus == focusInspect)

	var left strings.Builder
	spans := make([]subTabSpan, 0, len(panes))
	x := startX
	for i, mode := range panes {
		if i > 0 {
			sep := "  "
			left.WriteString(sep)
			x += len(sep)
		}
		label := subTabLabel(mode)
		rendered := subTabTextStyle(mode == active, focused).Render(label)
		w := ansi.StringWidth(rendered)
		left.WriteString(rendered)
		spans = append(spans, subTabSpan{from: x, to: x + w, mode: mode})
		x += w
	}

	hint := m.sidebarFocusHint(b, active, focused)
	return spread(left.String(), dim(hint), width), spans
}

func (m model) sidebarFocusHint(b *fleet.Sandbox, active sidebarMode, focused bool) string {
	if focused {
		if active == paneShell {
			return "C-b q leaves"
		}
		return "esc leaves"
	}
	if active == paneShell && len(m.podWindows(b.Name)) == 0 {
		return ""
	}
	return "⏎ or click to focus"
}

func (m model) podContextLine(b *fleet.Sandbox, width int) string {
	if b.Claimed() && b.Volume != "" {
		return truncateANSI(dim("volume ")+b.Volume+dim(" · kept when destroyed"), max(width, 1))
	}
	parts := make([]string, 0, 3)
	if b.Image != "" {
		parts = append(parts, dim("image ")+imageLabel(b.Image, b.AgentVersion))
	}
	if len(b.Ports) > 0 {
		parts = append(parts, dim("ports ")+strings.Join(b.Ports, ", "))
	}
	if len(parts) == 0 {
		return dim("spec unavailable")
	}
	return truncateANSI(strings.Join(parts, dim(" · ")), max(width, 1))
}

func (m model) podMetaLine(b *fleet.Sandbox, width int) string {
	var parts []string
	if b.Warm() && len(m.podWindows(b.Name)) > 0 {
		parts = append(parts, warmStyle.Render("● herdr live"))
	}
	if b.CPUReqMC > 0 || b.CPULimitMC > 0 {
		parts = append(parts, dim("cpu ")+fmt.Sprintf("%d/%dm", b.CPUReqMC, b.CPULimitMC))
	}
	if b.MemReqMiB > 0 || b.MemLimitMiB > 0 {
		parts = append(parts, dim("mem ")+fmt.Sprintf("%dMi/%dMi", b.MemReqMiB, b.MemLimitMiB))
	}
	if b.RestartPolicy != "" {
		parts = append(parts, dim("restart ")+b.RestartPolicy)
	}
	if b.NodeName != "" {
		parts = append(parts, dim("node ")+b.NodeName)
	}
	if len(parts) == 0 {
		return ""
	}
	return fitParts(parts, width)
}

func (m model) podStateLine(b *fleet.Sandbox, width int) string {
	kind, kindColor := kindName(b)
	pill := styles.PrView.PillStyle.Background(kindColor).Render(" " + roleGlyph(b.PodRole()) + " " + kind + " ")
	phaseText := b.Phase
	phaseCol := phaseColor(b.Phase)
	if b.Waiting != "" {
		phaseText = b.Waiting
		phaseCol = warnColor
	}
	status := lipgloss.NewStyle().Foreground(phaseCol).Render("● " + phaseText)
	if b.Ready != "" {
		status += " " + dim(b.Ready)
	}
	if b.Restarts > 0 {
		status += " " + warnStyle.Render(fmt.Sprintf("%d↺", b.Restarts))
	}
	var extra []string
	if b.PodIP != "" {
		extra = append(extra, b.PodIP)
	} else {
		node := b.NodeName
		if node == "" {
			node = podUnassigned
		}
		extra = append(extra, dim("node ")+node)
	}
	extra = append(extra, dim(ageString(b.Created)))
	return truncateANSI(pill+" "+status+dim(" · ")+strings.Join(extra, dim(" · ")), max(width, 1))
}

func podHeaderLocation(b *fleet.Sandbox) string {
	ns := b.EffectiveNamespace()
	if b.Owner != "" {
		return ns + " · " + b.Owner
	}
	return ns
}

func (m model) claimedSessionLines(b *fleet.Sandbox, width int) []string {
	wins := m.podWindows(b.Name)
	session := dim("○ herdr off")
	if len(wins) > 0 {
		session = warmStyle.Render("● herdr live")
		if len(wins) > 1 {
			session += dim(fmt.Sprintf(" · %d windows", len(wins)))
		}
	}
	bar, _ := m.windowTabBar(width-1, 0)
	if bar != "" {
		bar = " " + bar
	}
	return []string{" " + session, bar}
}

// infoLines is the head of the preview sidebar: location bar, pod name,
// role/health pill, live CPU/MEM usage table, spec/context, optional Herdr
// window bar (for Claimed sandboxes on Shell), and clickable sidebar tabs.
func (m model) infoLines(width int) []string {
	b := m.selectedBox()
	if b == nil {
		return []string{common.RenderPreviewHeader(th, width, truncateANSI(fleet.Namespace, max(width-2, 1)))}
	}
	header := common.RenderPreviewHeader(th, width, truncateANSI(podHeaderLocation(b), max(width-2, 1)))
	title := strings.Split(common.RenderPreviewTitle(th, styles.Common, width, truncateANSI(b.Name, max(width-2, 1))), "\n")

	h := m.hist[b.Name]
	usage := usageTable([]usageRow{
		{colCPU, b.CPUUsageMC, b.CPULimitMC, "m", h.cpu},
		{colMEM, b.MemUsageMiB, b.MemLimitMiB, "Mi", h.mem},
	}, b.UsageKnown, width-1)

	lines := []string{
		header,
		title[0],
		" " + m.podStateLine(b, width-1),
	}
	if b.Startup != nil {
		for _, sl := range startupInfoLines(b.Startup, time.Now(), width-1) {
			lines = append(lines, " "+sl)
		}
	}
	lines = append(lines,
		" "+usage[0],
		" "+usage[1],
		" "+m.podContextLine(b, width-1),
	)

	if b.Claimed() && m.effectivePane() == paneShell {
		lines = append(lines, m.claimedSessionLines(b, width)...)
	} else {
		lines = append(lines, " "+m.podMetaLine(b, width-1))
	}

	subBar, _ := m.subTabBar(width, 0)
	return append(lines, " "+subBar)
}

type windowAction int

const (
	winActionSelect windowAction = iota
	winActionNew
	winActionClose
)

type windowTabSpan struct {
	from, to int
	action   windowAction
	address  string
	index    int
}

// windowTabRow returns the frame row of the claimed sandbox's window tab bar,
// directly below the "● herdr live" line, or -1 if none is shown.
func (m model) windowTabRow() int {
	b := m.selectedBox()
	if b == nil || !b.Claimed() || m.effectivePane() != paneShell {
		return -1
	}
	l := m.layout()
	// infoLines ends with [..., session, windowTabBar, subTabBar], so the tab bar
	// sits two rows above docTop.
	return l.docTop - 2
}

// windowTabSpans gives screen-X clickable hit boxes for the window tab bar.
func (m model) windowTabSpans() []windowTabSpan {
	l := m.layout()
	inner := l.sideW - 1
	startX := 1 // stacked: leading " " inside infoLines
	if !l.stacked {
		startX = l.sideX + 2 // side-by-side: "│" border + leading " "
	}
	_, spans := m.windowTabBar(inner-1, startX)
	return spans
}

func windowStatusPrefix(status string) string {
	switch status {
	case "working":
		return warnStyle.Render("●") + " "
	case "blocked":
		return warnStyle.Render("◆") + " "
	case "error":
		return badStyle.Render("●") + " "
	case "done":
		return warmStyle.Render("✓") + " "
	default:
		return ""
	}
}

// windowTabBar renders the Herdr-style window tabs and + / × controls for the
// selected claimed sandbox, returning the styled line and hit boxes starting
// at startX.
func (m model) windowTabBar(width, startX int) (string, []windowTabSpan) {
	b := m.selectedBox()
	if b == nil || !b.Claimed() || width < 1 {
		return "", nil
	}
	wins := m.podWindows(b.Name)
	if len(wins) == 0 {
		if b.Phase != "Running" {
			return "", nil
		}
		btn := winActionStyle.Render(" + new window ")
		w := ansi.StringWidth(btn)
		return truncateANSI(btn, width), []windowTabSpan{{
			from: startX, to: startX + min(w, width), action: winActionNew,
		}}
	}

	activeIdx := m.activeWindowIndex(b.Name)
	type tabItem struct {
		rendered string
		width    int
		address  string
		index    int
	}
	items := make([]tabItem, len(wins))
	for i, w := range wins {
		label := truncateANSI(w.Label, 14)
		text := fmt.Sprintf(" %d:%s ", i+1, label)
		var rendered string
		switch {
		case i == activeIdx && m.focus == focusShell:
			rendered = winFocusedStyle.Render(text)
		case i == activeIdx:
			rendered = winActiveStyle.Render(text)
		default:
			if prefix := windowStatusPrefix(w.Status); prefix != "" {
				rendered = " " + prefix + winInactiveStyle.Render(fmt.Sprintf("%d:%s ", i+1, label))
			} else {
				rendered = winInactiveStyle.Render(text)
			}
		}
		items[i] = tabItem{
			rendered: rendered,
			width:    ansi.StringWidth(rendered),
			address:  w.Address,
			index:    i,
		}
	}

	addBtn := winActionStyle.Render(" + ")
	closeBtn := winCloseStyle.Render(" × ")
	addW := ansi.StringWidth(addBtn)
	closeW := ansi.StringWidth(closeBtn)
	controlsW := 1 + addW + closeW // leading space + " + " + " × "

	// Fit as many tabs around activeIdx as width allows.
	lo, hi := activeIdx, activeIdx
	avail := max(width-controlsW, 1)
	used := items[activeIdx].width
	for {
		expanded := false
		if hi+1 < len(items) {
			need := 1 + items[hi+1].width
			reserve := 0
			if lo > 0 {
				reserve += 2
			}
			if hi+2 < len(items) {
				reserve += 2
			}
			if used+need+reserve <= avail {
				hi++
				used += need
				expanded = true
			}
		}
		if lo > 0 {
			need := 1 + items[lo-1].width
			reserve := 0
			if lo-1 > 0 {
				reserve += 2
			}
			if hi+1 < len(items) {
				reserve += 2
			}
			if used+need+reserve <= avail {
				lo--
				used += need
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}

	var out strings.Builder
	var spans []windowTabSpan
	x := startX
	if lo > 0 {
		out.WriteString(dim("‹ "))
		x += 2
	}
	for i := lo; i <= hi; i++ {
		if i > lo {
			out.WriteByte(' ')
			x++
		}
		out.WriteString(items[i].rendered)
		spans = append(spans, windowTabSpan{
			from:    x,
			to:      x + items[i].width,
			action:  winActionSelect,
			address: items[i].address,
			index:   items[i].index,
		})
		x += items[i].width
	}
	if hi+1 < len(items) {
		out.WriteString(dim(" ›"))
		x += 2
	}
	out.WriteByte(' ')
	x++
	out.WriteString(addBtn)
	spans = append(spans, windowTabSpan{from: x, to: x + addW, action: winActionNew})
	x += addW
	out.WriteString(closeBtn)
	spans = append(spans, windowTabSpan{from: x, to: x + closeW, action: winActionClose})

	maxX := startX + width
	clamped := spans[:0]
	for _, s := range spans {
		if s.from >= maxX {
			continue
		}
		if s.to > maxX {
			s.to = maxX
		}
		clamped = append(clamped, s)
	}
	return truncateANSI(out.String(), width), clamped
}

type usageRow struct {
	label        string
	usage, limit int64
	unit         string
	hist         []float64
}

// usageTable lays metrics out as fixed columns (label, used/limit,
// history, percent) so nothing shifts when a number changes width.
func usageTable(rows []usageRow, known bool, width int) []string {
	usedW, limitW := 0, 0
	for _, r := range rows {
		limitW = max(limitW, len(fmt.Sprintf("%d%s", r.limit, r.unit)))
	}
	usedW = max(limitW, 2) // usage rarely outgrows its limit's digits
	const labelW, pctW = 3, 5
	graphW := clamp(width-labelW-1-usedW-1-limitW-2-1-pctW, 0, 20)
	out := make([]string, len(rows))
	for i, r := range rows {
		if r.limit <= 0 {
			if known {
				out[i] = dim(fmt.Sprintf("%-*s ", labelW, r.label)) + formatQuantity(r.usage, r.unit) + dim(" (no limit)")
			} else {
				out[i] = dim(r.label + " no limit")
			}
			continue
		}
		used, ratio := "--", 0.0
		if known {
			used = fmt.Sprintf("%d%s", r.usage, r.unit)
			ratio = float64(r.usage) / float64(r.limit)
		}
		pct := lipgloss.NewStyle().Foreground(levelColor(ratio)).Render(fmt.Sprintf("%*.0f%%", pctW-1, ratio*100))
		line := dim(fmt.Sprintf("%-*s ", labelW, r.label)) +
			fmt.Sprintf("%*s", usedW, used) + dim(fmt.Sprintf("/%-*s", limitW, fmt.Sprintf("%d%s", r.limit, r.unit))) + "  "
		if graphW > 0 {
			line += usageGraph(r.hist, graphW) + " "
		}
		out[i] = truncateANSI(line+pct, max(width, 1))
	}
	return out
}

// imageLabel shows the tag a pod actually runs: with the CLI version
// probed inside it, :latest resolves to :<semver>; other answers append.
func imageLabel(image, agentVersion string) string {
	if v := agentSemver(agentVersion); v != "" {
		if strings.HasSuffix(image, ":latest") {
			return strings.TrimSuffix(image, "latest") + v
		}
		if !strings.HasSuffix(image, ":"+v) {
			return image + " · " + v
		}
		return image
	}
	if agentVersion != "" {
		return image + " · " + agentVersion
	}
	return image
}

var semverPattern = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// agentSemver extracts the first x.y.z from a CLI's --version answer, so
// outputs like "codex-cli 0.171.0" still name the version.
func agentSemver(raw string) string {
	return semverPattern.FindString(raw)
}

// fitParts joins as many leading parts as fit in room, dot-separated.
func fitParts(parts []string, room int) string {
	sep := dim(" · ")
	out := ""
	for _, p := range parts {
		next := p
		if out != "" {
			next = out + sep + p
		}
		if lipgloss.Width(next) > room-2 {
			break
		}
		out = next
	}
	return out
}

func (m model) docLines(l layout) []string {
	if m.showingDocPane() {
		return m.docInspectView(l)
	}
	return m.shellLinesView(l)
}

func (m model) sidebarView(l layout) []string {
	inner := l.sideW - 1
	lines := m.infoLines(inner)
	for _, d := range m.docLines(l) {
		lines = append(lines, "  "+d)
	}
	border := lipgloss.NewStyle().Foreground(th.PrimaryBorder)
	if m.focus == focusShell || m.focus == focusInspect {
		border = lipgloss.NewStyle().Foreground(th.SecondaryBorder)
	}
	out := make([]string, 0, l.sideH)
	if l.stacked {
		out = append(out, border.Render(strings.Repeat("━", l.sideW)))
		for i := 0; i < l.sideH-1; i++ {
			line := ""
			if i < len(lines) {
				line = lines[i]
			}
			out = append(out, padTo(line, l.sideW))
		}
		return out
	}
	edge := border.Render("│")
	for i := 0; i < l.sideH; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, edge+padTo(line, inner))
	}
	return out
}

func (m model) shellLinesView(l layout) []string {
	b := m.selectedBox()
	switch {
	case b == nil:
		return nil
	case !m.hasSession(b.Name):
		return []string{dim("no session — claim with c, or enter on a claimed sandbox")}
	case m.shellPod != b.Name || (m.shellTerm != "" && m.shellTerm != m.activeTerm(b.Name)):
		return nil
	}
	screen := m.shellScreen(l)
	s := m.shownSelection(screen)
	if s == nil {
		return screen
	}
	if s.dragging {
		screen = s.lines
	}
	lines := make([]string, len(screen))
	for row, ln := range screen {
		lines[row] = s.highlight(ln, row)
	}
	return lines
}

// ---- footer -------------------------------------------------------------

var (
	keyMove       = key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move"))
	keySection    = key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "section"))
	keyOpen       = key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "open"))
	keyClaim      = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "claim"))
	keyDestroy    = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "destroy"))
	keyFull       = key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "fullscreen"))
	keyRefresh    = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))
	keyTabs       = key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("⇥", "tabs"))
	keyQuit       = key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))
	keyScroll     = key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "scroll"))
	keyPage       = key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page"))
	keyBack       = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))
	keyLeaveShell = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("C-b q", "leaves bash"))
)

func viewKeyBinding() key.Binding {
	return key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view"))
}

func logsKeyBinding() key.Binding {
	return key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "logs"))
}

func (m model) hints() []key.Binding {
	if m.focus == focusInspect {
		return []key.Binding{keyScroll, keyPage, viewKeyBinding(), keyBack, keyClaim, keyTabs, keyQuit, keyHelp}
	}
	return []key.Binding{keyMove, keySection, keyOpen, viewKeyBinding(), keyClaim, keyDestroy, keyFull, keyRefresh, keyTabs, keyQuit, keyHelp}
}

// renderHints draws key hints on the footer's background, dropping from the
// end, keeping the last, until they fit room.
func renderHints(hints []key.Binding, room int) string {
	bgc := th.SelectedBackground
	keyS := lipgloss.NewStyle().Background(bgc).Foreground(th.PrimaryText)
	descS := lipgloss.NewStyle().Background(bgc).Foreground(th.FaintText)
	sepS := lipgloss.NewStyle().Background(bgc).Foreground(th.SecondaryBorder)
	sep := sepS.Render(" • ")
	sepW := lipgloss.Width(sep)
	parts := make([]string, len(hints))
	width := -sepW
	for i, h := range hints {
		parts[i] = keyS.Render(h.Help().Key) + descS.Render(" "+h.Help().Desc)
		width += sepW + lipgloss.Width(parts[i])
	}
	for len(parts) > 1 && width > room {
		last := len(parts) - 1
		width -= sepW + lipgloss.Width(parts[last-1])
		parts = append(parts[:last-1], parts[last])
	}
	return strings.Join(parts, sep)
}

func (m model) statusMessage() string {
	bgc := lipgloss.NewStyle().Background(th.SelectedBackground)
	switch {
	case m.err != nil:
		return bgc.Foreground(badColor).Render(styles.Common.FailureGlyph + " fetch error: " + m.err.Error())
	case m.status != "" && m.statusBad:
		return bgc.Foreground(badColor).Render(m.status)
	case m.status != "" && m.statusTTL == 0:
		return bgc.Foreground(warnColor).Render(m.status)
	case m.status != "":
		return bgc.Foreground(th.PrimaryText).Render(m.status)
	case !m.metricsOK && m.loaded:
		return bgc.Foreground(warnColor).Render("live usage unavailable (metrics-server)")
	}
	return ""
}

func (m model) pagerText() string {
	order := m.navigationIndices()
	pos := 0
	for i, index := range order {
		if i == 0 || m.boxes[index].Name == m.selected {
			pos = i + 1
		}
	}
	updated := "never"
	if !m.updatedAt.IsZero() {
		updated = "~" + ageString(m.updatedAt) + " ago"
	}
	return fmt.Sprintf("%s Updated %s • Pod %d/%d", glyphClock, updated, pos, len(order))
}

func (m model) statusLine() string {
	bgc := lipgloss.NewStyle().Background(th.SelectedBackground)
	width := max(m.width, 1)
	var left string
	switch {
	case m.focus == focusShell && m.prefix:
		left = prefixBarStyle.Render(" C-b armed · c new · n/p/1-9 switch · x close · F full · q leave ")
	case m.focus == focusShell:
		left = shellBarStyle.Render(" BASH ") + bgc.Render(" ") +
			renderHints([]key.Binding{keyLeaveShell}, width) +
			bgc.Foreground(th.FaintText).Render(" · C-b c new · n/p/1-9 switch · x close")
	}
	right := m.statusMessage()
	if right == "" {
		right = bgc.Foreground(th.FaintText).Render(m.pagerText())
	}
	right += bgc.Render(" ")
	if left == "" {
		left = renderHints(m.hints(), width-lipgloss.Width(right)-3)
	}
	left = bgc.Render(" ") + left
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncateANSI(left+bgc.Render(" ")+right, width)
	}
	return left + bgc.Render(strings.Repeat(" ", gap)) + right
}

// ---- frame --------------------------------------------------------------

// Count is the number of sandboxes, for the tab bar.
func (m model) Count() int { return len(m.boxes) }

// View renders the tab at exactly its width and height.
func (m model) View() string {
	if !m.loaded && m.err == nil {
		return fill(dim(" loading pods…"), m.width, m.height)
	}
	l := m.layout()
	var b strings.Builder
	b.WriteString(m.tabsView() + "\n")
	side := m.sidebarView(l)
	if m.zoom {
		for _, row := range side {
			b.WriteString(row + "\n")
		}
		b.WriteString(m.statusLine())
		return m.overlays(b.String())
	}
	table := m.tableView(l)
	if l.stacked {
		for _, row := range append(table, side...) {
			b.WriteString(row + "\n")
		}
	} else {
		for i := range table {
			b.WriteString(table[i])
			if i < len(side) {
				b.WriteString(side[i])
			}
			b.WriteString("\n")
		}
	}
	b.WriteString(m.statusLine())
	if m.showHelp {
		b.WriteString("\n" + m.helpView())
	}
	return m.overlays(b.String())
}

// overlays draws the checkout card in the bottom right, over the sidebar,
// and the destroy dialog over everything.
func (m model) overlays(view string) string {
	if m.checkout != nil {
		card := m.checkoutCard(time.Now())
		x := max(0, m.width-lipgloss.Width(card)-1)
		y := max(0, m.height-m.footerHeight()-lipgloss.Height(card))
		view = dialog.Overlay(view, card, x, y, m.height)
	}
	if m.destroy != nil {
		p := m.destroyPlaced()
		view = dialog.Overlay(view, p.Box, p.X, p.Y, m.height)
	}
	return view
}

// ---- helpers ------------------------------------------------------------

func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		return truncateANSI(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func padTo(s string, width int) string {
	if width > 0 && !strings.Contains(s, "\n") {
		if w := ansi.StringWidth(s); w <= width {
			return s + strings.Repeat(" ", width-w)
		}
	}
	s = truncateANSI(s, width)
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

// usageGraph draws a 1-row braille history chart (2 samples per cell,
// 4 dot rows scaled against the limit) batching same-color runs.
func usageGraph(hist []float64, width int) string {
	if width < 1 {
		return ""
	}
	if len(hist) == 0 {
		return dim(strings.Repeat("\u00b7", width))
	}
	vals := hist
	if dotCols := 2 * width; len(vals) > dotCols {
		vals = vals[len(vals)-dotCols:]
	}
	const dotRows = 4
	firstCell := width - (len(vals)+1)/2
	type graphCell struct {
		mask rune
		maxV float64
	}
	cells := make([]graphCell, width)
	for j, v := range vals {
		h := clamp(int(v*dotRows+0.5), 1, dotRows)
		c := firstCell + j/2
		col := j % 2
		for top := dotRows - h; top < dotRows; top++ {
			cells[c].mask |= dotBit(col, top)
		}
		if v > cells[c].maxV {
			cells[c].maxV = v
		}
	}

	var out strings.Builder
	if firstCell > 0 {
		out.WriteString(strings.Repeat(" ", firstCell))
	}
	var run strings.Builder
	curBucket := -1
	flush := func() {
		if run.Len() > 0 {
			out.WriteString(levelStyles[curBucket].Render(run.String()))
			run.Reset()
		}
	}
	for c := firstCell; c < width; c++ {
		bucket := levelBucket(cells[c].maxV)
		if bucket != curBucket {
			flush()
			curBucket = bucket
		}
		run.WriteRune(0x2800 + cells[c].mask)
	}
	flush()
	return out.String()
}

func levelBucket(ratio float64) int {
	switch {
	case ratio >= 0.9:
		return 3
	case ratio >= 0.6:
		return 2
	case ratio >= 0.3:
		return 1
	default:
		return 0
	}
}

func levelColor(ratio float64) color.Color {
	switch levelBucket(ratio) {
	case 3:
		return badColor
	case 2:
		return warnColor
	case 1:
		return midGreenColor
	default:
		return warmColor
	}
}

// dotBit maps (col 0..1, row 0..3) to its Unicode braille bit: dots 1-3
// run down col 0, dots 4-6 down col 1, and dots 7-8 sit on row 3.
func dotBit(col, row int) rune {
	if row < 3 {
		return 1 << rune(col*3+row)
	}
	return 1 << rune(6+col)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}

func ageString(ts time.Time) string {
	if ts.IsZero() {
		return "…"
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}

// fill pads s to a width by height block so the frame never leaves stale cells.
func fill(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, padTo(line, width))
	}
	return strings.Join(out, "\n")
}
