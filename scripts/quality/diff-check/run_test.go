package main

import (
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

func diffInputs(diff, status, untracked string) map[string]string {
	dir := ".tools/gate/diff/"
	return map[string]string{dir + "diff": diff, dir + "status": status, dir + "untracked": untracked, dir + "subjects": "feat: add a thing\n"}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GATE_DIFF_DIR", "PR_BODY", "PR_TITLE", "GATE_CI", "GATE_GUARDRAIL_BLOCK"} {
		t.Setenv(key, "")
	}
}

func TestRunPassesCleanChange(t *testing.T) {
	clearEnv(t)
	raw := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n"
	writeFiles(t, diffInputs(raw, "M\ta.go\n", ""))
	if code := run(); code != 0 {
		t.Errorf("run = %d, want 0", code)
	}
}

func TestRunFailsOnUntrackedSkip(t *testing.T) {
	clearEnv(t)
	files := diffInputs("", "", "x_test.go\n")
	files["x_test.go"] = "package x\n\nfunc TestX(t *testing.T) {\n\tt.Skip(\"later\")\n}\n"
	writeFiles(t, files)
	if code := run(); code != 1 {
		t.Errorf("run = %d, want 1", code)
	}
}

func TestRunNeedsSavedInputs(t *testing.T) {
	clearEnv(t)
	writeFiles(t, map[string]string{"placeholder": ""})
	if code := run(); code != 2 {
		t.Errorf("run = %d, want 2", code)
	}
}

func TestRunReportsUnreadableUntrackedFile(t *testing.T) {
	clearEnv(t)
	writeFiles(t, diffInputs("", "", "missing.go\n"))
	if code := run(); code != 2 {
		t.Errorf("run = %d, want 2", code)
	}
}

func TestLoadOptionsReadsEnvironment(t *testing.T) {
	t.Setenv("PR_BODY", "body")
	t.Setenv("PR_TITLE", "fix: x")
	t.Setenv("GATE_CI", "1")
	t.Setenv("GATE_GUARDRAIL_BLOCK", "1")
	got := loadOptions("one\ntwo\n")
	if !got.inCI || !got.guardrailBlocks || got.prBody != "body" || got.prTitle != "fix: x" {
		t.Errorf("options = %+v", got)
	}
	if strings.Join(got.subjects, ",") != "one,two" {
		t.Errorf("subjects = %v", got.subjects)
	}
}

func TestFindingString(t *testing.T) {
	f := finding{file: "a.go", line: 3, rule: "diff/x", msg: "m", warn: true}
	if got := f.String(); got != "a.go:3: diff/x [warning]: m" {
		t.Errorf("String = %q", got)
	}
}

func TestGuardrailBlocksWhenEnabled(t *testing.T) {
	files := []*fileDiff{{path: "internal/a/a.go", status: "M"}}
	got := checkGuardrail(files, options{subjects: []string{"fix(go): stop a crash"}, guardrailBlocks: true})
	if len(got) != 1 || got[0].warn {
		t.Errorf("findings = %+v, want one blocking finding", got)
	}
}
