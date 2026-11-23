package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tmih06/nightshift/scripts/quality/internal/difflines"
)

const sample = "mode: atomic\n" +
	"github.com/tmih06/nightshift/internal/a/a.go:1.1,3.2 2 1\n" +
	"github.com/tmih06/nightshift/internal/a/a.go:5.1,6.2 2 0\n" +
	"github.com/tmih06/nightshift/internal/a/a.go:5.1,6.2 2 3\n" +
	"github.com/tmih06/nightshift/internal/a/a.go:8.1,9.2 6 0\n" +
	"not a profile line\n"

func TestParseProfileMergesRuns(t *testing.T) {
	blocks := parseProfile(sample)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3", len(blocks))
	}
	for _, b := range blocks {
		if b.start == 5 && !b.hit {
			t.Error("a block hit in any run must count as hit")
		}
		if b.file != "internal/a/a.go" {
			t.Errorf("file = %q", b.file)
		}
	}
}

func TestReport(t *testing.T) {
	cases := []struct {
		name  string
		lines map[int]bool
		want  int
		text  string
	}{
		{"covered", map[int]bool{2: true, 5: true}, 0, "100.0%"},
		{"uncovered", map[int]bool{2: true, 8: true}, 1, "internal/a/a.go:8: coverage/changed-lines"},
		{"no statements", map[int]bool{20: true}, 0, "no changed statements"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := report(parseProfile(sample), difflines.Lines{"internal/a/a.go": tc.lines}, &stdout, &stderr)
			if err != nil {
				t.Fatal(err)
			}
			if code != tc.want {
				t.Errorf("code = %d, want %d", code, tc.want)
			}
			if !strings.Contains(stdout.String()+stderr.String(), tc.text) {
				t.Errorf("output %q lacks %q", stdout.String()+stderr.String(), tc.text)
			}
		})
	}
}
