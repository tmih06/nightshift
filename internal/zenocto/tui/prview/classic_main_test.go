package prview_test

import (
	"os"
	"testing"

	"github.com/tmih06/nightshift/internal/zenocto/tui/comp"
)

// zen-octo's tests find panes and cards by their box-drawing glyphs, so they
// run on the classic chrome; the rows and columns are the same either way.
// The gh-dash look has its own tests in comp and nightshift_look_test.go.
func TestMain(m *testing.M) {
	comp.ClassicChrome = true
	os.Exit(m.Run())
}
