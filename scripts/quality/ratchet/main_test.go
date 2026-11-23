package main

import (
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	base := map[string]counts{"golangci/errcheck": {First: 2, Vendored: 5}, "golangci/funlen": {First: 3}}
	cases := []struct {
		name         string
		cur          map[string]counts
		wantRegress  string
		wantImproved int
	}{
		{"unchanged", map[string]counts{"golangci/errcheck": {First: 2, Vendored: 5}, "golangci/funlen": {First: 3}}, "", 0},
		{"first-party rose", map[string]counts{"golangci/errcheck": {First: 3, Vendored: 5}, "golangci/funlen": {First: 3}}, "first-party count rose from 2 to 3", 0},
		{"vendored rose", map[string]counts{"golangci/errcheck": {First: 2, Vendored: 6}, "golangci/funlen": {First: 3}}, "vendored count rose from 5 to 6", 0},
		{"new rule", map[string]counts{"golangci/errcheck": {First: 2, Vendored: 5}, "golangci/funlen": {First: 3}, "semgrep/x": {First: 1}}, "ratchet/semgrep/x", 0},
		{"fell", map[string]counts{"golangci/errcheck": {First: 1, Vendored: 5}}, "", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			regressions, improved := compare(base, tc.cur)
			joined := strings.Join(regressions, "\n")
			if tc.wantRegress == "" && joined != "" {
				t.Errorf("unexpected regressions: %s", joined)
			}
			if !strings.Contains(joined, tc.wantRegress) {
				t.Errorf("regressions = %q, want %q", joined, tc.wantRegress)
			}
			if len(improved) != tc.wantImproved {
				t.Errorf("improved = %v, want %d", improved, tc.wantImproved)
			}
		})
	}
}

func TestLoweredNeverRaises(t *testing.T) {
	got := lowered(map[string]counts{"r": {First: 2, Vendored: 2}}, map[string]counts{"r": {First: 5, Vendored: 1}, "new": {First: 9}})
	if got["r"] != (counts{First: 2, Vendored: 1}) {
		t.Errorf("r = %+v", got["r"])
	}
	if _, ok := got["new"]; ok {
		t.Error("update added a rule that was not in the baseline")
	}
}

func TestCountDeadcodeSplitsVendored(t *testing.T) {
	out := map[string]counts{}
	countDeadcode(out, "internal/app/a.go:1:1: unreachable func: x\ninternal/ghdash/b.go:1:1: unreachable func: y\nnoise\n")
	if out[deadcodeRule] != (counts{First: 1, Vendored: 1}) {
		t.Errorf("deadcode = %+v", out[deadcodeRule])
	}
}

func TestHintFallsBack(t *testing.T) {
	if !strings.Contains(hint("no-such-linter"), "nolint") {
		t.Error("fallback hint must warn against //nolint")
	}
	if !strings.Contains(hint("errcheck"), "%w") {
		t.Error("errcheck hint must mention wrapping")
	}
}
