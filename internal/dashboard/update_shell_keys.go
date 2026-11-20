package dashboard

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/session"
)

// sgrMouseLeakRe matches SGR mouse suffixes that leak as KeyRunes when
// a PTY or SSH splits ESC from the rest of the sequence across reads.
var sgrMouseLeakRe = regexp.MustCompile(`^(?:\[?<\d+;\d+;\d+[Mm])+$`)

var shellKeys = map[string]string{
	"enter":     "Enter",
	"esc":       "Escape",
	"up":        "Up",
	"down":      "Down",
	"left":      "Left",
	"right":     "Right",
	"backspace": "BSpace",
	"delete":    "DC",
	"home":      "Home",
	"end":       "End",
	"pgup":      "PageUp",
	"pgdown":    "PageDown",
	"tab":       "Tab",
	"shift+tab": "BTab",
}

func (m model) updateShellKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.canFocusShell() {
		m.focus = focusNav
		m.prefix = false
		return m, nil
	}
	if m.prefix {
		m.prefix = false
		if msg.String() == "q" {
			m.focus = focusNav
			m.status, m.statusTTL = "", 0
		}
		return m, nil
	}
	if msg.String() == "ctrl+b" {
		m.prefix = true
		return m, nil
	}
	key, ok := shellKeyFromMsg(msg)
	if !ok {
		return m, nil
	}
	m.pending = append(m.pending, key)
	if m.sending {
		return m, nil
	}
	return m, m.startDrain()
}

func isMouseEscapeFragment(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes {
		return false
	}
	if msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == '[' {
		return true
	}
	return !msg.Paste && sgrMouseLeakRe.MatchString(string(msg.Runes))
}

func shellKeyFromMsg(msg tea.KeyMsg) (session.Key, bool) {
	if isMouseEscapeFragment(msg) {
		return session.Key{}, false
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		return session.Key{Text: string(msg.Runes)}, true
	}
	name, ok := shellKeyName(msg.String())
	return session.Key{Name: name}, ok
}

func shellKeyName(s string) (string, bool) {
	if name, ok := shellKeys[s]; ok {
		return name, true
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		return "C-" + s[5:], true
	}
	return "", false
}
