// Package internal holds the test that keeps Nightshift's two halves apart.
package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const module = "github.com/tmih06/nightshift/"

// Nightshift is two programs. The services (internal/services) own everything
// that touches the cluster. A frontend (internal/tui, and the vendored gh-dash
// and zen-octo it embeds) draws screens and reaches the services only through
// their API: the contract's types in api/v1 and the client in api/client. This
// test fails when an import crosses that line, or when a layer of the services
// reaches past the layer it stands on.
//
// It stands where depguard rules cannot be changed without a code owner: it
// reads the imports itself.

// services lists, for each package of the services, the first-party packages
// its non-test code may import besides the contract (api/v1) and the standard
// library. A package not listed here may import none of them.
func services() map[string][]string {
	return map[string][]string{
		"internal/services/agent": {},
		// nightshift-auth: its own program, a leaf like the egress proxy.
		"internal/services/auth":           {"internal/services/auth/providers"},
		"internal/services/auth/providers": {},
		"internal/services/cluster":        {},
		"internal/services/egress":         {},
		"internal/services/manifest":       {},
		"internal/services/session":        {"internal/services/cluster"},
		"internal/services/accounts":       {"internal/services/cluster"},
		"internal/services/checkout":       {"internal/services/agent", "internal/services/cluster", "internal/services/session"},
		"internal/services/runs":           {"internal/services/checkout", "internal/services/cluster", "internal/services/session"},
		// The HTTP layer knows nothing of Kubernetes: it serves the contract
		// over interfaces, and knows only the errors the checkouts raise.
		"internal/services/server": {"api", "internal/services/checkout"},
		"internal/services/live": {
			"internal/services/accounts", "internal/services/agent", "internal/services/checkout", "internal/services/cluster",
			"internal/services/runs", "internal/services/server", "internal/services/session",
		},
		"internal/services/daemon": {
			"internal/services/accounts", "internal/services/cluster", "internal/services/live", "internal/services/server",
		},
	}
}

// testExtras are what a package's tests may import beyond its code: the
// end-to-end tests of the server run the Go client against it.
func testExtras() map[string][]string {
	return map[string][]string{
		"internal/services/server": {"api/client"},
	}
}

// frontends are the trees that must stay clear of the services.
func frontends() []string {
	return []string{"internal/tui", "internal/ghdash", "internal/zenocto"}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(dir) // this file is in internal/
}

// imports is every first-party import of every Go file under dir, as
// file -> import paths (without the module prefix), split into code and tests.
func imports(t *testing.T, root, dir string) (code, tests map[string][]string) {
	t.Helper()
	code, tests = map[string][]string{}, map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		into := code
		if strings.HasSuffix(path, "_test.go") {
			into = tests
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, "\"")
			if rest, ok := strings.CutPrefix(p, module); ok {
				into[rel] = append(into[rel], rest)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return code, tests
}

func under(path, tree string) bool { return path == tree || strings.HasPrefix(path, tree+"/") }

func TestFrontendsReachTheServicesOnlyThroughTheirAPI(t *testing.T) {
	root := repoRoot(t)
	for _, tree := range frontends() {
		code, tests := imports(t, root, tree)
		for _, set := range []map[string][]string{code, tests} {
			for file, imps := range set {
				for _, imp := range imps {
					if under(imp, "internal/services") || under(imp, "cmd") {
						t.Errorf("%s imports %s. A frontend talks to the services through api/v1 and api/client.", file, imp)
					}
				}
			}
		}
	}
}

// The terminal program is a client of a server somewhere else. It must not
// carry the services inside itself, or "nothing runs here but the terminal"
// stops being true.
func TestTheTerminalProgramCarriesNoServices(t *testing.T) {
	root := repoRoot(t)
	code, tests := imports(t, root, "cmd/nightshift")
	for _, set := range []map[string][]string{code, tests} {
		for file, imps := range set {
			for _, imp := range imps {
				if under(imp, "internal/services") {
					t.Errorf("%s imports %s. The terminal talks to nightshiftd through api/client.", file, imp)
				}
			}
		}
	}
}

func TestTheServicesKnowNothingOfAnyFrontend(t *testing.T) {
	root := repoRoot(t)
	code, tests := imports(t, root, "internal/services")
	for _, set := range []map[string][]string{code, tests} {
		for file, imps := range set {
			for _, imp := range imps {
				for _, tree := range frontends() {
					if under(imp, tree) {
						t.Errorf("%s imports %s. The services serve any frontend and know none of them.", file, imp)
					}
				}
			}
		}
	}
}

func TestEachLayerOfTheServicesStandsOnTheOnesBelow(t *testing.T) {
	root := repoRoot(t)
	allowed := services()
	for pkg := range allowed {
		if _, err := os.Stat(filepath.Join(root, pkg)); err != nil {
			t.Errorf("%s is listed but does not exist: remove it from the table", pkg)
		}
	}
	code, tests := imports(t, root, "internal/services")
	check := func(files map[string][]string, extra map[string][]string) {
		for file, imps := range files {
			pkg := filepath.ToSlash(filepath.Dir(file))
			ok, known := allowed[pkg]
			if !known {
				t.Errorf("%s is in a package the table does not know: add %s to services()", file, pkg)
				continue
			}
			for _, imp := range imps {
				if imp == pkg || under(imp, pkg) || under(imp, "api/v1") || (!under(imp, "internal") && !under(imp, "api") && !under(imp, "cmd")) {
					continue
				}
				if !contains(ok, imp) && !contains(extra[pkg], imp) {
					t.Errorf("%s imports %s. %s may import only %v.", file, imp, pkg, sorted(ok))
				}
			}
		}
	}
	check(code, nil)
	check(tests, testExtras())
}

func TestTheContractStaysFreeOfEverythingElse(t *testing.T) {
	root := repoRoot(t)
	code, _ := imports(t, root, "api")
	for file, imps := range code {
		pkg := filepath.ToSlash(filepath.Dir(file))
		for _, imp := range imps {
			if pkg == "api/client" && imp == "api/v1" {
				continue
			}
			t.Errorf("%s imports %s. The contract and its client depend on the standard library and api/v1, so any frontend can use them.", file, imp)
		}
	}
}

func TestNothingImportsACommand(t *testing.T) {
	root := repoRoot(t)
	for _, tree := range []string{"api", "internal"} {
		code, tests := imports(t, root, tree)
		for _, set := range []map[string][]string{code, tests} {
			for file, imps := range set {
				for _, imp := range imps {
					if under(imp, "cmd") {
						t.Errorf("%s imports %s. Commands are the roots; nothing builds on them.", file, imp)
					}
				}
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sorted(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}
