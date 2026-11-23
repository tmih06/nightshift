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
		{"protected", []*fileDiff{{path: ".golangci.yml", status: "M"}}, options{}, "diff/protected-path"},
		{"protected approved", []*fileDiff{{path: ".golangci.yml", status: "M"}}, options{ownerApproved: true}, ""},
		{"protected bootstrap", []*fileDiff{{path: ".golangci.yml", status: "A"}}, options{bootstrap: true}, ""},
		{"go.mod in CI", []*fileDiff{{path: "go.mod", status: "M"}}, options{inCI: true}, "diff/go-mod-justification"},
		{"go.mod justified", []*fileDiff{{path: "go.mod", status: "M"}}, options{inCI: true, prBody: "Go-Mod-Justification: need it"}, ""},
		{"size cap", []*fileDiff{{path: "internal/a/a.go", status: "M", changed: sizeCap + 1}}, options{}, "diff/size-cap"},
		{"vendored size ignored", []*fileDiff{{path: "internal/ghdash/a.go", status: "M", changed: sizeCap + 1}}, options{}, ""},
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
