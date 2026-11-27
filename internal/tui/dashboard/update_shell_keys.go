package dashboard

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/tui/fleet"
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
		m.focus, m.zoom = focusNav, false
		m.prefix = false
		return m, nil
	}
	if m.prefix {
		m.prefix = false
		return m.handlePrefixKey(msg)
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

func (m model) handlePrefixKey(msg tea.KeyPressMsg) (model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "q", "d":
		m.focus, m.zoom = focusNav, false
		m.status, m.statusTTL = "", 0
		return m, nil
	case "c":
		b := m.selectedBox()
		if b == nil || b.Phase != fleet.PodPhaseRunning {
			return m, nil
		}
		m.flash("opening new window…")
		return m, createWindowCmd(m.api, b.Name, m.activeTerm(b.Name))
	case "n", "l", "right":
		cmd := m.stepWindow(1)
		return m, cmd
	case "p", "h", "left":
		cmd := m.stepWindow(-1)
		return m, cmd
	case "x", "&":
		return m.closeActiveWindow()
	case "f", "F":
		b := m.selectedBox()
		if b == nil || b.Phase != fleet.PodPhaseRunning {
			return m, nil
		}
		return m.zoomShell(b)
	case "ctrl+b":
		return m.queueShell(v1.Key{Name: "C-b"})
	}
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		cmd := m.selectWindowIndex(int(s[0] - '1'))
		return m, cmd
	}
	return m, nil
}

func (m model) closeActiveWindow() (model, tea.Cmd) {
	b := m.selectedBox()
	if b == nil {
		return m, nil
	}
	term := m.activeTerm(b.Name)
	if term == "" {
		return m, nil
	}
	m.flash("closing window…")
	return m, killWindowCmd(m.api, b.Name, term)
}

func (m model) pasteIntoShell(text string) (model, tea.Cmd) {
	if !m.canFocusShell() || text == "" {
		return m, nil
	}
	return m.queueShell(v1.Key{Text: text})
}

func (m model) queueShell(key v1.Key) (model, tea.Cmd) {
	m.pending = append(m.pending, key)
	if b := m.selectedBox(); b != nil && m.pendingTerm == "" {
		m.pendingTerm = m.activeTerm(b.Name)
	}
	if m.sending {
		return m, nil
	}
	cmd := m.startDrain()
	return m, cmd
}

func isMouseEscapeFragment(msg tea.KeyPressMsg) bool {
	if msg.Mod == tea.ModAlt && msg.Code == '[' {
		return true
	}
	return msg.Text != "" && sgrMouseLeakRe.MatchString(msg.Text)
}

func shellKeyFromMsg(msg tea.KeyPressMsg) (v1.Key, bool) {
	if isMouseEscapeFragment(msg) {
		return v1.Key{}, false
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		return v1.Key{Text: msg.Text}, true
	}
	name, ok := shellKeyName(msg.String())
	return v1.Key{Name: name}, ok
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
