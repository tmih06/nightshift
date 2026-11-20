// Package comp holds the widgets shared across screens.
package comp

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/zenocto/tui/paint"
	"github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

// Tab is one entry in a pane's heading. Badge follows the label muted and is skipped when empty.
type Tab struct {
	Label string
	Badge string
}

// Pane is a region of the screen: a heading row, content, and a closing
// row, with one column of edge on either side. nightshift paints it the way
// gh-dash paints its views: a heading bar on the selected background, a
// single rule between side-by-side panes, and no box. A pane with only a
// Header is a card in the conversation; Boxed keeps a frame for overlays,
// which float over other content and need one. The geometry (Chrome,
// Above, InnerWidth, InnerHeight) is zen-octo's, so cursors and mouse hits
// computed against it still land.
type Pane struct {
	theme   theme.Theme
	title   string
	header  string
	index   int
	tabs    []Tab
	active  int
	footer  string
	focused bool
	ruled   bool
	boxed   bool
	width   int
	height  int
}

// NewPane returns an unsized pane.
func NewPane(th theme.Theme) Pane {
	return Pane{theme: th}
}

// Title sets the heading. Tabs take precedence over it.
func (p Pane) Title(s string) Pane {
	p.title = s
	return p
}

// Header sets a heading row inside the pane, set off from the content. s is rendered as given.
func (p Pane) Header(s string) Pane {
	p.header = s
	return p
}

// Chrome is the lines the pane spends on its heading and closing rows and any header and rule.
func (p Pane) Chrome() int {
	if p.header == "" {
		return 2
	}
	return 4
}

// Above is the lines drawn before the content: the heading row, and the header and rule where they fit.
func (p Pane) Above() int {
	if p.width < 2 || p.height < 2 {
		return 0
	}
	if p.header == "" || p.InnerHeight() < 3 {
		return 1
	}
	return 3
}

// Index numbers the pane for the keys that jump to it. gh-dash's look has
// no room for the bracketed number in a heading, so it is kept but not drawn.
func (p Pane) Index(n int) Pane {
	p.index = n
	return p
}

func (p Pane) Tabs(tabs []Tab, active int) Pane {
	p.tabs, p.active = tabs, active
	return p
}

// Footer sets the right-aligned text: the scroll position on a region, the
// key hints on a card.
func (p Pane) Footer(s string) Pane {
	p.footer = s
	return p
}

func (p Pane) Focus(v bool) Pane {
	p.focused = v
	return p
}

// Ruled draws gh-dash's single rule down the pane's left edge, for a pane
// that sits to the right of another.
func (p Pane) Ruled(v bool) Pane {
	p.ruled = v
	return p
}

// Boxed frames the pane with square corners, for content drawn over the screen.
func (p Pane) Boxed(v bool) Pane {
	p.boxed = v
	return p
}

// Size sets the pane's outer dimensions, edges included.
func (p Pane) Size(width, height int) Pane {
	p.width, p.height = width, height
	return p
}

func (p Pane) InnerWidth() int { return max(0, p.width-2) }

func (p Pane) InnerHeight() int { return max(0, p.height-2) }

func (p Pane) card() bool {
	return !p.boxed && p.header != "" && p.title == "" && len(p.tabs) == 0
}

// Render draws content inside the pane, padding it with plain spaces or clipping it to the pane's size.
func (p Pane) Render(content string) string {
	if p.width < 2 || p.height < 2 {
		return ""
	}

	lines := make([]string, 0, p.height)
	lines = append(lines, p.topRow())

	rows := p.InnerHeight()
	if p.header != "" && rows >= 3 {
		lines = append(lines, p.row(p.header), p.rule())
		rows -= 2
	}

	if body := p.body(content, rows); body != "" {
		lines = append(lines, body)
	}
	return strings.Join(append(lines, p.bottomRow()), "\n")
}

func (p Pane) edgeStyle() lipgloss.Style {
	if p.boxed {
		return lipgloss.NewStyle().Foreground(p.theme.Accent)
	}
	return lipgloss.NewStyle().Foreground(p.theme.BorderSubtleOrBorder())
}

// leftEdge is the column before the content: the rule between panes, the
// focus bar on a lit card, the frame on a box, or a space.
func (p Pane) leftEdge() string {
	switch {
	case p.boxed:
		return p.edgeStyle().Render("│")
	case p.card() && p.focused:
		return lipgloss.NewStyle().Foreground(p.theme.Accent).Render("▎")
	case p.ruled:
		return p.edgeStyle().Render("│")
	}
	return " "
}

func (p Pane) rightEdge() string {
	if p.boxed {
		return p.edgeStyle().Render("│")
	}
	return " "
}

func (p Pane) rule() string {
	if ClassicChrome {
		return p.classicRule()
	}
	if p.boxed {
		return p.edgeStyle().Render("├" + strings.Repeat("─", p.InnerWidth()) + "┤")
	}
	if p.card() {
		return p.leftEdge() + strings.Repeat(" ", p.InnerWidth()) + p.rightEdge()
	}
	faint := lipgloss.NewStyle().Foreground(p.theme.BorderMutedOrSubtle())
	return p.leftEdge() + faint.Render(strings.Repeat("─", p.InnerWidth())) + p.rightEdge()
}

func (p Pane) row(line string) string {
	if ClassicChrome {
		return p.classicRow(line)
	}
	inner := p.InnerWidth()
	if ansi.StringWidth(line) > inner {
		line = ansi.Truncate(line, inner, "")
	}
	return p.leftEdge() + line + strings.Repeat(" ", max(0, inner-ansi.StringWidth(line))) + p.rightEdge()
}

func (p Pane) topRow() string {
	if ClassicChrome {
		return p.classicTop()
	}
	if p.boxed {
		return p.boxTop()
	}
	if p.card() {
		return strings.Repeat(" ", p.width)
	}

	inner := p.InnerWidth()
	bg := p.theme.SelectedBackground
	bar := lipgloss.NewStyle()
	if bg != nil {
		bar = bar.Background(bg)
	}

	label := p.tabStrip()
	if label == "" && p.title != "" {
		titleStyle := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle())
		if p.focused {
			titleStyle = lipgloss.NewStyle().Foreground(p.theme.Text).Bold(true)
		}
		label = titleStyle.Render(p.title)
	}
	right := ""
	if p.footer != "" {
		right = lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle()).Render(p.footer)
	}

	line := " " + label
	gap := inner - ansi.StringWidth(line) - ansi.StringWidth(right) - 1
	if gap < 1 {
		line = ansi.Truncate(line, max(0, inner-ansi.StringWidth(right)-2), "")
		gap = max(0, inner-ansi.StringWidth(line)-ansi.StringWidth(right)-1)
	}
	line += strings.Repeat(" ", gap) + right + " "
	if w := ansi.StringWidth(line); w > inner {
		line = ansi.Truncate(line, inner, "")
	} else {
		line += strings.Repeat(" ", inner-w)
	}

	lead := " "
	if p.ruled {
		lead = p.edgeStyle().Render("│")
	}
	if bg != nil {
		line = paint.OnBackground(line, bg)
	}
	return lead + line + bar.Render(" ")
}

func (p Pane) bottomRow() string {
	if ClassicChrome {
		return p.classicBottom()
	}
	if p.boxed {
		return p.boxBottom()
	}
	inner := p.InnerWidth()
	if p.card() {
		faint := lipgloss.NewStyle().Foreground(p.theme.BorderMutedOrSubtle())
		if p.footer == "" {
			return " " + faint.Render(strings.Repeat("─", inner)) + " "
		}
		hints := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle()).Render(" " + p.footer + " ")
		if ansi.StringWidth(hints) > inner-2 {
			hints = ansi.Truncate(hints, max(0, inner-2), "")
		}
		fill := max(0, inner-ansi.StringWidth(hints)-1)
		return " " + faint.Render(strings.Repeat("─", fill)) + hints + faint.Render("─") + " "
	}
	return p.leftEdge() + strings.Repeat(" ", inner) + p.rightEdge()
}

func (p Pane) boxTop() string {
	style := p.edgeStyle()
	mid := p.width - 2
	label := p.tabStrip()
	if label == "" && p.title != "" {
		label = lipgloss.NewStyle().Foreground(p.theme.Text).Bold(true).Render(" " + p.title + " ")
	}
	if ansi.StringWidth(label) > max(0, mid-1) {
		label = ansi.Truncate(label, max(0, mid-1), "")
	}
	used := 1 + ansi.StringWidth(label)
	return style.Render("┌─") + label + style.Render(strings.Repeat("─", max(0, mid-used))+"┐")
}

func (p Pane) boxBottom() string {
	style := p.edgeStyle()
	mid := p.width - 2
	if p.footer == "" {
		return style.Render("└" + strings.Repeat("─", mid) + "┘")
	}
	footer := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle()).Render(p.footer)
	if ansi.StringWidth(footer) > max(0, mid-1) {
		footer = ansi.Truncate(footer, max(0, mid-1), "")
	}
	fill := max(0, mid-ansi.StringWidth(footer)-1)
	return style.Render("└"+strings.Repeat("─", fill)) + footer + style.Render("─┘")
}

// tabStrip is gh-dash's section tabs: the active one bold, the rest muted,
// split by a faint bar.
func (p Pane) tabStrip() string {
	if len(p.tabs) == 0 {
		return ""
	}

	activeStyle := lipgloss.NewStyle().Foreground(p.theme.Text).Bold(true)
	idleStyle := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle())
	badgeStyle := lipgloss.NewStyle().Foreground(p.theme.MutedOrSubtle())
	sep := lipgloss.NewStyle().Foreground(p.theme.BorderSubtleOrBorder()).Render("  |  ")

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

func (p Pane) body(content string, rows int) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, rows)
	for i := range rows {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, p.row(line))
	}
	return strings.Join(out, "\n")
}
