package dashboard

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

const (
	headingH       = 1 // one line per group heading
	tableMediumMin = 68
	tableWideMin   = 84
	podUnassigned  = "unassigned"
	colCPU         = "CPU"
	colMEM         = "MEM"
)

// Sections are gh-dash-style filters over the essential pods, one tab each.
const (
	sectionAll = iota
	sectionClaimed
	sectionWarm
	sectionInfra
	sectionCount
)

func sectionTitle(section int) string {
	switch section {
	case sectionClaimed:
		return "Claimed"
	case sectionWarm:
		return "Warm"
	case sectionInfra:
		return "Infra"
	default:
		return "All"
	}
}

// tableEntry is either a non-selectable group heading or a pod row referencing m.boxes[boxIdx].
type tableEntry struct {
	isHeading bool
	heading   string
	boxIdx    int
}

type roleGroup struct {
	title string
	roles []fleet.PodRole
}

func allRoleGroups() []roleGroup {
	return append([]roleGroup{
		{title: "CLAIMED", roles: []fleet.PodRole{fleet.RoleClaimed}},
		{title: "WARM POOLS", roles: []fleet.PodRole{fleet.RoleWarm}},
	}, infraRoleGroups()...)
}

func infraRoleGroups() []roleGroup {
	return []roleGroup{
		{title: "EGRESS PROXY", roles: []fleet.PodRole{fleet.RoleEgress}},
		{title: "CLUSTER CORE", roles: []fleet.PodRole{fleet.RoleSystem}},
		{title: "JOBS & OTHER", roles: []fleet.PodRole{fleet.RoleJob, fleet.RoleOther}},
	}
}

func matchesRoles(role fleet.PodRole, allowed []fleet.PodRole) bool {
	for _, r := range allowed {
		if role == r {
			return true
		}
	}
	return false
}

func (m model) sortedIndicesForRoles(roles []fleet.PodRole) []int {
	var indices []int
	for i := range m.boxes {
		if matchesRoles(m.boxes[i].PodRole(), roles) {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := &m.boxes[indices[i]], &m.boxes[indices[j]]
		if a.NodeName != b.NodeName {
			if a.NodeName == "" {
				return false
			}
			if b.NodeName == "" {
				return true
			}
			return a.NodeName < b.NodeName
		}
		return a.Name < b.Name
	})
	return indices
}

// tableEntries builds the ordered list of group headings and pod rows for the
// active section. Single-role sections (Claimed, Warm) omit redundant headings.
func (m model) tableEntries() []tableEntry {
	switch m.section {
	case sectionClaimed:
		return flatEntries(m.sortedIndicesForRoles([]fleet.PodRole{fleet.RoleClaimed}))
	case sectionWarm:
		return flatEntries(m.sortedIndicesForRoles([]fleet.PodRole{fleet.RoleWarm}))
	case sectionInfra:
		return m.groupedEntries(infraRoleGroups())
	default:
		return m.groupedEntries(allRoleGroups())
	}
}

func flatEntries(indices []int) []tableEntry {
	out := make([]tableEntry, 0, len(indices))
	for _, idx := range indices {
		out = append(out, tableEntry{boxIdx: idx})
	}
	return out
}

func (m model) groupedEntries(groups []roleGroup) []tableEntry {
	type bucket struct {
		title   string
		indices []int
	}
	buckets := make([]bucket, 0, len(groups))
	for _, g := range groups {
		if idx := m.sortedIndicesForRoles(g.roles); len(idx) > 0 {
			buckets = append(buckets, bucket{title: g.title, indices: idx})
		}
	}
	showHeadings := len(buckets) > 1 || (m.section == sectionAll && len(buckets) == 1)
	out := make([]tableEntry, 0, len(m.boxes)+len(buckets))
	for _, bk := range buckets {
		if showHeadings {
			out = append(out, tableEntry{
				isHeading: true,
				heading:   fmt.Sprintf("%s (%d)", bk.title, len(bk.indices)),
			})
		}
		for _, idx := range bk.indices {
			out = append(out, tableEntry{boxIdx: idx})
		}
	}
	return out
}

// navigationIndices is the selectable pod order for the current section.
func (m model) navigationIndices() []int {
	entries := m.tableEntries()
	indices := make([]int, 0, len(entries))
	for _, e := range entries {
		if !e.isHeading {
			indices = append(indices, e.boxIdx)
		}
	}
	return indices
}

func (m model) sectionCount(section int) int {
	n := 0
	for i := range m.boxes {
		b := &m.boxes[i]
		role := b.PodRole()
		switch section {
		case sectionAll:
			n++
		case sectionClaimed:
			if role == fleet.RoleClaimed {
				n++
			}
		case sectionWarm:
			if role == fleet.RoleWarm {
				n++
			}
		case sectionInfra:
			if b.IsInfra() {
				n++
			}
		}
	}
	return n
}

// ---- section tabs & cluster summary ------------------------------------

type span struct{ from, to int }

// sectionSpans gives each section tab's columns in the tabs row.
func (m model) sectionSpans() []span {
	spans := make([]span, 0, sectionCount)
	x := 0
	for i := range sectionCount {
		w := lipgloss.Width(styles.Tabs.Tab.Render(m.sectionLabel(i)))
		spans = append(spans, span{from: x, to: x + w})
		x += w + 1
	}
	return spans
}

func (m model) sectionLabel(i int) string {
	return fmt.Sprintf("%s (%d)", sectionTitle(i), m.sectionCount(i))
}

func (m model) clusterSummary() string {
	if len(m.boxes) == 0 {
		return lipgloss.NewStyle().Foreground(th.SecondaryText).Render("ns " + fleet.Namespace)
	}
	running, egressUp, egressSeen := 0, false, false
	for i := range m.boxes {
		b := &m.boxes[i]
		if b.Phase == fleet.PodPhaseRunning {
			running++
		}
		if b.PodRole() == fleet.RoleEgress {
			egressSeen = true
			if b.Phase == fleet.PodPhaseRunning {
				egressUp = true
			}
		}
	}
	dot := warmStyle.Render("●")
	if running < len(m.boxes) {
		dot = warnStyle.Render("●")
	}
	summary := fmt.Sprintf("%s %d/%d running", dot, running, len(m.boxes))
	if egressSeen {
		if egressUp {
			summary += dim(" · ") + egressTextStyle().Render(glyphEgress+" egress up")
		} else {
			summary += dim(" · ") + badStyle.Render(glyphEgress+" egress down")
		}
	}
	return summary
}

func (m model) tabsView() string {
	parts := make([]string, 0, 2*sectionCount)
	for i := range sectionCount {
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
	right := m.clusterSummary() + " "
	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	row := truncateANSI(left+strings.Repeat(" ", gap)+right, m.width)
	return row + "\n" + lipgloss.NewStyle().Foreground(th.PrimaryBorder).Render(strings.Repeat("━", m.width))
}

// ---- columns & row rendering -------------------------------------------

type column struct {
	title string
	width int // 0 grows
	right bool
}

// tableColumns resolves the Pod column width against the inner table width so
// pod names and CPU/MEM metrics fit cleanly without truncation at 120 cols.
func tableColumns(inner int) []column {
	cols := []column{
		{title: glyphSandbox, width: 3},
		{title: "Pod"},
	}
	switch {
	case inner >= tableWideMin:
		cols = append(cols,
			column{title: "State", width: 10},
			column{title: colCPU, width: 12, right: true},
			column{title: colMEM, width: 12, right: true},
			column{title: glyphClock, width: 5, right: true},
		)
	case inner >= tableMediumMin:
		cols = append(cols,
			column{title: "State", width: 10},
			column{title: colCPU, width: 12, right: true},
			column{title: colMEM, width: 12, right: true},
		)
	default:
		cols = append(cols,
			column{title: colCPU, width: 12, right: true},
			column{title: colMEM, width: 12, right: true},
		)
	}
	used := 0
	for _, c := range cols {
		used += c.width
	}
	cols[1].width = max(inner-used, 8)
	return cols
}

func roleGlyph(role fleet.PodRole) string {
	switch role {
	case fleet.RoleClaimed:
		return glyphShell
	case fleet.RoleEgress:
		return glyphEgress
	case fleet.RoleSystem:
		return glyphCore
	case fleet.RoleJob:
		return glyphJob
	default:
		return glyphSandbox
	}
}

func kindName(b *fleet.Sandbox) (string, color.Color) {
	switch b.PodRole() {
	case fleet.RoleWarm:
		return "Warm", warmColor
	case fleet.RoleClaimed:
		return "Claimed", claimColor
	case fleet.RoleEgress:
		return "Egress", egressAccentColor()
	case fleet.RoleSystem:
		return "Core", systemAccentColor()
	case fleet.RoleJob:
		return "Job", warnColor
	default:
		if b.OwnerKind != "" {
			return b.OwnerKind, warnColor
		}
		return "Pod", warnColor
	}
}

func (m model) podSubtitle(b *fleet.Sandbox, compact bool, bg func(lipgloss.Style) lipgloss.Style) string {
	sec := bg(lipgloss.NewStyle().Foreground(th.SecondaryText))
	faint := bg(lipgloss.NewStyle().Foreground(th.FaintText))
	version := agentSemver(b.AgentVersion)
	switch b.PodRole() {
	case fleet.RoleClaimed:
		return m.claimedSubtitle(b, version, bg, sec, faint)
	case fleet.RoleWarm:
		tmpl := b.Template
		if tmpl == "" {
			tmpl = b.Owner
		}
		if tmpl == "" {
			tmpl = "warm"
		}
		if version != "" {
			return sec.Render(tmpl) + faint.Render(" · "+version)
		}
		return sec.Render(tmpl)
	default:
		return infraSubtitle(b, compact, bg, sec)
	}
}

func (m model) claimedSubtitle(
	b *fleet.Sandbox,
	version string,
	bg func(lipgloss.Style) lipgloss.Style,
	sec, faint lipgloss.Style,
) string {
	dot := faint.Render("○")
	if wins := m.podWindows(b.Name); len(wins) > 1 {
		dot = bg(lipgloss.NewStyle().Foreground(th.SuccessText)).Render(fmt.Sprintf("●%d", len(wins)))
	} else if len(wins) == 1 {
		dot = bg(lipgloss.NewStyle().Foreground(th.SuccessText)).Render("●")
	}
	parts := make([]string, 0, 3)
	if b.Owner != "" {
		parts = append(parts, b.Owner)
	}
	if b.Template != "" {
		parts = append(parts, strings.TrimSuffix(b.Template, "-template"))
	}
	if version != "" {
		parts = append(parts, version)
	}
	if len(parts) == 0 {
		return dot + sec.Render(" claimed")
	}
	return dot + " " + sec.Render(strings.Join(parts, " · "))
}

func infraSubtitle(
	b *fleet.Sandbox,
	compact bool,
	bg func(lipgloss.Style) lipgloss.Style,
	sec lipgloss.Style,
) string {
	ns := b.EffectiveNamespace()
	parts := []string{ns}
	if compact {
		if b.Ready != "" {
			parts = append(parts, b.Ready)
		}
		if b.Restarts > 0 {
			return sec.Render(strings.Join(parts, " · ")) +
				bg(lipgloss.NewStyle().Foreground(warnColor)).Render(fmt.Sprintf(" %d↺", b.Restarts))
		}
		if age := ageString(b.Created); age != "…" {
			parts = append(parts, age)
		}
		return sec.Render(strings.Join(parts, " · "))
	}
	if b.PodIP != "" {
		parts = append(parts, b.PodIP)
	} else if b.Owner != "" {
		parts = append(parts, b.Owner)
	}
	return sec.Render(strings.Join(parts, " · "))
}

func stateCell(b *fleet.Sandbox, bg func(lipgloss.Style) lipgloss.Style) [2]string {
	status := b.Phase
	statusColor := phaseColor(b.Phase)
	if b.Waiting != "" {
		status = b.Waiting
		statusColor = warnColor
	}
	top := bg(lipgloss.NewStyle().Foreground(statusColor)).Render(status)
	bot := ""
	if b.Ready != "" {
		bot = bg(lipgloss.NewStyle().Foreground(th.SecondaryText)).Render(b.Ready)
	}
	if b.Restarts > 0 {
		rst := bg(lipgloss.NewStyle().Foreground(warnColor)).Render(fmt.Sprintf("%d↺", b.Restarts))
		if bot != "" {
			bot += " " + rst
		} else {
			bot = rst
		}
	}
	return [2]string{top, bot}
}

func (m model) renderHeadingRow(title string, inner int) string {
	label := groupHeadTextStyle().Render(title)
	w := lipgloss.Width(label)
	ruleW := max(0, inner-w-1)
	rule := ""
	if ruleW > 0 {
		rule = " " + lipgloss.NewStyle().Foreground(th.FaintBorder).Render(strings.Repeat("─", ruleW))
	}
	return " " + truncateANSI(label+rule, inner)
}

// renderRow draws one pod as two lines plus a faint separator rule.
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

	_, kindColor := kindName(&b)
	nameS := bg(lipgloss.NewStyle().Foreground(th.PrimaryText))
	if selected {
		nameS = nameS.Bold(true)
	}
	phaseDot := bg(lipgloss.NewStyle().Foreground(phaseColor(b.Phase))).Render("●")
	if b.Waiting != "" {
		phaseDot = bg(lipgloss.NewStyle().Foreground(warnColor)).Render("●")
	}

	compact := inner < tableMediumMin
	h := m.hist[b.Name]
	known := b.UsageKnown
	subtitle := m.podSubtitle(&b, compact, bg)
	if b.Startup != nil {
		subtitle = startupSubtitle(b.Startup, time.Now(), compact, bg)
	}
	cells := [][2]string{
		{bg(lipgloss.NewStyle().Foreground(kindColor)).Render(roleGlyph(b.PodRole())), phaseDot},
		{nameS.Render(b.Name), subtitle},
	}
	if !compact {
		cells = append(cells, stateCell(&b, bg))
	}
	cells = append(cells,
		usageCell(usageRow{usage: b.CPUUsageMC, limit: b.CPULimitMC, unit: "m", hist: h.cpu}, known, bg),
		usageCell(usageRow{usage: b.MemUsageMiB, limit: b.MemLimitMiB, unit: "Mi", hist: h.mem}, known, bg),
	)
	if inner >= tableWideMin {
		cells = append(cells, [2]string{
			bg(lipgloss.NewStyle().Foreground(th.SecondaryText)).Render(ageString(b.Created)),
			"",
		})
	}

	var top, bottom strings.Builder
	for c, col := range tableColumns(inner) {
		top.WriteString(cell(style, col.width, cells[c][0], col.right))
		bottom.WriteString(cell(style, col.width, cells[c][1], col.right))
	}
	rule := lipgloss.NewStyle().Foreground(th.FaintBorder).Render(strings.Repeat("─", inner))
	return []string{" " + top.String(), " " + bottom.String(), " " + rule}
}

func formatQuantity(val int64, unit string) string {
	if unit == "Mi" && val >= 1024 && val%1024 == 0 {
		return fmt.Sprintf("%dGi", val/1024)
	}
	return fmt.Sprintf("%d%s", val, unit)
}

func usageCell(r usageRow, known bool, bg func(lipgloss.Style) lipgloss.Style) [2]string {
	if r.limit <= 0 {
		if known {
			return [2]string{
				bg(lipgloss.NewStyle().Foreground(th.PrimaryText)).Render(formatQuantity(r.usage, r.unit)),
				bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("no limit"),
			}
		}
		return [2]string{bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("no limit"), ""}
	}
	num, ratio := "--", 0.0
	if known {
		num = formatQuantity(r.usage, r.unit)
		ratio = float64(r.usage) / float64(r.limit)
	}
	value := bg(lipgloss.NewStyle().Foreground(th.PrimaryText)).Render(num) +
		bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("/"+formatQuantity(r.limit, r.unit))
	pct := bg(lipgloss.NewStyle().Foreground(levelColor(ratio))).Render(fmt.Sprintf("%4.0f%%", ratio*100))
	return [2]string{value, usageGraph(r.hist, sparkW) + pct}
}

// entryLineStarts returns the starting line offset of each tableEntry and total lines.
func entryLineStarts(entries []tableEntry) (starts []int, total int) {
	starts = make([]int, len(entries))
	for i, e := range entries {
		starts[i] = total
		if e.isHeading {
			total += headingH
		} else {
			total += itemH
		}
	}
	return starts, total
}

// tableLineOffset is the first visible body line, keeping the selected pod
// (and its group heading when first in a group) on screen.
func (m model) tableLineOffset(entries []tableEntry, l layout) int {
	starts, total := entryLineStarts(entries)
	viewH := max(l.tableH-tableHeaderH, 1)
	if total <= viewH {
		return 0
	}
	selTop, selBot := 0, itemH
	for i, e := range entries {
		if !e.isHeading && m.boxes[e.boxIdx].Name == m.selected {
			selTop = starts[i]
			if i > 0 && entries[i-1].isHeading {
				selTop = starts[i-1]
			}
			selBot = starts[i] + itemH
			break
		}
	}
	offset := selBot - viewH
	if selTop < offset {
		offset = selTop
	}
	return clamp(offset, 0, max(total-viewH, 0))
}

// cachedRow keeps both selection states so moving through the table does not
// redraw unchanged pods. Rows are populated only when they enter the viewport.
func (m model) cachedRow(index, inner int) []string {
	state := rowState{index: index, selected: m.boxes[index].Name == m.selected}
	if m.rowCache == nil {
		return m.renderRow(index, inner, state.selected)
	}
	key := rowKey{rev: m.dataRev, width: inner, theme: themeRevision}
	if m.rowCache.rows == nil || m.rowCache.key != key {
		m.rowCache.key = key
		m.rowCache.rows = make(map[rowState][]string)
	}
	if lines, ok := m.rowCache.rows[state]; ok {
		return lines
	}
	lines := m.renderRow(index, inner, state.selected)
	m.rowCache.rows[state] = lines
	return lines
}

func (m model) visibleRows(entries []tableEntry, inner, start, end int) []string {
	var out []string
	line := 0
	for _, e := range entries {
		h := itemH
		if e.isHeading {
			h = headingH
		}
		if line >= end {
			break
		}
		if line+h > start {
			var rows []string
			if e.isHeading {
				rows = []string{m.renderHeadingRow(e.heading, inner)}
			} else {
				rows = m.cachedRow(e.boxIdx, inner)
			}
			out = append(out, rows[max(start-line, 0):min(end-line, h)]...)
		}
		line += h
	}
	return out
}

func (m model) tableView(l layout) []string {
	inner := max(l.tableW-2, 1)
	out := make([]string, 0, l.tableH)
	out = append(out, m.tableHeader(inner), "")
	entries := m.tableEntries()
	if len(entries) == 0 {
		msg := "No pods"
		if len(m.boxes) > 0 {
			msg = "No " + strings.ToLower(sectionTitle(m.section)) + " pods"
		}
		out = append(out, "  "+dim(msg))
	} else {
		start := m.tableLineOffset(entries, l)
		end := start + max(l.tableH-tableHeaderH, 0)
		out = append(out, m.visibleRows(entries, inner, start, end)...)
	}
	for i := range out {
		out[i] = padTo(out[i], l.tableW)
	}
	for len(out) < l.tableH {
		out = append(out, strings.Repeat(" ", l.tableW))
	}
	return out[:l.tableH]
}

// rowAt maps a frame row inside the table body to the pod index drawn there.
func (m model) rowAt(y int) (int, bool) {
	l := m.layout()
	body := y - l.bodyTop - tableHeaderH
	if body < 0 || y >= l.bodyTop+l.tableH {
		return 0, false
	}
	entries := m.tableEntries()
	if len(entries) == 0 {
		return 0, false
	}
	line := m.tableLineOffset(entries, l) + body
	starts, _ := entryLineStarts(entries)
	for i, e := range entries {
		h := itemH
		if e.isHeading {
			h = headingH
		}
		if line >= starts[i] && line < starts[i]+h {
			if e.isHeading || line-starts[i] == itemH-1 {
				return 0, false
			}
			return e.boxIdx, true
		}
	}
	return 0, false
}
