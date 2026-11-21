// Package dialog draws nightshift's dialogs and cards in gh-dash's look: a
// rounded box over the screen holding a title, a few faint lines, and a
// menu to pick from.
package dialog

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

// Option is one choice in a menu. A dangerous one is painted as an error.
type Option struct {
	Text   string
	Danger bool
}

// Field is an optional single-line text input drawn above the menu's options.
type Field struct {
	Label   string
	View    string // rendered input line; may contain ANSI styling
	Focused bool
	Err     string
}

// Menu is a dialog that asks for one of its options.
type Menu struct {
	Title        string
	Lines        []string
	Field        *Field
	OptionsLabel string
	Options      []Option
	Cursor       int
	Hint         string
}

// Move steps the cursor by delta, wrapping round.
func (m *Menu) Move(delta int) {
	if n := len(m.Options); n > 0 {
		m.Cursor = ((m.Cursor+delta)%n + n) % n
	}
}

// Placed is a menu laid out in the middle of a width×height area.
type Placed struct {
	Box        string
	X, Y, W, H int
	optionTop  int
	options    int
	hasField   bool
	fieldY     int
}

// Inside is whether x, y falls on the box.
func (p Placed) Inside(x, y int) bool {
	return x >= p.X && x < p.X+p.W && y >= p.Y && y < p.Y+p.H
}

// FieldY is the row of the menu's text field.
func (p Placed) FieldY() int { return p.fieldY }

// OnField is whether x, y falls on the menu's text field.
func (p Placed) OnField(x, y int) bool {
	return p.hasField && p.Inside(x, y) && y == p.fieldY
}

// OptionY is the row of option i.
func (p Placed) OptionY(i int) int { return p.optionTop + i }

// OptionAt is the option on row y, if x, y falls on one.
func (p Placed) OptionAt(x, y int) (int, bool) {
	i := y - p.optionTop
	return i, p.Inside(x, y) && i >= 0 && i < p.options
}

// Place lays m out in the middle of a width×height area.
func (m Menu) Place(th ghtheme.Theme, width, height int) Placed {
	inner := lipgloss.Width(m.Title)
	for _, s := range append(append([]string{m.Hint, m.OptionsLabel}, m.Lines...), optionTexts(m.Options)...) {
		inner = max(inner, lipgloss.Width(s))
	}
	if m.Field != nil {
		inner = max(inner, 44, lipgloss.Width(m.Field.Label), lipgloss.Width(m.Field.View), lipgloss.Width(m.Field.Err))
	}
	inner = min(inner, max(1, width-4))

	plain := lipgloss.NewStyle()
	lines := []string{Line(plain.Foreground(th.PrimaryText).Bold(true), m.Title, inner)}
	for _, l := range m.Lines {
		lines = append(lines, Line(plain.Foreground(th.FaintText), l, inner))
	}
	lines = append(lines, "")

	fieldRow := 0
	if m.Field != nil {
		labelStyle := plain.Foreground(th.FaintText)
		if m.Field.Focused {
			labelStyle = plain.Foreground(th.SuccessText).Bold(true)
		}
		lines = append(lines, Line(labelStyle, m.Field.Label, inner))
		fieldRow = len(lines)
		lines = append(lines, Line(plain, m.Field.View, inner))
		if m.Field.Err != "" {
			lines = append(lines, Line(plain.Foreground(th.ErrorText), m.Field.Err, inner))
		}
		lines = append(lines, "")
	}
	if m.OptionsLabel != "" {
		labelStyle := plain.Foreground(th.FaintText)
		if m.Field == nil || !m.Field.Focused {
			labelStyle = plain.Foreground(th.SuccessText).Bold(true)
		}
		lines = append(lines, Line(labelStyle, m.OptionsLabel, inner))
	}

	optRow := len(lines)
	for i, o := range m.Options {
		s := plain.Foreground(th.PrimaryText)
		if o.Danger {
			s = s.Foreground(th.ErrorText)
		}
		if i == m.Cursor {
			s = s.Background(th.SelectedBackground).Bold(true)
		}
		lines = append(lines, Line(s, o.Text, inner))
	}
	if m.Hint != "" {
		lines = append(lines, "", Line(plain.Foreground(th.FaintText), m.Hint, inner))
	}
	box := Box(th, lines)
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	x, y := max(0, (width-w)/2), max(0, (height-h)/2)
	return Placed{
		Box:       box,
		X:         x,
		Y:         y,
		W:         w,
		H:         h,
		optionTop: y + 1 + optRow,
		options:   len(m.Options),
		hasField:  m.Field != nil,
		fieldY:    y + 1 + fieldRow,
	}
}

func optionTexts(opts []Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Text
	}
	return out
}

// Line is text in style, cut or padded to width.
func Line(style lipgloss.Style, text string, width int) string {
	return style.Width(width).MaxWidth(width).Render(ansi.Truncate(text, width, ""))
}

// Box frames lines as gh-dash frames its popups.
func Box(th ghtheme.Theme, lines []string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.SecondaryBorder).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

// Overlay draws box over base at x, y, first padding base to height rows so
// the frame keeps its size whatever is under the box.
func Overlay(base, box string, x, y, height int) string {
	if missing := height - lipgloss.Height(base); missing > 0 {
		base += strings.Repeat("\n", missing)
	}
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}
