// Command diff-check fails when a change weakens tests or the quality gate.
// It reads the git output saved by its run script, which compares the working
// tree with the merge base of GATE_BASE.
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const agentsFile = "AGENTS.md"

var (
	assertionRE = regexp.MustCompile(`\bt\.(Error|Errorf|Fatal|Fatalf|Fail|FailNow)\(|\b(assert|require)\.[A-Z]`)
	skipRE      = regexp.MustCompile(`\b[tb]\.(Skip|Skipf|SkipNow)\(`)
	testFuncRE  = regexp.MustCompile(`^func (Test|Fuzz)[A-Z0-9_]`)
	hunkRE      = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	modNoteRE   = regexp.MustCompile(`(?m)^Go-Mod-Justification:\s*\S+`)
	noGuardRE   = regexp.MustCompile(`(?m)^No-guardrail:\s*\S+`)
	fixTitleRE  = regexp.MustCompile(`^fix(\(|!|:)`)
)

// finding is one reported violation, printed as file:line: rule: message.
type finding struct {
	file, rule, msg string
	line            int
	warn            bool
}

func (f finding) String() string {
	level := "error"
	if f.warn {
		level = "warning"
	}
	return fmt.Sprintf("%s:%d: %s [%s]: %s", f.file, f.line, f.rule, level, f.msg)
}

// fileDiff is the part of one file's diff that the rules read.
type fileDiff struct {
	path, status      string
	added             []addedLine
	removedAssertions int
	addedAssertions   int
	addedTests        int
}

type addedLine struct {
	n    int
	text string
}

// options carries the pull request context the rules depend on.
type options struct {
	prBody, prTitle string
	subjects        []string
	ownerApproved   bool
	bootstrap       bool
	inCI            bool
	guardrailBlocks bool
}

func main() {
	os.Exit(run())
}

func run() int {
	root, err := os.OpenRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff-check: open repo root: %v\n", err)
		return 2
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "diff-check: close repo root: %v\n", closeErr)
		}
	}()
	dir := os.Getenv("GATE_DIFF_DIR")
	if dir == "" {
		dir = ".tools/gate/diff"
	}
	files, opts, err := load(root, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff-check: %v. Run it through scripts/quality/diff-check/run.\n", err)
		return 2
	}
	failed := false
	for _, f := range check(files, opts) {
		fmt.Fprintln(os.Stderr, f)
		failed = failed || !f.warn
	}
	if failed {
		return 1
	}
	return 0
}

// load reads the git output that the run script saved under dir.
func load(root *os.Root, dir string) ([]*fileDiff, options, error) {
	inputs := map[string]string{}
	for _, name := range []string{"status", "diff", "untracked", "subjects"} {
		content, err := root.ReadFile(dir + "/" + name)
		if err != nil {
			return nil, options{}, fmt.Errorf("read %s input: %w", name, err)
		}
		inputs[name] = string(content)
	}
	files := parseDiff(inputs["diff"], parseStatus(inputs["status"]))
	for _, path := range strings.Fields(inputs["untracked"]) {
		fd, err := untrackedDiff(root, path)
		if err != nil {
			return nil, options{}, err
		}
		files = append(files, fd)
	}
	_, statErr := root.Stat(dir + "/bootstrap")
	return files, loadOptions(inputs["subjects"], statErr == nil), nil
}

func loadOptions(subjects string, bootstrap bool) options {
	return options{
		prBody:          os.Getenv("PR_BODY"),
		prTitle:         os.Getenv("PR_TITLE"),
		subjects:        strings.Split(strings.TrimSpace(subjects), "\n"),
		ownerApproved:   os.Getenv("GATE_OWNER_APPROVED") == "1",
		bootstrap:       bootstrap,
		inCI:            os.Getenv("GATE_CI") == "1",
		guardrailBlocks: os.Getenv("GATE_GUARDRAIL_BLOCK") == "1",
	}
}

func untrackedDiff(root *os.Root, path string) (*fileDiff, error) {
	content, err := root.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read untracked %s: %w", path, err)
	}
	fd := &fileDiff{path: path, status: "A"}
	for i, line := range strings.Split(string(content), "\n") {
		recordAdded(fd, i+1, line)
	}
	return fd, nil
}

func parseStatus(out string) map[string]string {
	status := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) == 2 {
			status[parts[1]] = parts[0]
		}
	}
	return status
}

func parseDiff(raw string, status map[string]string) []*fileDiff {
	var files []*fileDiff
	var cur *fileDiff
	lineNo := 0
	for _, line := range strings.Split(raw, "\n") {
		if path, ok := strings.CutPrefix(line, "diff --git a/"); ok {
			name := path[strings.Index(path, " b/")+3:]
			cur = &fileDiff{path: name, status: status[name]}
			files = append(files, cur)
			continue
		}
		if cur == nil {
			continue
		}
		if m := hunkRE.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				lineNo = n
			}
			continue
		}
		lineNo = applyLine(cur, line, lineNo)
	}
	return files
}

func applyLine(fd *fileDiff, line string, lineNo int) int {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return lineNo
	case strings.HasPrefix(line, "+"):
		recordAdded(fd, lineNo, line[1:])
		return lineNo + 1
	case strings.HasPrefix(line, "-"):
		if assertionRE.MatchString(line) {
			fd.removedAssertions++
		}
	}
	return lineNo
}

func recordAdded(fd *fileDiff, n int, text string) {
	fd.added = append(fd.added, addedLine{n: n, text: text})
	if assertionRE.MatchString(text) {
		fd.addedAssertions++
	}
	if testFuncRE.MatchString(text) {
		fd.addedTests++
	}
}

func check(files []*fileDiff, opts options) []finding {
	var out []finding
	for _, fd := range files {
		out = append(out, checkContent(fd)...)
		out = append(out, checkAssertions(fd)...)
	}
	out = append(out, checkDeletedTests(files)...)
	out = append(out, checkProtected(files, opts)...)
	out = append(out, checkGoMod(files, opts)...)
	return append(out, checkGuardrail(files, opts)...)
}

func checkContent(fd *fileDiff) []finding {
	if !strings.HasSuffix(fd.path, ".go") || isGateFile(fd.path) {
		return nil
	}
	var out []finding
	for _, l := range fd.added {
		if strings.Contains(l.text, "//nolint") {
			out = append(out, finding{file: fd.path, line: l.n, rule: "diff/no-nolint",
				msg: "new //nolint. Fix the code. If the rule seems wrong, stop and ask a human."})
		}
		if skipRE.MatchString(l.text) {
			out = append(out, finding{file: fd.path, line: l.n, rule: "diff/no-skip",
				msg: "new test skip. Make the test pass, or ask a human to retire it."})
		}
		if strings.Contains(l.text, "//go:build ignore") {
			out = append(out, finding{file: fd.path, line: l.n, rule: "diff/no-build-ignore",
				msg: "this hides the file from every check. Delete it or make it build."})
		}
	}
	return out
}

func checkAssertions(fd *fileDiff) []finding {
	if !strings.HasSuffix(fd.path, "_test.go") || fd.status == "D" || fd.removedAssertions <= fd.addedAssertions {
		return nil
	}
	return []finding{{file: fd.path, line: 1, rule: "diff/assertion-removed",
		msg: fmt.Sprintf("removes %d assertions and adds %d. Keep the checks, or replace them with equivalent ones.",
			fd.removedAssertions, fd.addedAssertions)}}
}

func checkDeletedTests(files []*fileDiff) []finding {
	addedIn := map[string]bool{}
	for _, fd := range files {
		if strings.HasSuffix(fd.path, "_test.go") && fd.status != "D" && fd.addedTests > 0 {
			addedIn[dirOf(fd.path)] = true
		}
	}
	var out []finding
	for _, fd := range files {
		if strings.HasSuffix(fd.path, "_test.go") && fd.status == "D" && !addedIn[dirOf(fd.path)] {
			out = append(out, finding{file: fd.path, line: 1, rule: "diff/test-deleted",
				msg: "deletes a test file with no replacement in its package. Restore it or add a replacement."})
		}
	}
	return out
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

func isGateFile(path string) bool {
	return strings.HasPrefix(path, "scripts/quality/") || strings.HasPrefix(path, ".semgrep/")
}

func isProtected(path string) bool {
	switch path {
	case ".golangci.yml", ".semgrepignore", ".github/CODEOWNERS", "docs/quality-baseline.json",
		"Makefile", ".agents/settings.json", ".claude/settings.json", agentsFile:
		return true
	}
	return isGateFile(path) || strings.HasPrefix(path, ".github/workflows/")
}

func checkProtected(files []*fileDiff, opts options) []finding {
	if opts.ownerApproved || opts.bootstrap {
		return nil
	}
	var out []finding
	for _, fd := range files {
		if isProtected(fd.path) {
			out = append(out, finding{file: fd.path, line: 1, rule: "diff/protected-path",
				msg: "this file controls the gate and needs CODEOWNERS approval. Revert it, or stop and ask a human."})
		}
	}
	return out
}

func checkGoMod(files []*fileDiff, opts options) []finding {
	for _, fd := range files {
		if fd.path != "go.mod" || modNoteRE.MatchString(opts.prBody) {
			continue
		}
		return []finding{{file: "go.mod", line: 1, rule: "diff/go-mod-justification", warn: !opts.inCI,
			msg: "add a 'Go-Mod-Justification: <reason>' line to the PR description."}}
	}
	return nil
}

func isFixChange(opts options) bool {
	if fixTitleRE.MatchString(opts.prTitle) {
		return true
	}
	for _, s := range opts.subjects {
		if fixTitleRE.MatchString(s) {
			return true
		}
	}
	return false
}

func hasGuardrail(files []*fileDiff) bool {
	for _, fd := range files {
		if fd.path == agentsFile || fd.path == ".golangci.yml" || strings.HasPrefix(fd.path, ".semgrep/") {
			return true
		}
		if strings.HasSuffix(fd.path, "_test.go") && fd.addedTests > 0 {
			return true
		}
	}
	return false
}

func checkGuardrail(files []*fileDiff, opts options) []finding {
	if !isFixChange(opts) || hasGuardrail(files) || noGuardRE.MatchString(opts.prBody) {
		return nil
	}
	return []finding{{file: agentsFile, line: 1, rule: "diff/fix-needs-guardrail", warn: !opts.guardrailBlocks,
		msg: "a fix must add a rule or regression test, or a 'No-guardrail: <reason>' line in the PR description."}}
}
