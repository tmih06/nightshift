package dashboard

import "github.com/charmbracelet/lipgloss"

var (
	warmColor     = lipgloss.Color("42")
	midGreenColor = lipgloss.Color("112")
	claimColor    = lipgloss.Color("45")
	accent        = lipgloss.Color("39")
	warnColor     = lipgloss.Color("214")
	badColor      = lipgloss.Color("196")
	dimColor      = lipgloss.Color("241")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle      = lipgloss.NewStyle().Foreground(dimColor)
	warmStyle     = lipgloss.NewStyle().Foreground(warmColor)
	midGreenStyle = lipgloss.NewStyle().Foreground(midGreenColor)
	claimStyle    = lipgloss.NewStyle().Foreground(claimColor)
	warnStyle     = lipgloss.NewStyle().Foreground(warnColor)
	badStyle      = lipgloss.NewStyle().Foreground(badColor)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	nameStyle     = lipgloss.NewStyle().Bold(true)
	sectionStyle  = lipgloss.NewStyle().Bold(true)

	mdH1Style     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mdH2Style     = lipgloss.NewStyle().Bold(true).Foreground(claimColor)
	mdH3Style     = lipgloss.NewStyle().Bold(true).Foreground(warmColor)
	mdCodeStyle   = lipgloss.NewStyle().Foreground(warnColor)
	yamlKeyStyle  = lipgloss.NewStyle().Foreground(claimColor)
	yamlStrStyle  = lipgloss.NewStyle().Foreground(warmColor)
	yamlNumStyle  = lipgloss.NewStyle().Foreground(warnColor)
	yamlBoolStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("176"))

	// Indexed by stress bucket: rest, low, warn, high.
	levelStyles = [4]lipgloss.Style{warmStyle, midGreenStyle, warnStyle, badStyle}

	shellBarStyle  = lipgloss.NewStyle().Background(accent).Foreground(lipgloss.Color("16")).Bold(true)
	prefixBarStyle = lipgloss.NewStyle().Background(warnColor).Foreground(lipgloss.Color("16")).Bold(true)

	// Exactly one pane carries the accent border at any time.
	navFocusStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
	shellFocusStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1)
	rightPanelStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dimColor).Padding(0, 1)
)

func dim(s string) string { return dimStyle.Render(s) }
