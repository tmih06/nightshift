// The view. Left pane: navigation, warm and claimed sandboxes in
// separate sections, its width set by dragging the splitter. Right
// column: a header pane with the selection's essential data and a
// shell pane embedding the sandbox's tmux session, which never takes
// over the whole terminal. Bordered panes get their content filled to
// an exact height, never a Height on the style, and every string is
// truncated to the pane's inner width.
package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tmih06/nightshift/internal/cluster"
)

const (
	// stackedBelow is the width where the panes stop fitting side by
	// side.
	stackedBelow = 50
	minLeftW     = 15
	minRightW    = 20
)

// layout is the one source of truth for pane geometry: the view, the
// mouse hit tests, and the tmux pane size all read it, so they cannot
// disagree.
type layout struct {
	leftW, leftH int
	colW         int // right column outer width
	infoW        int // info box outer width, fitted to its text
	usageW       int // usage box outer width, the rest of the column
	headerH      int
	shellH       int
	stacked      bool
	shellTop     int // absolute row where the shell pane starts
}

// layout derives the geometry. Golden rule 4: the drag stores a
// clamped override, and every other size follows from weights.
func (m model) layout() layout {
	panelsH := max(m.height-2, 3) // title line plus status line
	var l layout
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
		l.headerH = headerHeight(rest)
		l.shellH = max(rest-l.headerH, 3)
		l.infoW, l.usageW = m.headerBoxWidths(l.colW)
		l.shellTop = contentStartY + l.leftH + l.headerH
		return l
	}
	l.leftW = m.leftWidth
	if l.leftW <= 0 {
		l.leftW = m.fitLeftWidth()
	}
	l.leftW = clamp(l.leftW, minLeftW, max(m.width-minRightW, minLeftW))
	l.leftH = panelsH
	l.colW = max(m.width-l.leftW-1, 1)
	l.headerH = headerHeight(panelsH)
	l.shellH = max(panelsH-l.headerH, 3)
	l.infoW, l.usageW = m.headerBoxWidths(l.colW)
	l.shellTop = contentStartY + l.headerH
	return l
}

// headerBoxWidths splits the header column between the two bento boxes.
// The info box gets exactly what its text needs, never truncating, and
// the usage box takes the remainder for charts; both keep a minimum.
func (m model) headerBoxWidths(colW int) (infoW, usageW int) {
	infoW = 0
	for _, ln := range m.headerInfoLines() {
		// Width(n) gives outer n+2 on this bordered, padded style, so
		// the box needs the text width plus the padding and border
		// columns: +4.
		if w := lipgloss.Width(ln) + 4; w > infoW {
			infoW = w
		}
	}
	// The info box never cuts text: it takes exactly what its longest
	// line needs. Only when the column is genuinely too narrow does the
	// usage box shrink to its minimum, and then the info box itself
	// wraps, which the pane height covers.
	infoW = clamp(infoW, 16, max(colW-16, 16))
	usageW = max(colW-infoW-1, 12)
	return infoW, usageW
}

// fitLeftWidth sizes the sidebar to its content: the longest entry and
// the section headers, so no name truncates at the default width.
func (m model) fitLeftWidth() int {
	w := 18 // room for a section header row
	for _, b := range m.boxes {
		if n := len([]rune(b.Name)) + 8; n > w {
			w = n
		}
	}
	return clamp(w, minLeftW, max(m.width-minRightW, minLeftW))
}

// headerHeight caps the header row at the usage box's four content
// rows (value row plus one graph row per metric) plus borders, and
// never more than half the column.
func headerHeight(avail int) int {
	return min(6, max(avail/2, 6))
}

// shellPaneSize is the embedded shell's inner size, what the tmux
// window is resized to.
func (m model) shellPaneSize() (int, int) {
	l := m.layout()
	return max(l.colW-4, 1), max(l.shellH-2, 1)
}

// pane fills lines to innerH and renders them in the bordered style.
// Measured lipgloss semantics: Width(n) with this style's border and
// padding yields an outer width of n+2, so a pane of outer width W
// passes Width(W-2) and its text area is W-4.
func pane(style lipgloss.Style, lines []string, outerW, outerH int) string {
	innerH := max(outerH-2, 1)
	for len(lines) < innerH {
		lines = append(lines, "")
	}
	if len(lines) > innerH {
		lines = lines[:innerH]
	}
	return style.Width(outerW - 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// leftLines renders the navigation lines and maps every line that
// holds an entry to its index in m.boxes. One function backs the view
// and the mouse so the two cannot disagree.
func (m model) leftLines() ([]string, map[int]int) {
	l := m.layout()
	innerW := max(l.leftW-4, 1)
	targets := map[int]int{}
	var lines []string
	var warm, claimed []int
	for i, b := range m.boxes {
		if b.OwnerKind == "SandboxWarmPool" {
			warm = append(warm, i)
		} else {
			claimed = append(claimed, i)
		}
	}

	entry := func(i int) string {
		b := m.boxes[i]
		marker := " "
		if b.Name == m.selected {
			marker = cursorStyle.Render("▶")
		}
		kindStyle := warmStyle
		if b.OwnerKind != "SandboxWarmPool" {
			kindStyle = claimStyle
		}
		text := truncate(b.Name, innerW-4)
		if b.Name == m.selected {
			text = cursorStyle.Render(text)
		} else {
			text = kindStyle.Render(text)
		}
		sess := dim("○")
		if m.sessions[b.Name] {
			sess = warmStyle.Render("●")
		}
		return marker + " " + text + " " + sess
	}

	if len(m.boxes) == 0 {
		lines = append(lines, " "+dim("no sandboxes…"))
		return lines, targets
	}
	lines = append(lines, " "+sectionStyle.Render(fmt.Sprintf("WARM POOL (%d)", len(warm))))
	for _, i := range warm {
		targets[len(lines)] = i
		lines = append(lines, entry(i))
	}
	lines = append(lines, "")
	lines = append(lines, " "+sectionStyle.Render(fmt.Sprintf("CLAIMED (%d)", len(claimed))))
	for _, i := range claimed {
		targets[len(lines)] = i
		lines = append(lines, entry(i))
	}
	return lines, targets
}

// headerInfoLines renders the text of the info box, untruncated: the
// box sizes itself to fit this content exactly.
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
	sess := dim("tmux off")
	if m.sessions[b.Name] {
		sess = warmStyle.Render("tmux live")
	}

	return []string{
		" " + nameStyle.Render(b.Name),
		" " + dim("owner ") + b.Owner,
		" " + b.Phase + dim(" · ") + badgeStyle.Render(badge),
		" " + sess + dim(" · ") + ageString(b.Created),
	}
}

// headerUsageLines renders the text of the usage box, one metric per
// two-row group: a table-aligned value row with a bar under the
// percentage, then a btop-style history graph. Columns align across
// CPU and MEM, so the numbers read as a table.
func (m model) headerUsageLines() []string {
	b := m.selectedBox()
	if b == nil {
		return []string{dim("—")}
	}
	h := m.hist[b.Name]
	inner := max(m.layout().usageW-4, 1)
	var lines []string
	for _, row := range []struct {
		label string
		known bool
		usage int64
		limit int64
		unit  string
		hist  []float64
	}{
		{"CPU", m.metricsOK, b.CPUUsageMC, b.CPULimitMC, "m", h.cpu},
		{"MEM", m.metricsOK, b.MemUsageMiB, b.MemLimitMiB, "Mi", h.mem},
	} {
		lines = append(lines, usageLines(row.label, row.known, row.usage, row.limit, row.unit, row.hist, inner)...)
	}
	return lines
}

// usageLines renders one metric's group: the table value row, then the
// braille history graph rows. The fixed columns are the table: label,
// value, percent, bar.
func usageLines(label string, known bool, usage, limit int64, unit string, hist []float64, width int) []string {
	if limit <= 0 {
		value := fmt.Sprintf(" %-4s %-18s", dim(label), dim("no limit"))
		return []string{value, usageGraph(nil, width)}
	}
	num := "--"
	ratio := 0.0
	if known {
		num = fmt.Sprintf("%d%s", usage, unit)
		ratio = float64(usage) / float64(limit)
	}
	value := fmt.Sprintf("%s / %d%s", num, limit, unit)
	text := fmt.Sprintf(" %-4s %-19s %3.0f%%", dim(label), value, ratio*100)
	// One separator column between the percent and the bar; the total
	// row lands on exactly width columns.
	barW := width - lipgloss.Width(text) - 1
	var valueRow string
	if barW > 0 {
		valueRow = text + " " + bar(ratio, barW)
	} else {
		valueRow = lipgloss.NewStyle().MaxWidth(width).Render(text)
	}
	return []string{valueRow, usageGraph(hist, width)}
}

// usageGraph renders one metric's history as a single-row braille
// graph, btop style: each character cell holds two dot columns by four
// dot rows, and a sample fills its column from the baseline up to its
// own height — one dot at rest, four dots at the limit — so stress
// reads as a tall stack and calm as a short one. The height is the
// sample's ratio against the limit, not a scale of the window's own
// peak: 45% of the limit draws two dot rows even when it is the
// highest thing on screen, and the wave keeps its true shape. Each
// cell is colored on the green-to-red ramp by its stronger sample, so
// a spike reads red wherever it happened. A young history grows from
// the right edge.
func usageGraph(hist []float64, width int) string {
	if width < 1 {
		return ""
	}
	if len(hist) == 0 {
		return dim(strings.Repeat("\u00b7", width))
	}
	dotCols := 2 * width
	vals := hist
	if len(vals) > dotCols {
		vals = vals[len(vals)-dotCols:]
	}
	const dotRows = 4
	// A young history grows from the right edge, the way a scrolling
	// chart fills, so the first cells stay blank.
	firstCell := width - (len(vals)+1)/2
	type graphCell struct {
		mask rune
		maxV float64
	}
	cells := make([]graphCell, width)
	for j, v := range vals {
		// Absolute scale: the sample's ratio against the limit is its
		// dot height, one row minimum so the baseline stays visible.
		h := int(v*float64(dotRows) + 0.5)
		if h < 1 {
			h = 1
		}
		if h > dotRows {
			h = dotRows
		}
		c := firstCell + j/2
		col := j % 2
		// A column, not a point: every dot row from the baseline up to
		// the sample's height lights up.
		for top := dotRows - h; top <= dotRows-1; top++ {
			cells[c].mask |= dotBit(col, top)
		}
		if v > cells[c].maxV {
			cells[c].maxV = v
		}
	}
	var b strings.Builder
	for c, cell := range cells {
		if c < firstCell {
			b.WriteByte(' ')
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(levelColor(cell.maxV)).
			Render(string(0x2800 + cell.mask)))
	}
	return b.String()
}

// levelColor maps a usage ratio to a color: green at rest, yellow as
// it climbs, red at the top. The graph uses this per sample, so each
// point carries its own stress color.
func levelColor(ratio float64) lipgloss.Color {
	switch {
	case ratio >= 0.9:
		return badColor
	case ratio >= 0.6:
		return warnColor
	case ratio >= 0.3:
		return lipgloss.Color("112") // light green
	default:
		return warmColor
	}
}

// dotBit maps a dot position inside a braille cell to its bit in the
// Unicode pattern. A cell is two dot columns by four dot rows: dots 1,
// 2, 3 run down the left column, 4, 5, 6 down the right, and 7, 8 sit
// on the fourth row. Shifting a column's base bit would work only for
// the top three rows, so the fourth row is its own case.
func dotBit(col, row int) rune {
	if row < 3 {
		return 1 << rune(col*3+row)
	}
	return 1 << rune(6+col)
}

func (m model) shellLinesView() []string {
	l := m.layout()
	innerW := max(l.colW-4, 1)
	innerH := max(l.shellH-2, 1)
	b := m.selectedBox()

	var title string
	switch {
	case b == nil:
		title = dim("shell")
	case !m.sessions[b.Name]:
		title = dim("no session — claim with c, or enter on a claimed sandbox")
	case m.shellFocus:
		title = cursorStyle.Render("▶ bash — C-b q leaves")
	default:
		title = dim("bash — click here or enter to focus")
	}
	lines := []string{" " + title}

	if b != nil && m.sessions[b.Name] && m.shellPod == b.Name {
		content := m.shellLines
		if len(content) > innerH-1 {
			content = content[len(content)-(innerH-1):]
		}
		for _, ln := range content {
			lines = append(lines, " "+truncate(ln, innerW-1))
		}
	}
	return lines
}

// bar draws a usage bar of the given width: the filled part colored by
// the level, the empty part dim, so the whole bar stays visible.
func bar(ratio float64, width int) string {
	filled := int(ratio * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	filledPart := lipgloss.NewStyle().Foreground(barColor(ratio)).
		Render(strings.Repeat("█", filled))
	emptyPart := dim(strings.Repeat("░", width-filled))
	return filledPart + emptyPart
}

func barColor(ratio float64) lipgloss.Color {
	switch {
	case ratio >= 0.9:
		return badColor
	case ratio >= 0.6:
		return warnColor
	default:
		return warmColor
	}
}

func truncate(s string, max int) string {
	if max < 1 {
		return "…"
	}
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
		return lipgloss.NewStyle().MaxWidth(m.width).Render(msg)
	}
	// Shell mode fills the bar so the mode is unmistakable, and carries
	// its own tips: no keybind escapes the sandbox.
	var line string
	var bg lipgloss.Color
	switch {
	case m.shellFocus && m.prefix:
		line, bg = "C-b armed · q leaves bash, any other key cancels", warnColor
	case m.shellFocus:
		line, bg = "C-b q leaves bash · every key goes to the sandbox", accent
	default:
		return lipgloss.NewStyle().MaxWidth(m.width).Render(
			dim("j/k move · enter bash (claimed) · F fullscreen · c claim+bash · x destroy · q quit · drag resizes"))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).
		Background(bg).Foreground(lipgloss.Color("16")).Bold(true).
		Render(" " + line + " ")
}

func (m model) View() string {
	if !m.loaded && m.err == nil {
		return "loading sandboxes…"
	}

	title := titleStyle.Render("nightshift") + "  " +
		dim(fmt.Sprintf("%d sandboxes · ns %s", len(m.boxes), cluster.Namespace))

	l := m.layout()
	leftLines, _ := m.leftLines()
	// Exactly one accent border: the nav pane in navigation, the shell
	// pane in shell focus, never both.
	leftStyle := navFocusStyle
	if m.shellFocus {
		leftStyle = rightPanelStyle
	}
	left := pane(leftStyle, leftLines, l.leftW, l.leftH)

	infoBox := pane(rightPanelStyle, m.headerInfoLines(), l.infoW, l.headerH)
	usageBox := pane(rightPanelStyle, m.headerUsageLines(), l.usageW, l.headerH)
	headerRow := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, usageBox)
	shellStyle := rightPanelStyle
	if m.shellFocus {
		shellStyle = shellFocusStyle
	}
	shell := pane(shellStyle, m.shellLinesView(), l.colW, l.shellH)
	rightCol := lipgloss.JoinVertical(lipgloss.Left, headerRow, shell)

	var body string
	if l.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, left, rightCol)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, rightCol)
	}
	return title + "\n" + body + "\n" + m.statusLine()
}
