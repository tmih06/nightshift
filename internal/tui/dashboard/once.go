package dashboard

import ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"

// Fetched returns the tab after one synchronous read of the cluster, for
// printing a single frame without a terminal.
func Fetched(t ghtheme.Theme, api API) Model {
	m := New(t, api)
	m, _ = m.Update(fetchCmd(api)())
	return m
}
