package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmih06/nightshift/scripts/quality/internal/difflines"
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

const changedDiff = "diff --git a/internal/a/a.go b/internal/a/a.go\n--- a/internal/a/a.go\n+++ b/internal/a/a.go\n@@ -0,0 +8,1 @@\n+x\n"

func TestRunFailsOnUntestedChange(t *testing.T) {
	writeFiles(t, map[string]string{profile: sample, diffDir + "/diff": changedDiff, diffDir + "/untracked": ""})
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr); code != 1 {
		t.Errorf("run = %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "need 70%") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRunPassesWithNoChanges(t *testing.T) {
	writeFiles(t, map[string]string{profile: sample, diffDir + "/diff": "", diffDir + "/untracked": ""})
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr); code != 0 {
		t.Errorf("run = %d: %s", code, stderr.String())
	}
}

func TestRunReportsMissingInputs(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no profile": {diffDir + "/diff": "", diffDir + "/untracked": ""},
		"no diff":    {profile: sample},
	} {
		t.Run(name, func(t *testing.T) {
			writeFiles(t, files)
			var stdout, stderr bytes.Buffer
			if code := run(&stdout, &stderr); code != 2 {
				t.Errorf("run = %d, want 2", code)
			}
		})
	}
}

func TestParseLineRejectsMalformedBlocks(t *testing.T) {
	for _, line := range []string{"mode: atomic", "a.go 1 2", "a.go:1.1 1 2", "a.go:x.1,2.1 1 2", "a.go:1.1,2.1 y 2"} {
		if _, _, ok := parseLine(line); ok {
			t.Errorf("parseLine(%q) accepted a malformed line", line)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestReportSurfacesWriteErrors(t *testing.T) {
	blocks := parseProfile(sample)
	cases := map[string]difflines.Lines{
		"none":    {},
		"passing": {"internal/a/a.go": {2: true}},
		"failing": {"internal/a/a.go": {8: true}},
	}
	for name, added := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := report(blocks, added, failingWriter{}, failingWriter{}); err == nil {
				t.Error("report ignored a write error")
			}
		})
	}
	if code := run(failingWriter{}, failingWriter{}); code != 3 {
		t.Errorf("run = %d, want 3", code)
	}
}
