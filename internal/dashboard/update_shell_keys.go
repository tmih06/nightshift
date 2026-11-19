package dashboard

import (
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/session"
)

// sgrMouseLeakRe matches raw SGR mouse escape suffixes that leak as
// KeyRunes when a terminal/SSH/tmux splits ESC from the rest of the
// sequence across read boundaries.
var sgrMouseLeakRe = regexp.MustCompile(`^(?:\[?<\d+;\d+;\d+[Mm])+$`)

// updateShellKeys routes every keystroke into the sandbox's tmux session
// while the shell pane has focus. No keybind escapes the sandbox: only
// C-b followed by q leaves, like a tmux prefix chord. Keys queue and
// drain one batch at a time, so fast typing is batched and never
// reorders.
func (m model) updateShellKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.canFocusShell() {
		m.focus = focusNav
		m.prefix = false
		return m, nil
	}
	if m.prefix {
		// A chord is armed: q leaves, anything else cancels and is
		// swallowed, the way an unbound tmux prefix chord does nothing.
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
		return m, nil // a drain is in flight, the queue keeps order
	}
	return m, m.startDrain()
}

// isMouseEscapeFragment reports whether a KeyMsg is a broken CSI/SGR
// mouse sequence fragment rather than user-typed input.
func isMouseEscapeFragment(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes {
		return false
	}
	// An orphaned CSI introducer (ESC [) arrives as Alt+[.
	if msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == '[' {
		return true
	}
	if !msg.Paste && sgrMouseLeakRe.MatchString(string(msg.Runes)) {
		return true
	}
	return false
}

// shellKeyFromMsg turns a keypress into a session input event: runes
// become literal text, mapped keys become their tmux names.
func shellKeyFromMsg(msg tea.KeyMsg) (session.Key, bool) {
	if isMouseEscapeFragment(msg) {
		return session.Key{}, false
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		return session.Key{Text: string(msg.Runes)}, true
	}
	name, ok := tmuxKeyName(msg.String())
	return session.Key{Name: name}, ok
}

// tmuxKeyName maps a bubbletea key string to the tmux key name
// send-keys understands.
func tmuxKeyName(s string) (string, bool) {
	if name, ok := map[string]string{
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
	}[s]; ok {
		return name, true
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		return "C-" + s[5:], true
	}
	return "", false
}
