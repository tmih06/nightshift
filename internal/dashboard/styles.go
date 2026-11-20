package dashboard

import (
	"image/color"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

// The sandbox tab paints from gh-dash's theme and styles, so it reads as one
// more gh-dash view. useTheme reassigns every style; New calls it.
var (
	th            ghtheme.Theme
	styles        context.Styles
	themeRevision uint64

	warmColor     color.Color
	midGreenColor color.Color
	claimColor    color.Color
	accent        color.Color
	warnColor     color.Color
	badColor      color.Color
	dimColor      color.Color

	titleStyle    lipgloss.Style
	dimStyle      lipgloss.Style
	warmStyle     lipgloss.Style
	midGreenStyle lipgloss.Style
	claimStyle    lipgloss.Style
	warnStyle     lipgloss.Style
	badStyle      lipgloss.Style
	nameStyle     lipgloss.Style

	mdH1Style     lipgloss.Style
	mdH2Style     lipgloss.Style
	mdH3Style     lipgloss.Style
	mdCodeStyle   lipgloss.Style
	yamlKeyStyle  lipgloss.Style
	yamlStrStyle  lipgloss.Style
	yamlNumStyle  lipgloss.Style
	yamlBoolStyle lipgloss.Style

	// Indexed by stress bucket: rest, low, warn, high.
	levelStyles [4]lipgloss.Style

	shellBarStyle  lipgloss.Style
	prefixBarStyle lipgloss.Style

	helpStyles help.Styles
)

func init() { useTheme(*ghtheme.DefaultTheme) }

func useTheme(t ghtheme.Theme) {
	themeRevision++
	th = t
	styles = context.InitStyles(t)

	warmColor = t.SuccessText
	midGreenColor = t.SuccessText
	claimColor = styles.Colors.OpenPR
	accent = styles.Colors.OpenPR
	warnColor = t.WarningText
	badColor = t.ErrorText
	dimColor = t.FaintText

	titleStyle = styles.Common.MainTextStyle
	dimStyle = styles.Common.FaintTextStyle
	warmStyle = lipgloss.NewStyle().Foreground(warmColor)
	midGreenStyle = lipgloss.NewStyle().Foreground(midGreenColor)
	claimStyle = lipgloss.NewStyle().Foreground(claimColor)
	warnStyle = lipgloss.NewStyle().Foreground(warnColor)
	badStyle = lipgloss.NewStyle().Foreground(badColor)
	nameStyle = lipgloss.NewStyle().Bold(true).Foreground(t.PrimaryText)

	mdH1Style = lipgloss.NewStyle().Bold(true).Foreground(t.PrimaryText)
	mdH2Style = lipgloss.NewStyle().Bold(true).Foreground(claimColor)
	mdH3Style = lipgloss.NewStyle().Bold(true).Foreground(t.SecondaryText)
	mdCodeStyle = lipgloss.NewStyle().Foreground(warnColor)
	yamlKeyStyle = lipgloss.NewStyle().Foreground(claimColor)
	yamlStrStyle = lipgloss.NewStyle().Foreground(t.SecondaryText)
	yamlNumStyle = lipgloss.NewStyle().Foreground(warnColor)
	yamlBoolStyle = lipgloss.NewStyle().Foreground(styles.Colors.MergedPR)

	levelStyles = [4]lipgloss.Style{warmStyle, midGreenStyle, warnStyle, badStyle}

	shellBarStyle = lipgloss.NewStyle().Background(t.SuccessText).Foreground(t.InvertedText).Bold(true)
	prefixBarStyle = lipgloss.NewStyle().Background(t.WarningText).Foreground(t.InvertedText).Bold(true)

	helpStyles = styles.Help.BubbleStyles
}

func dim(s string) string { return dimStyle.Render(s) }
