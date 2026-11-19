package dashboard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/session"
)

// updateShellKeys routes every keystroke into the sandbox's tmux session
// while the shell pane has focus. No keybind escapes the sandbox: only
// C-b followed by q leaves, like a tmux prefix chord. Keys queue and
// drain one batch at a time, so fast typing is batched and never
// reorders.
func (m model) updateShellKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := m.selectedBox()
	if b == nil || !m.sessions[b.Name] {
		m.shellFocus = false
		m.prefix = false
		return m, nil
	}
	if m.prefix {
		// A chord is armed: q leaves, anything else cancels and is
		// swallowed, the way an unbound tmux prefix chord does nothing.
		m.prefix = false
		if msg.String() == "q" {
			m.shellFocus = false
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

// shellKeyFromMsg turns a keypress into a session input event: runes
// become literal text, mapped keys become their tmux names.
func shellKeyFromMsg(msg tea.KeyMsg) (session.Key, bool) {
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
