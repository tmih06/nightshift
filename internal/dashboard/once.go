package dashboard

import ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"

// Fetched returns the tab after one synchronous read of the cluster, for
// printing a single frame without a terminal.
func Fetched(t ghtheme.Theme) Model {
	m := New(t)
	m, _ = m.Update(fetchCmd()())
	return m
}
