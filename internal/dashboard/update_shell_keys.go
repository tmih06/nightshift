package dashboard

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// updateShellKeys routes keystrokes into the sandbox's tmux session
// while the shell pane has focus. Esc is the only key the dashboard
// keeps for itself, so there is no way to get stuck in the shell.
func (m model) updateShellKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.shellFocus = false
		return m, nil
	}
	b := m.selectedBox()
	if b == nil || !m.sessions[b.Name] {
		m.shellFocus = false
		return m, nil
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		return m, sendCmd(b.Name, "", string(msg.Runes))
	}
	key, ok := tmuxKeyName(msg.String())
	if !ok {
		return m, nil
	}
	return m, sendCmd(b.Name, key, "")
}

// tmuxKeyName maps a bubbletea key string to the tmux key name
// send-keys understands.
func tmuxKeyName(s string) (string, bool) {
	if name, ok := map[string]string{
		"enter":     "Enter",
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
