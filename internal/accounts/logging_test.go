package accounts

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// The terminal belongs to the TUI, and the standard library's log writes to
// stderr, over it. Nightshift logs through charm's log, which main points at
// a file or at nothing. A failed port-forward once scribbled across the tab.
func TestTabPackagesLogThroughCharmLog(t *testing.T) {
	for _, dir := range []string{".", "../accountsview", "../app"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range parsed.Imports {
				if strings.Trim(imp.Path.Value, `"`) == "log" {
					t.Errorf("%s imports the standard log; use charm.land/log/v2", f)
				}
			}
		}
	}
}
