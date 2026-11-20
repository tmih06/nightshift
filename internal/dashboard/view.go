package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tmih06/nightshift/internal/cluster"
)

const (
	stackedBelow = 50
	minLeftW     = 15
	minRightW    = 20
	headerH      = 6 // 4 content rows + top/bottom borders
)

type layout struct {
	leftW, leftH int
	colW         int
	infoW        int
	usageW       int
	headerH      int
	shellH       int
	stacked      bool
	shellTop     int
}

func (m model) layout() layout {
	return m.layoutWithInfo(m.headerInfoLines())
}

func (m model) layoutWithInfo(info []string) layout {
	panelsH := max(m.height-2, 3) // title + status line
	var l layout
	l.headerH = headerH
	if m.width < stackedBelow {
		l.stacked = true
		l.leftW = m.width
		l.leftH = m.leftHeight
		if l.leftH <= 0 {
			l.leftH = panelsH / 3
		}
		l.leftH = clamp(l.leftH, 3, max(panelsH-6, 3))
		l.colW = m.width
		rest := max(panelsH-l.leftH, 6)
		l.shellH = max(rest-l.headerH, 3)
		l.infoW, l.usageW = headerBoxWidths(l.colW, info)
		l.shellTop = 1 + l.leftH + l.headerH
		return l
	}
	l.leftW = m.leftWidth
	if l.leftW <= 0 {
		l.leftW = m.fitLeftWidth()
	}
	l.leftW = clamp(l.leftW, minLeftW, max(m.width-minRightW, minLeftW))
	l.leftH = panelsH
	l.colW = max(m.width-l.leftW, 1)
	l.shellH = max(panelsH-l.headerH, 3)
	l.infoW, l.usageW = headerBoxWidths(l.colW, info)
	l.shellTop = 1 + l.headerH
	return l
}

// headerBoxWidths fits the info box to its longest line (bounded so the
// usage box stays readable) and gives usageW the exact remainder so
// infoW + usageW == colW.
func headerBoxWidths(colW int, info []string) (infoW, usageW int) {
	for _, ln := range info {
		// Border (2) + Padding(0, 1) (2) = 4 columns around content.
		if w := lipgloss.Width(ln) + 4; w > infoW {
			infoW = w
		}
	}
	minInfo := min(16, colW/2)
	minUsage := min(24, max(colW-minInfo, 1))
	maxInfo := max(colW-minUsage, minInfo)
	infoW = clamp(infoW, minInfo, maxInfo)
	return infoW, max(colW-infoW, 1)
}

func (m model) fitLeftWidth() int {
	w := 18
	for _, b := range m.boxes {
		if n := len([]rune(b.Name)) + 24; n > w {
			w = n
		}
		if n := len([]rune(b.NodeName)) + 13; n > w {
			w = n
		}
	}
	return clamp(w, minLeftW, max(m.width-minRightW, minLeftW))
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

func (m model) navigationIndices() []int {
	indices := make([]int, 0, len(m.boxes))
	for rank := 0; rank < 3; rank++ {
		for _, group := range m.nodeGroups(rank) {
			indices = append(indices, group.boxes...)
		}
	}
	return indices
}

func (m model) shellPaneSize() (int, int) {
	l := m.layout()
	return max(l.colW-4, 1), max(l.shellH-2, 1)
}

// pane truncates every line to innerW so bordered boxes never auto-wrap
// or change height. Lipgloss Width(n) on this padded border style yields
// outer width n+2, so outerW passes Width(outerW-2).
func pane(style lipgloss.Style, lines []string, outerW, outerH int) string {
	innerW := max(outerW-4, 1)
	innerH := max(outerH-2, 1)
	fit := make([]string, innerH)
	for i := 0; i < len(lines) && i < innerH; i++ {
		fit[i] = truncateANSI(lines[i], innerW)
	}
	return style.Width(outerW - 2).Render(strings.Join(fit, "\n"))
}

func (m model) leftLines() ([]string, map[int]int) {
	return m.leftLinesWith(m.layout())
}

func (m model) leftLinesWith(l layout) ([]string, map[int]int) {
	innerW := max(l.leftW-4, 1)
	targets := map[int]int{}
	if len(m.boxes) == 0 {
		return []string{dim("no sandboxes…")}, targets
	}

	lines := make([]string, 0, len(m.boxes)+4)
	entry := func(i int, prefix string) string {
		b := m.boxes[i]
		marker := " "
		if b.Name == m.selected {
			marker = cursorStyle.Render("▶")
		}
		kind, kindStyle := "[other]", warnStyle
		switch b.OwnerKind {
		case "SandboxWarmPool":
			kind, kindStyle = "[warm]", warmStyle
		case "SandboxClaim":
			kind, kindStyle = "[claimed]", claimStyle
		}
		nameW := max(innerW-lipgloss.Width(prefix)-lipgloss.Width(kind)-5, 1)
		name := truncateANSI(b.Name, nameW)
		switch {
		case b.Name == m.selected:
			name = cursorStyle.Render(name)
		case b.OwnerKind == "SandboxWarmPool":
			name = warmStyle.Render(name)
		case b.OwnerKind == "SandboxClaim":
			name = claimStyle.Render(name)
		default:
			name = warnStyle.Render(name)
		}
		session := dim("○")
		if m.sessions[b.Name] {
			session = warmStyle.Render("●")
		}
		return prefix + marker + " " + kindStyle.Render(kind) + " " + name + " " + session
	}

	for rank, title := range []string{"WARM POOL", "CLAIMED", "OTHER"} {
		groups := m.nodeGroups(rank)
		if rank == 2 && len(groups) == 0 {
			continue
		}
		count := 0
		for _, group := range groups {
			count += len(group.boxes)
		}
		lines = append(lines, sectionStyle.Render(fmt.Sprintf("%s (%d)", title, count)))
		for groupIndex, group := range groups {
			lastGroup := groupIndex == len(groups)-1
			branch := "├─ "
			if lastGroup {
				branch = "└─ "
			}
			nodeName := group.name
			if nodeName == "" {
				nodeName = "unassigned"
			}
			lines = append(lines, branch+sectionStyle.Render(fmt.Sprintf("%s (%d)", nodeName, len(group.boxes))))

			parentBranch := "│  "
			if lastGroup {
				parentBranch = "   "
			}
			for i, boxIndex := range group.boxes {
				leafBranch := parentBranch + "├─ "
				if i == len(group.boxes)-1 {
					leafBranch = parentBranch + "└─ "
				}
				targets[len(lines)] = boxIndex
				lines = append(lines, entry(boxIndex, leafBranch))
			}
		}
	}

	visibleRows := max(l.leftH-2, 1)
	if len(lines) <= visibleRows {
		return lines, targets
	}
	selectedRow := -1
	for row, boxIndex := range targets {
		if m.boxes[boxIndex].Name == m.selected {
			selectedRow = row
			break
		}
	}
	start := 0
	if selectedRow >= 0 {
		start = clamp(selectedRow-visibleRows/2, 0, len(lines)-visibleRows)
	}
	visible := lines[start : start+visibleRows]
	visibleTargets := make(map[int]int, len(targets))
	for row, boxIndex := range targets {
		if row >= start && row < start+visibleRows {
			visibleTargets[row-start] = boxIndex
		}
	}
	return visible, visibleTargets
}

func (m model) headerInfoLines() []string {
	b := m.selectedBox()
	if b == nil {
		return []string{"nothing selected"}
	}
	badge, badgeStyle := "warm", warmStyle
	switch {
	case b.OwnerKind == "SandboxClaim":
		badge, badgeStyle = "claimed", claimStyle
	case b.OwnerKind != "SandboxWarmPool":
		badge, badgeStyle = b.OwnerKind, warnStyle
	}
	sess := dim("herdr off")
	if m.sessions[b.Name] {
		sess = warmStyle.Render("herdr live")
	}
	return []string{
		nameStyle.Render(b.Name),
		dim("owner ") + b.Owner,
		b.Phase + dim(" · ") + badgeStyle.Render(badge),
		sess + dim(" · ") + ageString(b.Created),
	}
}

func (m model) headerUsageLines() []string {
	return m.headerUsageLinesWith(m.layout())
}

func (m model) headerUsageLinesWith(l layout) []string {
	b := m.selectedBox()
	if b == nil {
		return []string{dim("—")}
	}
	inner := max(l.usageW-4, 1)
	if b.OwnerKind != "SandboxClaim" {
		return specLines(b, inner)
	}
	h := m.hist[b.Name]
	lines := make([]string, 0, 4)
	lines = append(lines, usageLines("CPU", m.metricsOK, b.CPUUsageMC, b.CPULimitMC, "m", h.cpu, inner)...)
	lines = append(lines, usageLines("MEM", m.metricsOK, b.MemUsageMiB, b.MemLimitMiB, "Mi", h.mem, inner)...)
	return lines
}

func specLines(b *cluster.Sandbox, width int) []string {
	cpu := fmt.Sprintf("%d/%dm", b.CPUReqMC, b.CPULimitMC)
	mem := fmt.Sprintf("%dMi/%dMi", b.MemReqMiB, b.MemLimitMiB)
	lbl := func(s string) string { return dim(fmt.Sprintf("%-5s", s)) }
	imgMax := max(width-10-len(b.RestartPolicy), 8)
	cmdMax := max(width-6, 8)
	return []string{
		fmt.Sprintf("%s %s %s %s", lbl("image"), truncateANSI(b.Image, imgMax), dim("rc"), b.RestartPolicy),
		fmt.Sprintf("%s %s", lbl("cmd"), truncateANSI(b.Command, cmdMax)),
		fmt.Sprintf("%s %-16s %s", lbl("CPU"), cpu, dim("req/lim")),
		fmt.Sprintf("%s %-16s %s", lbl("MEM"), mem, dim("req/lim")),
	}
}

func usageLines(label string, known bool, usage, limit int64, unit string, hist []float64, width int) []string {
	lbl := dim(fmt.Sprintf("%-3s", label))
	if limit <= 0 {
		return []string{truncateANSI(fmt.Sprintf("%s %-18s", lbl, dim("no limit")), width), usageGraph(nil, width)}
	}
	num, ratio := "--", 0.0
	if known {
		num = fmt.Sprintf("%d%s", usage, unit)
		ratio = float64(usage) / float64(limit)
	}
	value := fmt.Sprintf("%s / %d%s", num, limit, unit)
	text := fmt.Sprintf("%s %-15s %3.0f%%", lbl, value, ratio*100)
	barW := width - lipgloss.Width(text) - 1
	var valueRow string
	if barW > 0 {
		valueRow = text + " " + bar(ratio, barW)
	} else {
		valueRow = truncateANSI(text, width)
	}
	return []string{valueRow, usageGraph(hist, width)}
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

func levelColor(ratio float64) lipgloss.Color {
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

func (m model) shellLinesView(l layout) []string {
	innerW := max(l.colW-4, 1)
	innerH := max(l.shellH-2, 1)
	b := m.selectedBox()

	var title string
	switch {
	case b == nil:
		title = dim("shell")
	case !m.sessions[b.Name]:
		title = dim("no session — claim with c, or enter on a claimed sandbox")
	case m.focus == focusShell:
		title = cursorStyle.Render("▶ bash — C-b q or click sidebar leaves")
	default:
		title = dim("bash — click here or enter to focus")
	}
	lines := make([]string, 0, innerH)
	lines = append(lines, title)

	if b != nil && m.sessions[b.Name] && m.shellPod == b.Name {
		content := m.shellLines
		if len(content) > innerH-1 {
			content = content[len(content)-(innerH-1):]
		}
		for _, ln := range content {
			lines = append(lines, truncateANSI(ln, innerW))
		}
	}
	return lines
}

func bar(ratio float64, width int) string {
	filled := clamp(int(ratio*float64(width)), 0, width)
	st := warmStyle
	switch {
	case ratio >= 0.9:
		st = badStyle
	case ratio >= 0.6:
		st = warnStyle
	}
	return st.Render(strings.Repeat("█", filled)) + dim(strings.Repeat("░", width-filled))
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

func (m model) statusLine() string {
	msg := m.status
	if m.err != nil {
		msg = badStyle.Render("fetch error: " + m.err.Error())
	} else if msg == "" && !m.metricsOK && m.loaded {
		msg = warnStyle.Render("live usage unavailable (metrics-server)")
	}
	if msg != "" {
		return truncateANSI(msg, m.width)
	}
	switch {
	case m.focus == focusShell && m.prefix:
		return truncateANSI(prefixBarStyle.Render(" C-b armed · q leaves bash, any other key cancels "), m.width)
	case m.focus == focusShell:
		return truncateANSI(shellBarStyle.Render(" C-b q leaves bash · click sidebar to exit · every key goes to the sandbox "), m.width)
	case m.focus == focusInspect:
		return truncateANSI(dim("j/k/pgup/pgdn scroll · esc or click sidebar back · c claim+bash · q quit"), m.width)
	default:
		return truncateANSI(dim("j/k move · enter bash (claimed) · F fullscreen · c claim+bash · x destroy · q quit · drag resizes"), m.width)
	}
}

func (m model) View() string {
	if !m.loaded && m.err == nil {
		return "loading sandboxes…"
	}

	title := titleStyle.Render("nightshift") + "  " +
		dim(fmt.Sprintf("%d sandboxes · ns %s", len(m.boxes), cluster.Namespace))

	info := m.headerInfoLines()
	l := m.layoutWithInfo(info)
	leftLines, _ := m.leftLinesWith(l)
	leftStyle := navFocusStyle
	if m.focus != focusNav {
		leftStyle = rightPanelStyle
	}
	left := pane(leftStyle, leftLines, l.leftW, l.leftH)

	infoBox := pane(rightPanelStyle, info, l.infoW, l.headerH)
	usageBox := pane(rightPanelStyle, m.headerUsageLinesWith(l), l.usageW, l.headerH)
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, usageBox)

	var lowerPane string
	if m.isWarmSelected() {
		st := rightPanelStyle
		if m.focus == focusInspect {
			st = shellFocusStyle
		}
		lowerPane = pane(st, m.warmInspectView(l), l.colW, l.shellH)
	} else {
		st := rightPanelStyle
		if m.focus == focusShell {
			st = shellFocusStyle
		}
		lowerPane = pane(st, m.shellLinesView(l), l.colW, l.shellH)
	}
	rightCol := lipgloss.JoinVertical(lipgloss.Left, headerRow, lowerPane)

	var body string
	if l.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, left, rightCol)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, rightCol)
	}
	return title + "\n" + body + "\n" + m.statusLine()
}
