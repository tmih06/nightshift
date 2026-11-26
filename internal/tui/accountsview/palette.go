package accountsview

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

// The theme has no color ramp or per-family hues, so the meters, the calendar,
// and the model families bring their own, each for light and dark terminals.

func adaptive(light, dark string) color.Color {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}

// trackColor is the unfilled part of a meter.
func trackColor() color.Color { return adaptive("#e4e4e4", "#3a3a3a") }

// heatColor is GitHub's contribution ramp: level 0 is an idle day.
func heatColor(level int) color.Color {
	switch level {
	case 1:
		return adaptive("#9be9a8", "#0e4429")
	case 2:
		return adaptive("#40c463", "#006d32")
	case 3:
		return adaptive("#30a14e", "#26a641")
	case 4:
		return adaptive("#216e39", "#39d353")
	default:
		return adaptive("#ebedf0", "#2d333b")
	}
}

// familyColor tells model families apart; other names stay neutral.
func familyColor(family string) color.Color {
	switch family {
	case familyClaude, familyOpus:
		return adaptive("#b85c38", "#e08a64")
	case familySonnet:
		return adaptive("#8a6d1d", "#d9b45b")
	case familyHaiku, familyFable:
		return adaptive("#9c3d78", "#d787b7")
	case familyGemini:
		return adaptive("#1a63d1", "#6ea8fe")
	case familyGPT:
		return adaptive("#0b7a5f", "#2fc7a1")
	default:
		return adaptive("#555555", "#bbbbbb")
	}
}
