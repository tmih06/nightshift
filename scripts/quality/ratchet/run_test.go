package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func gateFiles(lintIssues string) map[string]string {
	return map[string]string{
		gateDir + "/lint-full.json":          `{"Issues":[` + lintIssues + `]}`,
		gateDir + "/lint-new.json":           `{"Issues":[` + lintIssues + `]}`,
		gateDir + "/semgrep.json":            `{"results":[{"check_id":"x.slop-rule","path":"internal/a/a.go","start":{"line":2,"col":1},"extra":{"message":"do better"}},{"check_id":"x.slop-rule","path":"internal/ghdash/b.go","start":{"line":9,"col":1},"extra":{"message":"old"}}]}`,
		gateDir + "/deadcode.txt":            "internal/a/a.go:1:1: unreachable func: x\n",
		gateDir + "/jscpd/jscpd-report.json": `{"duplicates":[{"lines":7,"firstFile":{"name":"zenocto/c.go"}}]}`,
		gateDir + "/diff/diff":               "diff --git a/internal/a/a.go b/internal/a/a.go\n--- a/internal/a/a.go\n+++ b/internal/a/a.go\n@@ -1,0 +2 @@\n+x\n",
		gateDir + "/diff/untracked":          "",
	}
}

const oneIssue = `{"FromLinter":"errcheck","Text":"unchecked","Pos":{"Filename":"internal/a/a.go","Line":3,"Column":2}}`

func runCmd(args ...string) (code int, output string) {
	var stdout, stderr bytes.Buffer
	code = run(args, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

func readBaseline(t *testing.T) map[string]counts {
	t.Helper()
	raw, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	var b baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return b.Rules
}

func TestInitThenCheckPasses(t *testing.T) {
	files := gateFiles(oneIssue)
	files[baselinePath] = "{}"
	writeFiles(t, files)
	if code, out := runCmd("init"); code != 0 {
		t.Fatalf("init = %d: %s", code, out)
	}
	rules := readBaseline(t)
	if rules["golangci/errcheck"].First != 1 || rules[jscpdRule].Vendored != 7 || rules["semgrep/slop-rule"] != (counts{First: 1, Vendored: 1}) {
		t.Errorf("baseline = %+v", rules)
	}
	if code, out := runCmd("check"); code != 0 {
		t.Errorf("check = %d: %s", code, out)
	}
}

func TestCheckFailsWhenDebtRises(t *testing.T) {
	files := gateFiles(oneIssue + "," + oneIssue)
	files[baselinePath] = `{"rules":{"golangci/errcheck":{"first":1,"vendored":0}}}`
	writeFiles(t, files)
	code, out := runCmd("check")
	if code != 1 || !strings.Contains(out, "ratchet/golangci/errcheck") {
		t.Errorf("check = %d: %s", code, out)
	}
}

func TestCheckReportsImprovementAndUpdateLowers(t *testing.T) {
	files := gateFiles(oneIssue)
	files[baselinePath] = `{"rules":{"golangci/errcheck":{"first":4,"vendored":0},"semgrep/slop-rule":{"first":1,"vendored":1},"` +
		deadcodeRule + `":{"first":1,"vendored":0},"` + jscpdRule + `":{"first":0,"vendored":7}}}`
	writeFiles(t, files)
	code, out := runCmd("check")
	if code != 0 || !strings.Contains(out, "make quality-baseline") {
		t.Errorf("check = %d: %s", code, out)
	}
	if code, out := runCmd("update"); code != 0 {
		t.Fatalf("update = %d: %s", code, out)
	}
	if got := readBaseline(t)["golangci/errcheck"].First; got != 1 {
		t.Errorf("errcheck after update = %d, want 1", got)
	}
}

func TestReportLint(t *testing.T) {
	writeFiles(t, gateFiles(oneIssue))
	code, out := runCmd("report", "lint")
	if code != 1 || !strings.Contains(out, "internal/a/a.go:3:2: golangci/errcheck [error]: unchecked. Fix:") {
		t.Errorf("report = %d: %s", code, out)
	}
}

func TestReportLintCleanPasses(t *testing.T) {
	writeFiles(t, gateFiles(""))
	if code, out := runCmd("report", "lint"); code != 0 {
		t.Errorf("report = %d: %s", code, out)
	}
}

func TestReportSemgrepKeepsOnlyChangedLines(t *testing.T) {
	writeFiles(t, gateFiles(""))
	code, out := runCmd("report", "semgrep")
	if code != 1 || !strings.Contains(out, "internal/a/a.go:2:1: semgrep/slop-rule") {
		t.Errorf("report = %d: %s", code, out)
	}
	if strings.Contains(out, "ghdash") {
		t.Errorf("reported an unchanged line: %s", out)
	}
}

func TestBadInvocations(t *testing.T) {
	writeFiles(t, map[string]string{"placeholder": ""})
	for _, args := range [][]string{{}, {"nope"}, {"report"}, {"report", "nope"}, {"report", "lint"}, {"report", "semgrep"}, {"check"}, {"update"}, {"init"}} {
		if code, _ := runCmd(args...); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
	}
}

func TestCollectReportsEachMissingInput(t *testing.T) {
	for _, missing := range []string{"/semgrep.json", "/jscpd/jscpd-report.json", "/deadcode.txt"} {
		t.Run(missing, func(t *testing.T) {
			files := gateFiles("")
			files[baselinePath] = "{}"
			delete(files, gateDir+missing)
			writeFiles(t, files)
			if code, _ := runCmd("check"); code != 2 {
				t.Errorf("check = %d, want 2", code)
			}
		})
	}
}

func TestMalformedJSONFails(t *testing.T) {
	files := gateFiles("")
	files[gateDir+"/lint-new.json"] = "not json"
	writeFiles(t, files)
	if code, _ := runCmd("report", "lint"); code != 2 {
		t.Errorf("report = %d, want 2", code)
	}
}
