package paint

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// OnBackground paints s on bg, putting the background back after every
// reset inside s, so a line built from several styles reads as one bar.
func OnBackground(s string, bg color.Color) string {
	if bg == nil || s == "" {
		return s
	}
	probe := lipgloss.NewStyle().Background(bg).Render("x")
	open, _, ok := strings.Cut(probe, "x")
	if !ok || open == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+open)
	s = strings.ReplaceAll(s, "\x1b[m", "\x1b[m"+open)
	return open + s + "\x1b[m"
}
