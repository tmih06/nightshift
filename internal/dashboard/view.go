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
		l.shellTop = contentStartY + l.leftH + l.headerH
		return l
	}
	l.leftW = m.leftWidth
	if l.leftW <= 0 {
		l.leftW = m.width / 3
	}
	l.leftW = clamp(l.leftW, minLeftW, max(m.width-minRightW, minLeftW))
	l.leftH = panelsH
	l.colW = max(m.width-l.leftW-1, 1)
	l.headerH = headerHeight(panelsH)
	l.shellH = max(panelsH-l.headerH, 3)
	l.shellTop = contentStartY + l.headerH
	return l
}

// headerHeight sizes the compact header: five content rows plus the
// border, and never more than half the column.
func headerHeight(avail int) int {
	return min(7, max(avail/2, 3))
}

// shellPaneSize is the embedded shell's inner size, what the tmux
// window is resized to.
func (m model) shellPaneSize() (int, int) {
	l := m.layout()
	return max(l.colW-4, 1), max(l.shellH-2, 1)
}

// pane fills lines to innerH and renders them in the bordered style.
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
		if i == m.selected {
			marker = cursorStyle.Render("▶")
		}
		kindStyle := warmStyle
		if b.OwnerKind != "SandboxWarmPool" {
			kindStyle = claimStyle
		}
		text := truncate(b.Name, innerW-6)
		if i == m.selected {
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

// headerLines renders the compact inspect header: identity on one
// row, provenance on the next, the two usage bars, and one hint row.
func (m model) headerLines() []string {
	l := m.layout()
	innerW := max(l.colW-4, 1)
	b := m.selectedBox()
	if b == nil {
		return []string{" " + dim("nothing selected")}
	}

	badge, badgeStyle := "warm", warmStyle
	if b.OwnerKind == "SandboxClaim" {
		badge, badgeStyle = "claimed", claimStyle
	} else if b.OwnerKind != "SandboxWarmPool" {
		badge, badgeStyle = b.OwnerKind, warnStyle
	}
	sess := dim("off")
	if m.sessions[b.Name] {
		sess = warmStyle.Render("live")
	}

	name := truncate(b.Name, max(innerW-len(badge)-len(b.Phase)-10, 4))
	row1 := " " + nameStyle.Render(name) + dim(" · ") + badgeStyle.Render(badge) +
		dim(" · ") + b.Phase
	owner := truncate(b.Owner, max(innerW-26, 4))
	row2 := " " + dim("owner ") + owner + dim(" · age ") + ageString(b.Created) +
		dim(" · tmux ") + sess
	row3 := resourceLine("CPU", m.metricsOK, b.CPUUsageMC, b.CPULimitMC, "m", innerW)
	row4 := resourceLine("MEM", m.metricsOK, b.MemUsageMiB, b.MemLimitMiB, "Mi", innerW)

	hint := "[enter] bash · [F] fullscreen · [c] claim"
	if b.OwnerKind == "SandboxClaim" {
		hint = "[enter] bash · [F] fullscreen · [x] destroy claim"
	}
	row5 := " " + dim(hint)
	return []string{row1, row2, row3, row4, row5}
}

// shellLinesView renders the embedded shell: a title row, then the
// captured pane content, bottom-aligned and truncated. The capture is
// shown only while it matches the selection.
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
		title = dim("no session — enter starts bash")
	case m.shellFocus:
		title = cursorStyle.Render("▶ bash — keys go here, esc leaves")
	default:
		title = dim("bash — click here or press enter to focus")
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

// pair renders a label and value row.
func pair(label, value string) string {
	return dimStyle.Render(fmt.Sprintf("%-6s", label)) + " " + value
}

// resourceLine renders one usage line, padded to exactly innerW
// columns so it never wraps. Fixed columns: leading space, 6 label, 11
// numbers, 4 percent, three separators; the bar takes the rest.
func resourceLine(label string, usageKnown bool, usage, limit int64, unit string, innerW int) string {
	num := "--"
	if usageKnown {
		num = fmt.Sprintf("%d%s", usage, unit)
	}
	if limit <= 0 {
		return pair(label, num+" "+dim("no limit"))
	}
	num = fmt.Sprintf("%s/%d%s", num, limit, unit)
	numCol := fmt.Sprintf("%-11s", num)

	barW := innerW - 25
	ratio := 0.0
	if usageKnown {
		ratio = float64(usage) / float64(limit)
	}
	var b string
	if barW > 0 {
		b = lipgloss.NewStyle().Foreground(barColor(ratio)).Render(bar(ratio, barW))
	}
	return fmt.Sprintf(" %s %s %s %3.0f%%", dim(fmt.Sprintf("%-6s", label)), numCol, b, ratio*100)
}

// bar draws a usage bar of the given width. The caller guards the ratio.
func bar(ratio float64, width int) string {
	filled := int(ratio * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
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
	// A message replaces the key hint instead of joining it: two full
	// strings do not fit one row.
	var line string
	switch {
	case msg != "":
		line = msg + dim("  ·  q quits")
	case m.shellFocus:
		line = dim("esc back · keys go to bash · C-b d works inside")
	default:
		line = dim("j/k move · enter bash · F fullscreen · c claim · x destroy · q quit · drag resizes")
	}
	// MaxWidth truncates by display width, ANSI-aware.
	return lipgloss.NewStyle().MaxWidth(m.width).Render(line)
}

func (m model) View() string {
	if !m.loaded && m.err == nil {
		return "loading sandboxes…"
	}

	title := titleStyle.Render("nightshift") + "  " +
		dim(fmt.Sprintf("%d sandboxes · ns %s", len(m.boxes), cluster.Namespace))

	l := m.layout()
	leftLines, _ := m.leftLines()
	left := pane(leftPanelStyle, leftLines, l.leftW, l.leftH)

	header := pane(rightPanelStyle, m.headerLines(), l.colW, l.headerH)
	shellStyle := rightPanelStyle
	if m.shellFocus {
		shellStyle = leftPanelStyle // accent border while keys land here
	}
	shell := pane(shellStyle, m.shellLinesView(), l.colW, l.shellH)
	rightCol := lipgloss.JoinVertical(lipgloss.Left, header, shell)

	var body string
	if l.stacked {
		body = lipgloss.JoinVertical(lipgloss.Left, left, rightCol)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, rightCol)
	}
	return title + "\n" + body + "\n" + m.statusLine()
}
