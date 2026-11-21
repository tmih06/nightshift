package dashboard

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// The full key list that ? opens under the footer, in columns, as gh-dash
// shows its own.
var (
	keyEnds      = key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "first/last"))
	keyBash      = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "attach bash"))
	keyDestroy2  = key.NewBinding(key.WithKeys("x"), key.WithHelp("x x", "destroy claim"))
	keyLeave     = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("C-b q", "leave bash"))
	keyWinNew    = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("C-b c", "new window"))
	keyWinSwitch = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("C-b n/p/1-9", "switch window"))
	keyWinClose  = key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("C-b x", "close window"))
	keyDrag      = key.NewBinding(key.WithKeys(""), key.WithHelp("drag │", "resize sidebar"))
	keyHelp      = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help"))
)

type helpKeys struct{}

func (helpKeys) ShortHelp() []key.Binding { return []key.Binding{keyHelp} }

func (helpKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keyMove, keyEnds, keySection, keyDrag},
		{keyOpen, keyClaim, keyBash, keyDestroy2, keyFull},
		{keyWinNew, keyWinSwitch, keyWinClose, keyLeave},
		{keyScroll, keyPage, keyBack, keyRefresh, keyTabs, keyHelp, keyQuit},
	}
}

func (m model) helpView() string {
	if !m.showHelp {
		return ""
	}
	h := help.New()
	h.ShowAll = true
	h.Styles = styles.Help.BubbleStyles
	h.SetWidth(m.width)
	return h.View(helpKeys{})
}

// footerHeight is the footer bar plus the open help.
func (m model) footerHeight() int {
	if !m.showHelp {
		return footerH
	}
	return footerH + lipgloss.Height(m.helpView())
}
