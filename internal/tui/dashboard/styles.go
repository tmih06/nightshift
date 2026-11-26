package dashboard

import (
	"image/color"

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
	warnColor     color.Color
	badColor      color.Color

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

	winFocusedStyle  lipgloss.Style
	winActiveStyle   lipgloss.Style
	winInactiveStyle lipgloss.Style
	winActionStyle   lipgloss.Style
	winCloseStyle    lipgloss.Style
)

func init() { useTheme(*ghtheme.DefaultTheme) }

func useTheme(t ghtheme.Theme) {
	themeRevision++
	th = t
	styles = context.InitStyles(t)

	warmColor = t.SuccessText
	midGreenColor = t.SuccessText
	claimColor = styles.Colors.OpenPR
	warnColor = t.WarningText
	badColor = t.ErrorText

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

	winFocusedStyle = lipgloss.NewStyle().Background(t.SuccessText).Foreground(t.InvertedText).Bold(true)
	winActiveStyle = lipgloss.NewStyle().Background(t.SelectedBackground).Foreground(t.PrimaryText).Bold(true)
	winInactiveStyle = lipgloss.NewStyle().Foreground(t.SecondaryText)
	winActionStyle = lipgloss.NewStyle().Foreground(t.PrimaryText).Bold(true)
	winCloseStyle = lipgloss.NewStyle().Foreground(t.FaintText)
}

func dim(s string) string { return dimStyle.Render(s) }

func egressAccentColor() color.Color { return styles.Colors.MergedPR }
func systemAccentColor() color.Color { return th.WarningText }

func egressTextStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(egressAccentColor()) }
func groupHeadTextStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(th.FaintText)
}

func subTabTextStyle(active, focused bool) lipgloss.Style {
	if !active {
		return lipgloss.NewStyle().Foreground(th.FaintText)
	}
	fg := th.PrimaryText
	if focused {
		fg = th.SuccessText
	}
	return lipgloss.NewStyle().Bold(true).Underline(true).Foreground(fg)
}

func logActionStyle(deny bool) lipgloss.Style {
	fg := th.SuccessText
	if deny {
		fg = th.ErrorText
	}
	return lipgloss.NewStyle().Foreground(fg).Bold(true)
}
