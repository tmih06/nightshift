package dashboard

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/session"
)

// sgrMouseLeakRe matches SGR mouse suffixes that leak as text when a PTY or
// SSH splits ESC from the rest of the sequence across reads.
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

func (m model) updateShellKeys(msg tea.KeyPressMsg) (model, tea.Cmd) {
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
	return m.queueShell(key)
}

func (m model) pasteIntoShell(text string) (model, tea.Cmd) {
	if !m.canFocusShell() || text == "" {
		return m, nil
	}
	return m.queueShell(session.Key{Text: text})
}

func (m model) queueShell(key session.Key) (model, tea.Cmd) {
	m.pending = append(m.pending, key)
	if m.sending {
		return m, nil
	}
	return m, m.startDrain()
}

func isMouseEscapeFragment(msg tea.KeyPressMsg) bool {
	if msg.Mod == tea.ModAlt && msg.Code == '[' {
		return true
	}
	return msg.Text != "" && sgrMouseLeakRe.MatchString(msg.Text)
}

func shellKeyFromMsg(msg tea.KeyPressMsg) (session.Key, bool) {
	if isMouseEscapeFragment(msg) {
		return session.Key{}, false
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		return session.Key{Text: msg.Text}, true
	}
	name, ok := shellKeyName(msg.String())
	return session.Key{Name: name}, ok
}

func shellKeyName(s string) (string, bool) {
	if name, ok := shellKeys[s]; ok {
		return name, true
	}
	if s == "space" {
		return "", false
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		return "C-" + s[5:], true
	}
	return "", false
}
