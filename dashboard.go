package main

// The view layer. Every sandbox renders as one bordered box in a grid:
// name, owner badge, pod phase, and two usage bars measured against the
// container limits. Warm boxes sit first and left, claimed ones follow.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	refreshEvery = 2 * time.Second
	boxWidth     = 36 // outer width, border and padding included
	barWidth     = 11
	gap          = 1
)

type model struct {
	boxes     []sandbox
	metricsOK bool
	err       error
	loaded    bool
	width     int
}

func initialModel() model {
	return model{}
}

type tickMsg struct{}

type loadedMsg struct {
	boxes     []sandbox
	metricsOK bool
	err       error
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func fetchCmd() tea.Cmd {
	return func() tea.Msg {
		boxes, metricsOK, err := fetchSandboxData()
		return loadedMsg{boxes: boxes, metricsOK: metricsOK, err: err}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchCmd(), tickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, fetchCmd()
		}
	case tickMsg:
		return m, tea.Batch(fetchCmd(), tickCmd())
	case loadedMsg:
		m.boxes, m.metricsOK, m.err, m.loaded = msg.boxes, msg.metricsOK, msg.err, true
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	warmColor  = lipgloss.Color("42")  // green
	claimColor = lipgloss.Color("45")  // cyan
	warnColor  = lipgloss.Color("214") // yellow
	badColor   = lipgloss.Color("196") // red
)

func dim(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(s)
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

func resourceLine(label string, usageKnown bool, usage, limit int64, unit string) string {
	num := "--"
	if usageKnown {
		num = fmt.Sprintf("%d%s", usage, unit)
	}
	if limit <= 0 {
		return fmt.Sprintf("%s %-11s %s", label, num, dim("no limit"))
	}
	num = fmt.Sprintf("%s/%d%s", num, limit, unit)
	ratio := 0.0
	if usageKnown {
		ratio = float64(usage) / float64(limit)
	}
	styled := lipgloss.NewStyle().Foreground(barColor(ratio)).Render(bar(ratio, barWidth))
	return fmt.Sprintf("%s %-11s %s %3.0f%%", label, num, styled, ratio*100)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func boxView(b sandbox, metricsOK bool) string {
	badge, border := "warm", warmColor
	switch b.ownerKind {
	case "SandboxClaim":
		badge, border = "claimed", claimColor
	case "SandboxWarmPool":
	default:
		badge, border = b.ownerKind, warnColor
	}
	if b.phase != "Running" && b.phase != "…" {
		border = warnColor
	}

	inner := boxWidth - 4 // border plus padding
	name := lipgloss.NewStyle().Bold(true).MaxWidth(inner).Render(truncate(b.name, inner))
	owner := lipgloss.NewStyle().Foreground(border).Render(badge) + dim(" · ") + b.phase
	cpu := resourceLine("CPU", metricsOK, b.cpuUsageMC, b.cpuLimitMC, "m")
	mem := resourceLine("MEM", metricsOK, b.memUsageMiB, b.memLimitMiB, "Mi")

	content := lipgloss.JoinVertical(lipgloss.Left, name, owner, cpu, mem)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Padding(0, 1).
		Width(boxWidth - 2). // text area plus padding; the border reaches boxWidth
		Render(content)
}

func (m model) View() string {
	if !m.loaded && m.err == nil {
		return "loading sandboxes…"
	}

	head := titleStyle.Render("nightshift") + "  " +
		dim(fmt.Sprintf("%d sandboxes · ns %s", len(m.boxes), namespace))
	if m.err != nil {
		head += "\n" + lipgloss.NewStyle().Foreground(badColor).Render("fetch error: "+m.err.Error())
	}

	perRow := 2
	if m.width > 0 {
		perRow = m.width / (boxWidth + gap)
		if perRow < 1 {
			perRow = 1
		}
	}
	var rows []string
	for i := 0; i < len(m.boxes); i += perRow {
		end := min(i+perRow, len(m.boxes))
		var boxes []string
		for _, b := range m.boxes[i:end] {
			boxes = append(boxes, boxView(b, m.metricsOK))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, boxes...))
	}
	grid := lipgloss.JoinVertical(lipgloss.Left, rows...)
	if len(m.boxes) == 0 && m.err == nil {
		grid = dim(fmt.Sprintf("no sandboxes in namespace %s", namespace))
	}

	legend := lipgloss.NewStyle().Foreground(warmColor).Render("● warm") + "  " +
		lipgloss.NewStyle().Foreground(claimColor).Render("● claimed")
	footer := legend + dim("   · q quit · r refresh · "+refreshEvery.String()+" tick")
	if !m.metricsOK {
		footer += "\n" + lipgloss.NewStyle().Foreground(warnColor).
			Render("live usage unavailable: metrics-server not reachable, budgets only")
	}
	return head + "\n\n" + grid + "\n\n" + footer
}

// runOnce prints one dashboard frame without a terminal, the headless
// check of the whole pipeline against the live cluster.
func runOnce() {
	boxes, metricsOK, err := fetchSandboxData()
	m := model{boxes: boxes, metricsOK: metricsOK, err: err, loaded: true, width: 100}
	fmt.Println(m.View())
}
