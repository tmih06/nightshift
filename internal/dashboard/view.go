package dashboard

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
)

const (
	tabsH        = 2 // gh-dash's section tabs and their rule
	footerH      = 1
	tableHeaderH = 2 // titles and a blank, as gh-dash heads its tables
	itemH        = 3 // two lines and a separator per sandbox
	infoRows     = 8
	stackedBelow = 90
	minTableW    = 56
	minSideW     = 36
	sparkW       = 5
)

// Sections are gh-dash-style filters over the fleet, one tab each.
const (
	sectionAll = iota
	sectionWarm
	sectionClaimed
)

var sectionTitles = [...]string{"All", "Warm", "Claimed"}

// Glyphs from Nerd Fonts, which gh-dash already needs.
const (
	glyphSandbox = "\uf1b2" // fa-cube
	glyphShell   = "\uf120" // fa-terminal
	glyphClock   = "\uf017" // fa-clock-o
	glyphConfigs = "\uf15c" // fa-file-text
)

// layout places gh-dash's chrome: section tabs on top, the sandbox table on
// the left, the preview sidebar on the right (below it when narrow), and the
// footer. docTop, docW, and docH are the sidebar's scrolling body, the shell
// or the configs.
type layout struct {
	bodyTop, bodyH int
	tableW, tableH int
	sideX, sideW   int
	sideTop, sideH int
	docTop         int
	docW, docH     int
	stacked        bool
}

func (m model) layout() layout {
	l := layout{bodyTop: tabsH}
	l.bodyH = max(m.height-tabsH-m.footerHeight(), 4)
	if m.width < stackedBelow {
		l.stacked = true
		l.tableW, l.sideW = m.width, m.width
		l.tableH = clamp(m.tableHeight, tableHeaderH+itemH, max(l.bodyH-infoRows-4, tableHeaderH+itemH))
		if m.tableHeight <= 0 {
			l.tableH = clamp(l.bodyH*2/5, tableHeaderH+itemH, max(l.bodyH-infoRows-4, tableHeaderH+itemH))
		}
		l.sideTop = l.bodyTop + l.tableH
		l.sideH = max(l.bodyH-l.tableH, 1)
		l.docTop = l.sideTop + 1 + infoRows
		l.docW = max(m.width-4, 1)
		l.docH = max(l.sideH-1-infoRows, 1)
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
	l.docTop = l.bodyTop + infoRows
	l.docW = max(l.sideW-4, 1)
	l.docH = max(l.bodyH-infoRows, 1)
	return l
}

type nodeGroup struct {
	name  string
	boxes []int
}

func (m model) nodeGroups(ownerRank int) []nodeGroup {
	byNode := make(map[string][]int)
	for i, b := range m.boxes {
		if sidebarOwnerRank(b.OwnerKind) == ownerRank {
			byNode[b.NodeName] = append(byNode[b.NodeName], i)
		}
	}
	nodes := make([]string, 0, len(byNode))
	for node := range byNode {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i] == "" {
			return false
		}
		if nodes[j] == "" {
			return true
		}
		return nodes[i] < nodes[j]
	})

	groups := make([]nodeGroup, 0, len(nodes))
	for _, node := range nodes {
		indices := byNode[node]
		sort.SliceStable(indices, func(i, j int) bool {
			return m.boxes[indices[i]].Name < m.boxes[indices[j]].Name
		})
		groups = append(groups, nodeGroup{name: node, boxes: indices})
	}
	return groups
}

func sidebarOwnerRank(kind string) int {
	switch kind {
	case "SandboxWarmPool":
		return 0
	case "SandboxClaim":
		return 1
	default:
		return 2
	}
}

// navigationIndices is the table's order for the current section: warm,
// claimed, then other, each by node and name.
func (m model) navigationIndices() []int {
	indices := make([]int, 0, len(m.boxes))
	for rank := 0; rank < 3; rank++ {
		if (m.section == sectionWarm && rank != 0) || (m.section == sectionClaimed && rank != 1) {
			continue
		}
		for _, group := range m.nodeGroups(rank) {
			indices = append(indices, group.boxes...)
		}
	}
	return indices
}

func (m model) sectionCount(section int) int {
	n := 0
	for _, b := range m.boxes {
		rank := sidebarOwnerRank(b.OwnerKind)
		if section == sectionAll || (section == sectionWarm && rank == 0) || (section == sectionClaimed && rank == 1) {
			n++
		}
	}
	return n
}

func (m model) shellPaneSize() (int, int) {
	l := m.layout()
	return l.docW, l.docH
}

// ---- section tabs -------------------------------------------------------

type span struct{ from, to int }

// sectionSpans gives each section tab's columns in the tabs row.
func (m model) sectionSpans() []span {
	spans := make([]span, 0, len(sectionTitles))
	x := 0
	for i := range sectionTitles {
		w := lipgloss.Width(styles.Tabs.Tab.Render(m.sectionLabel(i)))
		spans = append(spans, span{from: x, to: x + w})
		x += w + 1
	}
	return spans
}

func (m model) sectionLabel(i int) string {
	return fmt.Sprintf("%s (%d)", sectionTitles[i], m.sectionCount(i))
}

func (m model) tabsView() string {
	parts := make([]string, 0, 2*len(sectionTitles))
	for i := range sectionTitles {
		if i > 0 {
			parts = append(parts, styles.Tabs.TabSeparator.Render("|"))
		}
		style := styles.Tabs.Tab
		if i == m.section {
			style = styles.Tabs.ActiveTab
		}
		parts = append(parts, style.Render(m.sectionLabel(i)))
	}
	left := strings.Join(parts, "")
	right := lipgloss.NewStyle().Foreground(th.SecondaryText).Render("ns "+cluster.Namespace) + " "
	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	row := truncateANSI(left+strings.Repeat(" ", gap)+right, m.width)
	return row + "\n" + lipgloss.NewStyle().Foreground(th.PrimaryBorder).Render(strings.Repeat("━", m.width))
}

// ---- table --------------------------------------------------------------

type column struct {
	title string
	width int // 0 grows
	right bool
}

// tableColumns resolves the sandbox column against the inner table width.
func tableColumns(inner int) []column {
	cols := []column{
		{title: glyphSandbox, width: 4},
		{title: "Sandbox"},
		{title: "Template", width: 22},
	}
	if inner < 80 {
		cols[2].width = clamp(inner-4-10-11-8, 8, 22)
		cols = append(cols, column{title: "CPU", width: 10, right: true}, column{title: "MEM", width: 11, right: true})
	} else {
		cols = append(cols, column{title: glyphShell, width: 4}, column{title: "CPU", width: 12, right: true},
			column{title: "MEM", width: 13, right: true}, column{title: glyphClock, width: 5, right: true})
	}
	used := 0
	for _, c := range cols {
		used += c.width
	}
	cols[1].width = max(inner-used, 8)
	return cols
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

// renderRow draws one sandbox as gh-dash draws a pull request: two lines per
// cell, the selection as a background, and a faint rule under the row.
func (m model) renderRow(i, inner int, selected bool) []string {
	b := m.boxes[i]
	style := styles.Table.CellStyle
	if selected {
		style = styles.Table.SelectedCellStyle
	}
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(th.SelectedBackground)
		}
		return s
	}

	var kindColor color.Color = th.FaintText
	switch b.OwnerKind {
	case "SandboxWarmPool":
		kindColor = warmColor
	case "SandboxClaim":
		kindColor = claimColor
	}
	node := b.NodeName
	if node == "" {
		node = "unassigned"
	}
	where := node
	if b.OwnerKind != "SandboxWarmPool" && b.Owner != "" {
		where += " · " + b.Owner
	}
	session := bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("○")
	if m.sessions[b.Name] {
		session = bg(lipgloss.NewStyle().Foreground(th.SuccessText)).Render("●")
	}
	h := m.hist[b.Name]

	template := b.Template
	if template == "" {
		template = "—"
	}
	cells := [][2]string{
		{bg(lipgloss.NewStyle().Foreground(kindColor)).Render(glyphSandbox), ""},
		{bg(lipgloss.NewStyle().Foreground(th.SecondaryText)).Render(where),
			bg(lipgloss.NewStyle().Foreground(th.PrimaryText)).Render(b.Name)},
		{bg(lipgloss.NewStyle().Foreground(th.PrimaryText)).Render(template), ""},
	}
	if inner >= 80 {
		cells = append(cells, [2]string{session, ""})
	} else {
		cells[2][1] = session
	}
	cells = append(cells,
		usageCell(m.usageKnown(b.Name), b.CPUUsageMC, b.CPULimitMC, "m", h.cpu, bg),
		usageCell(m.usageKnown(b.Name), b.MemUsageMiB, b.MemLimitMiB, "Mi", h.mem, bg),
	)
	if inner >= 80 {
		cells = append(cells, [2]string{bg(lipgloss.NewStyle().Foreground(th.SecondaryText)).Render(ageString(b.Created)), ""})
	}
	var top, bottom strings.Builder
	for c, col := range tableColumns(inner) {
		top.WriteString(cell(style, col.width, cells[c][0], col.right))
		bottom.WriteString(cell(style, col.width, cells[c][1], col.right))
	}
	rule := lipgloss.NewStyle().Foreground(th.FaintBorder).Render(strings.Repeat("─", inner))
	return []string{" " + top.String(), " " + bottom.String(), " " + rule}
}

func usageCell(known bool, usage, limit int64, unit string, hist []float64, bg func(lipgloss.Style) lipgloss.Style) [2]string {
	if limit <= 0 {
		return [2]string{bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("no limit"), ""}
	}
	num, ratio := "--", 0.0
	if known {
		num = fmt.Sprintf("%d%s", usage, unit)
		ratio = float64(usage) / float64(limit)
	}
	value := bg(lipgloss.NewStyle().Foreground(th.PrimaryText)).Render(num) +
		bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render(fmt.Sprintf("/%d%s", limit, unit))
	pct := bg(lipgloss.NewStyle().Foreground(levelColor(ratio))).Render(fmt.Sprintf("%4.0f%%", ratio*100))
	return [2]string{value, usageGraph(hist, sparkW) + pct}
}

// rows returns the rendered table body, cached until the data, selection,
// focus, or width changes, so a frame that only moves the shell pays nothing.
func (m model) rows(inner int) []string {
	key := rowKey{rev: m.dataRev, selected: m.selected, section: m.section, width: inner}
	if m.rowCache != nil && m.rowCache.lines != nil && m.rowCache.key == key {
		return m.rowCache.lines
	}
	var lines []string
	for _, i := range m.navigationIndices() {
		lines = append(lines, m.renderRow(i, inner, m.boxes[i].Name == m.selected)...)
	}
	if m.rowCache != nil {
		m.rowCache.key, m.rowCache.lines = key, lines
	}
	return lines
}

type rowKey struct {
	rev      int
	selected string
	section  int
	width    int
}

// rowCacheEntry is shared by every copy of the model, so a View on a value
// receiver can still fill it.
type rowCacheEntry struct {
	key         rowKey
	lines       []string
	header      string
	headerWidth int
	headerTheme uint64
}

// tableOffset is the first visible item, keeping the selection on screen.
func (m model) tableOffset(l layout) int {
	visible := max((l.tableH-tableHeaderH)/itemH, 1)
	sel := m.selectedIndex()
	return clamp(sel-visible+1, 0, max(len(m.navigationIndices())-visible, 0))
}

func (m model) tableView(l layout) []string {
	inner := max(l.tableW-2, 1)
	out := make([]string, 0, l.tableH)
	out = append(out, m.tableHeader(inner), "")
	order := m.navigationIndices()
	if len(order) == 0 {
		msg := "No sandboxes"
		if len(m.boxes) > 0 {
			msg = "No " + strings.ToLower(sectionTitles[m.section]) + " sandboxes"
		}
		out = append(out, "  "+dim(msg))
	} else {
		rows := m.rows(inner)
		start := m.tableOffset(l) * itemH
		end := min(start+l.tableH-tableHeaderH, len(rows))
		out = append(out, rows[start:end]...)
	}
	for i := range out {
		out[i] = padTo(out[i], l.tableW)
	}
	for len(out) < l.tableH {
		out = append(out, strings.Repeat(" ", l.tableW))
	}
	return out[:l.tableH]
}

// rowAt maps a frame row inside the table body to the sandbox drawn there.
func (m model) rowAt(y int) (int, bool) {
	l := m.layout()
	body := y - l.bodyTop - tableHeaderH
	if body < 0 || y >= l.bodyTop+l.tableH {
		return 0, false
	}
	pos := m.tableOffset(l) + body/itemH
	order := m.navigationIndices()
	if body%itemH == itemH-1 || pos >= len(order) {
		return 0, false
	}
	return order[pos], true
}

// ---- sidebar ------------------------------------------------------------

func phaseColor(phase string) color.Color {
	switch phase {
	case "Running", "Succeeded":
		return th.SuccessText
	case "Pending", "":
		return th.WarningText
	default:
		return th.ErrorText
	}
}

func kindName(b *cluster.Sandbox) (string, color.Color) {
	switch b.OwnerKind {
	case "SandboxWarmPool":
		return "Warm", warmColor
	case "SandboxClaim":
		return "Claimed", claimColor
	}
	return b.OwnerKind, warnColor
}

// infoLines is the head of the preview, laid out as gh-dash heads a pull
// request: the location bar, the title block, a pill with the state, the
// facts, and the heading of the body below.
func (m model) infoLines(width int) []string {
	b := m.selectedBox()
	if b == nil {
		return []string{common.RenderPreviewHeader(th, width, truncateANSI(cluster.Namespace, max(width-2, 1)))}
	}
	kind, kindColor := kindName(b)
	header := common.RenderPreviewHeader(th, width, truncateANSI(cluster.Namespace+" · "+b.Owner, max(width-2, 1)))
	title := strings.Split(common.RenderPreviewTitle(th, styles.Common, width, truncateANSI(b.Name, max(width-2, 1))), "\n")

	pill := styles.PrView.PillStyle.Background(kindColor).Render(" " + glyphSandbox + " " + kind + " ")
	node := b.NodeName
	if node == "" {
		node = "unassigned"
	}
	state := pill + " " + lipgloss.NewStyle().Foreground(phaseColor(b.Phase)).Render("● "+b.Phase) +
		dim(" · node ") + node + dim(" · "+ageString(b.Created))

	var facts []string
	if b.OwnerKind == "SandboxClaim" {
		h := m.hist[b.Name]
		facts = usageTable([]usageRow{
			{"CPU", b.CPUUsageMC, b.CPULimitMC, "m", h.cpu},
			{"MEM", b.MemUsageMiB, b.MemLimitMiB, "Mi", h.mem},
		}, m.usageKnown(b.Name), width-1)
	} else {
		facts = []string{
			truncateANSI(dim("image ")+b.Image, max(width-1, 1)),
			fitParts([]string{
				dim("cpu ") + fmt.Sprintf("%d/%dm", b.CPUReqMC, b.CPULimitMC),
				dim("mem ") + fmt.Sprintf("%dMi/%dMi", b.MemReqMiB, b.MemLimitMiB),
				dim("restart ") + b.RestartPolicy,
			}, width-1),
		}
	}
	session := dim("○ herdr off")
	if m.sessions[b.Name] {
		session = warmStyle.Render("● herdr live")
	}

	heading, hint := glyphConfigs+" Configs", "⏎ or click to focus"
	focused := m.focus == focusInspect
	if !m.isWarmSelected() {
		heading, focused = glyphShell+" Shell", m.focus == focusShell
		if !m.sessions[b.Name] {
			hint = ""
		}
	}
	if focused {
		hint = "esc leaves"
		if m.focus == focusShell {
			hint = "C-b q leaves"
		}
	}
	headStyle := lipgloss.NewStyle().Bold(true).Underline(true).Foreground(th.PrimaryText)
	if focused {
		headStyle = headStyle.Foreground(th.SuccessText)
	}
	head := spread(headStyle.Render(heading), dim(hint), width)

	lines := []string{header}
	lines = append(lines, title[0])
	lines = append(lines, " "+state)
	for _, f := range facts {
		lines = append(lines, " "+f)
	}
	return append(lines, " "+session, "", " "+head)
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
			out[i] = dim(r.label + " no limit")
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
	if m.isWarmSelected() {
		lines, _ := m.warmInspectView(l)
		return lines
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
	case !m.sessions[b.Name]:
		return []string{dim("no session — claim with c, or enter on a claimed sandbox")}
	case m.shellPod != b.Name:
		return nil
	}
	content := m.shellLines
	if len(content) > l.docH {
		content = content[len(content)-l.docH:]
	}
	lines := make([]string, 0, len(content))
	for _, ln := range content {
		lines = append(lines, truncateANSI(ln, l.docW))
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

func (m model) hints() []key.Binding {
	if m.focus == focusInspect {
		return []key.Binding{keyScroll, keyPage, keyBack, keyClaim, keyTabs, keyQuit, keyHelp}
	}
	return []key.Binding{keyMove, keySection, keyOpen, keyClaim, keyDestroy, keyFull, keyRefresh, keyTabs, keyQuit, keyHelp}
}

// renderHints draws key hints on the footer's background, dropping from the
// end, keeping the last, until they fit room.
func renderHints(hints []key.Binding, room int) string {
	bgc := th.SelectedBackground
	keyS := lipgloss.NewStyle().Background(bgc).Foreground(th.PrimaryText)
	descS := lipgloss.NewStyle().Background(bgc).Foreground(th.FaintText)
	sepS := lipgloss.NewStyle().Background(bgc).Foreground(th.SecondaryBorder)
	render := func(hs []key.Binding) string {
		parts := make([]string, len(hs))
		for i, h := range hs {
			parts[i] = keyS.Render(h.Help().Key) + descS.Render(" "+h.Help().Desc)
		}
		return strings.Join(parts, sepS.Render(" • "))
	}
	for len(hints) > 1 {
		if line := render(hints); lipgloss.Width(line) <= room {
			return line
		}
		last := len(hints) - 1
		hints = append(hints[:last-1:last-1], hints[last])
	}
	return render(hints)
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
	if len(order) > 0 {
		pos = m.selectedIndex() + 1
	}
	updated := "never"
	if !m.updatedAt.IsZero() {
		updated = "~" + ageString(m.updatedAt) + " ago"
	}
	return fmt.Sprintf("%s Updated %s • Sandbox %d/%d", glyphClock, updated, pos, len(order))
}

func (m model) statusLine() string {
	bgc := lipgloss.NewStyle().Background(th.SelectedBackground)
	width := max(m.width, 1)
	var left string
	switch {
	case m.focus == focusShell && m.prefix:
		left = prefixBarStyle.Render(" C-b armed · q leaves bash, any other key cancels ")
	case m.focus == focusShell:
		left = shellBarStyle.Render(" BASH ") + bgc.Render(" ") +
			renderHints([]key.Binding{keyLeaveShell}, width) +
			bgc.Foreground(th.FaintText).Render(" · every key goes to the sandbox")
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

func sandboxCount(n int) string {
	if n == 1 {
		return "1 sandbox"
	}
	return fmt.Sprintf("%d sandboxes", n)
}

// Count is the number of sandboxes, for the tab bar.
func (m model) Count() int { return len(m.boxes) }

// View renders the tab at exactly its width and height.
func (m model) View() string {
	if !m.loaded && m.err == nil {
		return fill(dim(" loading sandboxes…"), m.width, m.height)
	}
	l := m.layout()
	var b strings.Builder
	b.WriteString(m.tabsView() + "\n")
	table := m.tableView(l)
	side := m.sidebarView(l)
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
	return b.String()
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
