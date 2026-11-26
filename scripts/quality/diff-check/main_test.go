package main

import (
	"strings"
	"testing"
)

func rules(findings []finding) string {
	names := make([]string, 0, len(findings))
	for _, f := range findings {
		names = append(names, f.rule)
	}
	return strings.Join(names, ",")
}

func added(fd *fileDiff, lines ...string) *fileDiff {
	for i, l := range lines {
		recordAdded(fd, i+1, l)
	}
	return fd
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name  string
		files []*fileDiff
		opts  options
		want  string
	}{
		{"clean", []*fileDiff{added(&fileDiff{path: "internal/a/a.go", status: "M"}, "x := 1")}, options{}, ""},
		{"nolint", []*fileDiff{added(&fileDiff{path: "internal/a/a.go", status: "M"}, "x() //nolint")}, options{}, "diff/no-nolint"},
		{"skip", []*fileDiff{added(&fileDiff{path: "internal/a/a_test.go", status: "M"}, "t.Skip(\"later\")")}, options{}, "diff/no-skip"},
		{"build ignore", []*fileDiff{added(&fileDiff{path: "internal/a/a.go", status: "M"}, "//go:build ignore")}, options{}, "diff/no-build-ignore"},
		{"assertion removed", []*fileDiff{{path: "internal/a/a_test.go", status: "M", removedAssertions: 2, addedAssertions: 1}}, options{}, "diff/assertion-removed"},
		{"assertion replaced", []*fileDiff{{path: "internal/a/a_test.go", status: "M", removedAssertions: 1, addedAssertions: 1}}, options{}, ""},
		{"test deleted", []*fileDiff{{path: "internal/a/a_test.go", status: "D"}}, options{}, "diff/test-deleted"},
		{"test moved", []*fileDiff{{path: "internal/a/a_test.go", status: "D"}, {path: "internal/a/b_test.go", status: "A", addedTests: 1}}, options{}, ""},
		{"assertions moved to another package", []*fileDiff{{path: "internal/a/a_test.go", status: "M", removedAssertions: 3}, {path: "api/v1/a_test.go", status: "A", addedAssertions: 3}}, options{}, ""},
		{"assertions moved with fewer", []*fileDiff{{path: "internal/a/a_test.go", status: "M", removedAssertions: 3}, {path: "api/v1/a_test.go", status: "A", addedAssertions: 2}}, options{}, "diff/assertion-removed"},
		{"test rewritten as it moves", []*fileDiff{{path: "internal/a/a_test.go", status: "D"}, {path: "internal/tui/a/a_test.go", status: "A", addedTests: 2}}, options{}, ""},
		{"test deleted beside an unrelated one", []*fileDiff{{path: "internal/a/a_test.go", status: "D"}, {path: "internal/tui/b/b_test.go", status: "A", addedTests: 2}}, options{}, "diff/test-deleted"},
		{"test renamed", []*fileDiff{{path: "internal/b/a_test.go", from: "internal/a/a_test.go", status: "R"}}, options{}, ""},
		{"test renamed to a non-test", []*fileDiff{{path: "internal/a/a.txt", from: "internal/a/a_test.go", status: "R"}}, options{}, "diff/test-deleted"},
		{"protected moved away", []*fileDiff{{path: "docs/lint.yml", from: ".golangci.yml", status: "R"}}, options{}, "diff/protected-path"},
		{"protected moved in", []*fileDiff{{path: ".golangci.yml", from: "docs/lint.yml", status: "R"}}, options{}, "diff/protected-path"},
		{"protected", []*fileDiff{{path: ".golangci.yml", status: "M"}}, options{}, "diff/protected-path"},
		{"protected approved", []*fileDiff{{path: ".golangci.yml", status: "M"}}, options{ownerApproved: true}, ""},
		{"protected bootstrap", []*fileDiff{{path: ".golangci.yml", status: "A"}}, options{bootstrap: true}, ""},
		{"go.mod in CI", []*fileDiff{{path: "go.mod", status: "M"}}, options{inCI: true}, "diff/go-mod-justification"},
		{"go.mod justified", []*fileDiff{{path: "go.mod", status: "M"}}, options{inCI: true, prBody: "Go-Mod-Justification: need it"}, ""},
		{"fix without guardrail", []*fileDiff{{path: "internal/a/a.go", status: "M"}}, options{prTitle: "fix(go): stop a crash"}, "diff/fix-needs-guardrail"},
		{"fix with test", []*fileDiff{added(&fileDiff{path: "internal/a/a_test.go", status: "M"}, "func TestCrash(t *testing.T) {")}, options{prTitle: "fix(go): stop a crash"}, ""},
		{"fix waived", []*fileDiff{{path: "internal/a/a.go", status: "M"}}, options{prTitle: "fix: typo", prBody: "No-guardrail: one-off typo"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rules(check(tc.files, tc.opts)); got != tc.want {
				t.Errorf("rules = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseDiffTracksLineNumbers(t *testing.T) {
	raw := "diff --git a/x_test.go b/x_test.go\n--- a/x_test.go\n+++ b/x_test.go\n@@ -4,1 +10,2 @@\n-\tt.Fatal(err)\n+\tfoo()\n+\tt.Skip()\n"
	files := parseDiff(raw, map[string]string{"x_test.go": "M"})
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	fd := files[0]
	if fd.removedAssertions != 1 {
		t.Errorf("removedAssertions = %d, want 1", fd.removedAssertions)
	}
	if len(fd.added) != 2 || fd.added[1].n != 11 {
		t.Errorf("added = %+v, want the skip on line 11", fd.added)
	}
}

// A moved test file is the same tests in a new place. Reading it as a delete
// and an add flagged every test of a directory move, and counted the skips it
// already had as new.
func TestAMovedFileIsOneChangeNotADeleteAndAnAdd(t *testing.T) {
	status := parseStatus("R087\ta/x_test.go\tb/x_test.go\nM\tc.go\n")
	if status["b/x_test.go"] != "R" || status["c.go"] != "M" {
		t.Fatalf("status = %v", status)
	}
	raw := "diff --git a/a/x_test.go b/b/x_test.go\nsimilarity index 87%\nrename from a/x_test.go\nrename to b/x_test.go\n" +
		"--- a/a/x_test.go\n+++ b/b/x_test.go\n@@ -3 +3 @@\n-import \"old\"\n+import \"new\"\n"
	files := parseDiff(raw, status)
	if len(files) != 1 || files[0].path != "b/x_test.go" || files[0].from != "a/x_test.go" || files[0].status != "R" {
		t.Fatalf("files = %+v", files)
	}
	if got := rules(check(files, options{})); got != "" {
		t.Errorf("a moved test raised %q", got)
	}
	if len(files[0].added) != 1 {
		t.Errorf("only the changed line is new, got %+v", files[0].added)
	}
}
