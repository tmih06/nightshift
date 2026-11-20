package app

import (
	"github.com/tmih06/nightshift/internal/zenocto/tui/syntax"
	"github.com/tmih06/nightshift/internal/zenocto/tui/testtheme"
	"github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

func newInternal(client GitHub) Model {
	syn, _ := syntax.New(theme.SyntaxDark)
	return New(client, testtheme.Theme, syn)
}
