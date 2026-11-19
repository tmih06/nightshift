package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Row 0 is the title line and row 1 the left pane's top border, so the
// left pane's content starts at row 2 in both layouts.
const contentStartY = 2

func (m model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		m.moveSelection(-1)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
		m.moveSelection(1)
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m = m.clickLeftPane(msg.X, msg.Y)
	}
	return m, nil
}

// clickLeftPane maps a click to the entry on that line. X selects the
// pane in the side-by-side layout, Y in the stacked one, and the
// line-to-entry map comes from the same leftLines the view renders, so
// clicks cannot drift from what is on screen.
func (m model) clickLeftPane(x, y int) model {
	leftW, leftH, stacked := m.calculateLayout()
	if stacked {
		if y < contentStartY || y >= leftH-1 {
			return m
		}
	} else {
		if x < 0 || x >= leftW {
			return m
		}
	}
	_, targets := m.leftLines()
	if idx, ok := targets[y-contentStartY]; ok {
		m.selected = idx
	}
	return m
}
