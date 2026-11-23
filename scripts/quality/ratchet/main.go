// Command ratchet keeps per-rule debt from growing, and reports new findings
// with a fix hint. It reads tool output saved under .tools/gate.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/tmih06/nightshift/scripts/quality/internal/difflines"
)

const (
	baselinePath = "docs/quality-baseline.json"
	gateDir      = ".tools/gate"
	deadcodeRule = "deadcode/unreachable-func"
	jscpdRule    = "jscpd/duplicated-lines"
)

// counts is one rule's debt, split so vendored code cannot hide first-party growth.
type counts struct {
	First    int `json:"first"`
	Vendored int `json:"vendored"`
}

type baseline struct {
	Rules map[string]counts `json:"rules"`
}

type lintReport struct {
	Issues []struct {
		FromLinter string
		Text       string
		Pos        struct {
			Filename string
			Line     int
			Column   int
		}
	}
}

type semgrepReport struct {
	Results []struct {
		CheckID string `json:"check_id"`
		Path    string `json:"path"`
		Start   struct {
			Line int `json:"line"`
			Col  int `json:"col"`
		} `json:"start"`
		Extra struct {
			Message string `json:"message"`
		} `json:"extra"`
	} `json:"results"`
}

type jscpdReport struct {
	Duplicates []struct {
		Lines     int `json:"lines"`
		FirstFile struct {
			Name string `json:"name"`
		} `json:"firstFile"`
	} `json:"duplicates"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	root, err := os.OpenRoot(".")
	if err != nil {
		return fail(stderr, err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			fail(stderr, closeErr)
		}
	}()
	if len(args) == 0 {
		return fail(stderr, errors.New("usage: ratchet check|update|init|report lint|report semgrep"))
	}
	return dispatch(root, args, stdout, stderr)
}

func dispatch(root *os.Root, args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "check":
		return runCheck(root, stdout, stderr)
	case "update":
		return runWrite(root, false, stderr)
	case "init":
		return runWrite(root, true, stderr)
	case "report":
		if len(args) == 2 {
			return runReport(root, args[1], stderr)
		}
	}
	return fail(stderr, fmt.Errorf("unknown command %q", strings.Join(args, " ")))
}

func fail(w io.Writer, err error) int {
	if _, writeErr := fmt.Fprintf(w, "ratchet: %v\n", err); writeErr != nil {
		return 3
	}
	return 2
}

func isVendored(path string) bool {
	return strings.Contains(path, "ghdash/") || strings.Contains(path, "zenocto/")
}

func add(m map[string]counts, rule, path string, n int) {
	c := m[rule]
	if isVendored(path) {
		c.Vendored += n
	} else {
		c.First += n
	}
	m[rule] = c
}

func readJSON(root *os.Root, path string, into any) error {
	raw, err := root.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func ruleName(checkID string) string {
	return checkID[strings.LastIndex(checkID, ".")+1:]
}

// collect totals the current findings of every tool, per rule.
func collect(root *os.Root) (map[string]counts, error) {
	out := map[string]counts{}
	var lint lintReport
	if err := readJSON(root, gateDir+"/lint-full.json", &lint); err != nil {
		return nil, err
	}
	for _, i := range lint.Issues {
		add(out, "golangci/"+i.FromLinter, i.Pos.Filename, 1)
	}
	var sem semgrepReport
	if err := readJSON(root, gateDir+"/semgrep.json", &sem); err != nil {
		return nil, err
	}
	for _, r := range sem.Results {
		add(out, "semgrep/"+ruleName(r.CheckID), r.Path, 1)
	}
	var dup jscpdReport
	if err := readJSON(root, gateDir+"/jscpd/jscpd-report.json", &dup); err != nil {
		return nil, err
	}
	for _, d := range dup.Duplicates {
		add(out, jscpdRule, d.FirstFile.Name, d.Lines)
	}
	dead, err := root.ReadFile(gateDir + "/deadcode.txt")
	if err != nil {
		return nil, fmt.Errorf("read deadcode output: %w", err)
	}
	countDeadcode(out, string(dead))
	return out, nil
}

func countDeadcode(out map[string]counts, text string) {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "unreachable func") {
			add(out, deadcodeRule, line, 1)
		}
	}
}

// compare returns one message per rule whose count rose, and the rules that fell.
func compare(base, cur map[string]counts) (regressions, improved []string) {
	rules := slices.Sorted(maps.Keys(cur))
	for _, rule := range slices.Sorted(maps.Keys(base)) {
		if _, ok := cur[rule]; !ok {
			rules = append(rules, rule)
		}
	}
	for _, rule := range rules {
		b, c := base[rule], cur[rule]
		if c.First > b.First {
			regressions = append(regressions, regression(rule, "first-party", b.First, c.First))
		}
		if c.Vendored > b.Vendored {
			regressions = append(regressions, regression(rule, "vendored", b.Vendored, c.Vendored))
		}
		if c.First < b.First || c.Vendored < b.Vendored {
			improved = append(improved, rule)
		}
	}
	return regressions, improved
}

func regression(rule, bucket string, was, now int) string {
	return fmt.Sprintf("%s:1: ratchet/%s [error]: %s count rose from %d to %d. "+
		"Fix: remove the new violations. Never raise the baseline to pass.", baselinePath, rule, bucket, was, now)
}

func runCheck(root *os.Root, stdout, stderr io.Writer) int {
	var base baseline
	if err := readJSON(root, baselinePath, &base); err != nil {
		return fail(stderr, err)
	}
	cur, err := collect(root)
	if err != nil {
		return fail(stderr, err)
	}
	regressions, improved := compare(base.Rules, cur)
	if len(regressions) > 0 {
		if _, err := fmt.Fprintln(stderr, strings.Join(regressions, "\n")); err != nil {
			return fail(stderr, err)
		}
		return 1
	}
	if len(improved) > 0 {
		if _, err := fmt.Fprintf(stdout, "ratchet: debt fell in %s. Run make quality-baseline to lock it in.\n",
			strings.Join(improved, ", ")); err != nil {
			return fail(stderr, err)
		}
	}
	return 0
}

// lowered keeps each rule at the smaller of its old and new count.
func lowered(base, cur map[string]counts) map[string]counts {
	out := map[string]counts{}
	for rule, b := range base {
		c := cur[rule]
		out[rule] = counts{First: min(b.First, c.First), Vendored: min(b.Vendored, c.Vendored)}
	}
	return out
}

func runWrite(root *os.Root, initial bool, stderr io.Writer) int {
	cur, err := collect(root)
	if err != nil {
		return fail(stderr, err)
	}
	if !initial {
		var base baseline
		if err := readJSON(root, baselinePath, &base); err != nil {
			return fail(stderr, err)
		}
		cur = lowered(base.Rules, cur)
	}
	raw, err := json.MarshalIndent(baseline{Rules: cur}, "", "  ")
	if err != nil {
		return fail(stderr, err)
	}
	if err := root.WriteFile(baselinePath, append(raw, '\n'), 0o644); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func hints() map[string]string {
	return map[string]string{
		"errcheck":         "handle the error, or return it wrapped with %w",
		"wrapcheck":        "wrap with fmt.Errorf(\"context: %w\", err)",
		"noctx":            "use the CommandContext or WithContext form with a deadline",
		"contextcheck":     "pass the caller's ctx down",
		"forbidigo":        "follow the message above",
		"gochecknoglobals": "move the value into a struct or function",
		"gochecknoinits":   "call an explicit setup function",
		"gocognit":         "extract helpers until each function has one job",
		"cyclop":           "extract helpers or use a lookup table",
		"funlen":           "split the function along its steps",
		"nestif":           "return early to flatten the branches",
		"dupl":             "extract the shared block",
		"goconst":          "name the repeated literal",
		"depguard":         "keep the import layering in AGENTS.md",
		"nolintlint":       "remove the directive and fix the code",
		"gocritic":         "apply the suggested rewrite",
		"gosec":            "fix the unsafe pattern",
		"revive":           "group parameters in a struct, or flatten the nesting",
	}
}

func hint(linter string) string {
	if h, ok := hints()[linter]; ok {
		return h
	}
	return "fix the code. Never add //nolint"
}

func runReport(root *os.Root, kind string, stderr io.Writer) int {
	var lines []string
	var err error
	switch kind {
	case "lint":
		lines, err = lintLines(root)
	case "semgrep":
		lines, err = semgrepLines(root)
	default:
		err = fmt.Errorf("unknown report %q", kind)
	}
	if err != nil {
		return fail(stderr, err)
	}
	if len(lines) == 0 {
		return 0
	}
	if _, err := fmt.Fprintln(stderr, strings.Join(lines, "\n")); err != nil {
		return fail(stderr, err)
	}
	return 1
}

func lintLines(root *os.Root) ([]string, error) {
	var report lintReport
	if err := readJSON(root, gateDir+"/lint-new.json", &report); err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(report.Issues))
	for _, i := range report.Issues {
		lines = append(lines, fmt.Sprintf("%s:%d:%d: golangci/%s [error]: %s. Fix: %s.",
			i.Pos.Filename, i.Pos.Line, i.Pos.Column, i.FromLinter, i.Text, hint(i.FromLinter)))
	}
	return lines, nil
}

func semgrepLines(root *os.Root) ([]string, error) {
	var report semgrepReport
	if err := readJSON(root, gateDir+"/semgrep.json", &report); err != nil {
		return nil, err
	}
	added, err := difflines.Load(root, gateDir+"/diff")
	if err != nil {
		return nil, fmt.Errorf("load changed lines: %w", err)
	}
	var lines []string
	for _, r := range report.Results {
		if added[r.Path][r.Start.Line] {
			lines = append(lines, fmt.Sprintf("%s:%d:%d: semgrep/%s [error]: %s",
				r.Path, r.Start.Line, r.Start.Col, ruleName(r.CheckID), r.Extra.Message))
		}
	}
	return lines, nil
}
