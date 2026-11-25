package dialog

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

// TextInput is a one-line input styled like gh-dash's prompts. It is not
// focused; callers focus it when it should take keys.
func TextInput(th ghtheme.Theme, prompt, placeholder string, limit int) textinput.Model {
	ti := textinput.New()
	ti.SetVirtualCursor(true)
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	ti.CharLimit = limit
	base := lipgloss.NewStyle()
	ti.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{
			Prompt:      base.Foreground(th.SuccessText).Bold(true),
			Text:        base.Foreground(th.PrimaryText),
			Placeholder: base.Foreground(th.FaintText),
		},
		Blurred: textinput.StyleState{
			Prompt:      base.Foreground(th.FaintText),
			Text:        base.Foreground(th.SecondaryText),
			Placeholder: base.Foreground(th.FaintText),
		},
		Cursor: textinput.CursorStyle{Color: th.PrimaryText, Shape: tea.CursorBar},
	})
	return ti
}
