package dashboard

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme. Warm pool entries are green, claimed entries cyan, warnings
// yellow, errors red, and the focused left pane border carries the
// accent.
var (
	warmColor  = lipgloss.Color("42")
	claimColor = lipgloss.Color("45")
	accent     = lipgloss.Color("39")
	warnColor  = lipgloss.Color("214")
	badColor   = lipgloss.Color("196")
	dimColor   = lipgloss.Color("241")

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle     = lipgloss.NewStyle().Foreground(dimColor)
	warmStyle    = lipgloss.NewStyle().Foreground(warmColor)
	claimStyle   = lipgloss.NewStyle().Foreground(claimColor)
	warnStyle    = lipgloss.NewStyle().Foreground(warnColor)
	badStyle     = lipgloss.NewStyle().Foreground(badColor)
	cursorStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	nameStyle    = lipgloss.NewStyle().Bold(true)
	sectionStyle = lipgloss.NewStyle().Bold(true)

	// Markdown and YAML styles for warm sandbox inspection.
	mdH1Style     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mdH2Style     = lipgloss.NewStyle().Bold(true).Foreground(claimColor)
	mdH3Style     = lipgloss.NewStyle().Bold(true).Foreground(warmColor)
	mdCodeStyle   = lipgloss.NewStyle().Foreground(warnColor)
	yamlKeyStyle  = lipgloss.NewStyle().Foreground(claimColor)
	yamlStrStyle  = lipgloss.NewStyle().Foreground(warmColor)
	yamlNumStyle  = lipgloss.NewStyle().Foreground(warnColor)
	yamlBoolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))

	// Exactly one pane carries the accent border: the nav pane while
	// the dashboard has focus, the shell pane while bash has it. The
	// header boxes stay dim in both modes.
	navFocusStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).Padding(0, 1)
	shellFocusStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).Padding(0, 1)
	rightPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(dimColor).Padding(0, 1)
)

func dim(s string) string { return dimStyle.Render(s) }
