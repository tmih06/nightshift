// The view. Left pane: navigation, warm and claimed sandboxes in
// separate sections. Right pane: the inspect view of the selection.
// Bordered panes get their content filled to an exact height, never a
// Height on the style, and every string is truncated to the pane's
// inner width.
package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tmih06/nightshift/internal/cluster"
)

// stackedBelow is the width where the panes stop fitting side by side.
const stackedBelow = 50

// calculateLayout returns the left pane's outer width, its outer
// height, and whether the panes stack on narrow terminals. Golden rule
// 4: weights, not pixels. The left pane takes a third of the width.
func (m model) calculateLayout() (leftW, leftH int, stacked bool) {
	stacked = m.width < stackedBelow
	panelsH := max(m.height-2, 3) // title line plus status line
	if stacked {
		return m.width, panelsH / 2, true
	}
	leftW = max(m.width/3, 20)
	if leftW > m.width {
		leftW = m.width
	}
	return leftW, panelsH, false
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
	innerW := m.leftInnerWidth()
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

func (m model) leftInnerWidth() int {
	leftW, _, _ := m.calculateLayout()
	return max(leftW-4, 1)
}

func (m model) rightInnerWidth() int {
	leftW, _, stacked := m.calculateLayout()
	if stacked {
		return max(m.width-4, 1)
	}
	return max(m.width-leftW-1-4, 1)
}

func (m model) rightLines() []string {
	innerW := m.rightInnerWidth()
	b := m.selectedBox()
	if b == nil {
		return []string{" " + dim("nothing selected")}
	}

	sess := dim("none — enter starts one")
	if m.sessions[b.Name] {
		sess = warmStyle.Render("live (" + sessionName(b.Name) + ")")
	}

	rows := []string{stringPair("kind", b.OwnerKind), stringPair("owner", b.Owner),
		stringPair("phase", b.Phase), stringPair("age", ageString(b.Created)),
		pair("tmux", sess)}

	var lines []string
	lines = append(lines, " "+nameStyle.Render(truncate(b.Name, innerW-2)))
	lines = append(lines, "")
	for _, r := range rows {
		lines = append(lines, " "+r)
	}
	lines = append(lines, "")
	lines = append(lines, resourceLine("CPU", m.metricsOK, b.CPUUsageMC, b.CPULimitMC, "m", innerW))
	lines = append(lines, resourceLine("MEM", m.metricsOK, b.MemUsageMiB, b.MemLimitMiB, "Mi", innerW))
	lines = append(lines, "")
	if b.OwnerKind == "SandboxClaim" {
		lines = append(lines, " "+dim("[enter] bash   [x] destroy claim"))
	} else {
		lines = append(lines, " "+dim("[enter] bash   [c] claim a warm sandbox"))
	}
	return lines
}

// pair renders a label and value row, truncating the value to the pane
// width. label+value both plain strings.
func pair(label, value string) string {
	return dimStyle.Render(fmt.Sprintf("%-6s", label)) + " " + value
}

// stringPair truncates the value before pairing.
func stringPair(label, value string) string {
	return pair(label, truncate(value, 40))
}

func sessionName(pod string) string {
	return "sbx-" + pod
}

// resourceLine renders one usage line: label, usage/limit, a bar, and
// a percentage, padded to exactly innerW columns so it never wraps.
// Fixed columns: leading space, 6 label, 11 numbers, and 4 percent,
// three separators; the bar takes the rest.
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
	keys := "j/k move · enter bash · c claim · x destroy · r refresh · q quit"
	line := dim(keys)
	msg := m.status
	if m.err != nil {
		msg = badStyle.Render("fetch error: " + m.err.Error())
	} else if msg == "" && !m.metricsOK && m.loaded {
		msg = warnStyle.Render("live usage unavailable: metrics-server not reachable")
	}
	if msg != "" {
		line += "   " + msg
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

	leftW, leftH, stacked := m.calculateLayout()
	leftLines, _ := m.leftLines()
	left := pane(leftPanelStyle, leftLines, leftW, leftH)

	var body string
	if stacked {
		rightH := max(m.height-2-leftH, 3)
		right := pane(rightPanelStyle, m.rightLines(), m.width, rightH)
		body = lipgloss.JoinVertical(lipgloss.Left, left, right)
	} else {
		rightW := max(m.width-leftW-1, 1)
		right := pane(rightPanelStyle, m.rightLines(), rightW, leftH)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
	return title + "\n" + body + "\n" + m.statusLine()
}
