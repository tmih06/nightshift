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

	leftPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).Padding(0, 1)
	rightPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(dimColor).Padding(0, 1)
)

func dim(s string) string { return dimStyle.Render(s) }
