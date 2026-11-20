package comp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

func Cursor(th theme.Theme, x, y int) *tea.Cursor {
	c := tea.NewCursor(x, y)
	c.Shape = tea.CursorBlock
	c.Blink = true
	c.Color = th.MutedOrSubtle()
	return c
}

// Offset moves c by (dx, dy) into the caller's frame. It returns nil for nil.
func Offset(c *tea.Cursor, dx, dy int) *tea.Cursor {
	if c == nil {
		return nil
	}
	moved := *c
	moved.X += dx
	moved.Y += dy
	return &moved
}
