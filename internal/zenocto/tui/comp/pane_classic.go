package comp

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// ClassicChrome draws zen-octo's original rounded boxes instead of gh-dash's
// look, in exactly the same rows and columns. It exists for zen-octo's own
// tests, which find panes and cards on screen by their box-drawing glyphs;
// nightshift never sets it.
var ClassicChrome bool

func (p Pane) classicRule() string {
	style := p.classicBorderStyle()
	return style.Render("├" + strings.Repeat("─", p.InnerWidth()) + "┤")
}

func (p Pane) classicRow(line string) string {
	side := p.classicBorderStyle().Render("│")
	line = lipgloss.NewStyle().MaxWidth(p.InnerWidth()).Render(line)
	return side + line + strings.Repeat(" ", max(0, p.InnerWidth()-lipgloss.Width(line))) + side
}

func (p Pane) classicBorderStyle() lipgloss.Style {
	c := p.theme.BorderSubtleOrBorder()
	if p.focused {
		c = p.theme.Accent
	}
	return lipgloss.NewStyle().Foreground(c)
}

func (p Pane) classicTop() string {
	style := p.classicBorderStyle()
	mid := p.width - 2

	segments := []string{style.Render("─")}
	used := 1

	if p.index > 0 {
		badge := lipgloss.NewStyle().Foreground(p.theme.Accent).Render("[" + strconv.Itoa(p.index) + "]")
		segments = append(segments, badge, style.Render("─"))
		used += lipgloss.Width(badge) + 1
	}

	label := p.classicTabStrip()
	if label == "" && p.title != "" {
		label = lipgloss.NewStyle().Foreground(p.theme.Text).Bold(true).Render(p.title)
	}
	if label != "" {
		label = lipgloss.NewStyle().MaxWidth(max(0, mid-used)).Render(label)
		segments = append(segments, label)
		used += lipgloss.Width(label)
	}

	segments = append(segments, style.Render(strings.Repeat("─", max(0, mid-used))))
	return style.Render("╭") + strings.Join(segments, "") + style.Render("╮")
}

func (p Pane) classicBottom() string {
	style := p.classicBorderStyle()
	mid := p.width - 2

	if p.footer == "" {
		return style.Render("╰" + strings.Repeat("─", mid) + "╯")
	}

	footer := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle()).
		MaxWidth(max(0, mid-1)).Render(p.footer)
	fill := max(0, mid-lipgloss.Width(footer)-1)

	return style.Render("╰"+strings.Repeat("─", fill)) + footer + style.Render("─╯")
}

func (p Pane) classicTabStrip() string {
	if len(p.tabs) == 0 {
		return ""
	}

	activeStyle := lipgloss.NewStyle().Foreground(p.theme.Accent).Bold(true)
	idleStyle := lipgloss.NewStyle().Foreground(p.theme.Subtle)
	badgeStyle := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle())
	sep := badgeStyle.Render(" - ")

	parts := make([]string, 0, len(p.tabs))
	for i, tab := range p.tabs {
		style := idleStyle
		if i == p.active {
			style = activeStyle
		}
		part := style.Render(tab.Label)
		if tab.Badge != "" {
			part += badgeStyle.Render(" " + tab.Badge)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, sep)
}
