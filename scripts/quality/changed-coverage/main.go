// Command changed-coverage fails when too few changed statements are tested.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/tmih06/nightshift/scripts/quality/internal/difflines"
)

const (
	// threshold matches first-party statement coverage (70.6%), rounded down.
	threshold  = 70.0
	modulePath = "github.com/tmih06/nightshift/"
	profile    = ".tools/gate/cover.out"
	diffDir    = ".tools/gate/diff"
)

type block struct {
	file             string
	start, end, stmt int
	hit              bool
}

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(stdout, stderr io.Writer) int {
	code, err := evaluateRepo(stdout, stderr)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "changed-coverage: %v\n", err); writeErr != nil {
			return 3
		}
		return 2
	}
	return code
}

func evaluateRepo(stdout, stderr io.Writer) (code int, err error) {
	root, err := os.OpenRoot(".")
	if err != nil {
		return 0, fmt.Errorf("open repo root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	raw, err := root.ReadFile(profile)
	if err != nil {
		return 0, fmt.Errorf("read coverage profile: %w", err)
	}
	added, err := difflines.Load(root, diffDir)
	if err != nil {
		return 0, fmt.Errorf("load changed lines: %w", err)
	}
	return report(parseProfile(string(raw)), added, stdout, stderr)
}

// parseProfile merges duplicate blocks, keeping a block hit if any run hit it.
func parseProfile(raw string) []block {
	merged := map[string]block{}
	for _, line := range strings.Split(raw, "\n") {
		b, key, ok := parseLine(line)
		if !ok {
			continue
		}
		if old, seen := merged[key]; seen {
			b.hit = b.hit || old.hit
		}
		merged[key] = b
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]block, 0, len(keys))
	for _, key := range keys {
		out = append(out, merged[key])
	}
	return out
}

func parseLine(line string) (block, string, bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 || strings.HasPrefix(line, "mode:") {
		return block{}, "", false
	}
	file, span, ok := strings.Cut(fields[0], ":")
	if !ok {
		return block{}, "", false
	}
	from, to, ok := strings.Cut(span, ",")
	if !ok {
		return block{}, "", false
	}
	start, startErr := strconv.Atoi(strings.Split(from, ".")[0])
	end, endErr := strconv.Atoi(strings.Split(to, ".")[0])
	stmt, stmtErr := strconv.Atoi(fields[1])
	count, countErr := strconv.Atoi(fields[2])
	if errors.Join(startErr, endErr, stmtErr, countErr) != nil {
		return block{}, "", false
	}
	b := block{file: strings.TrimPrefix(file, modulePath), start: start, end: end, stmt: stmt, hit: count > 0}
	return b, fields[0], true
}

func touches(b block, lines map[int]bool) bool {
	for n := b.start; n <= b.end; n++ {
		if lines[n] {
			return true
		}
	}
	return false
}

// tally returns covered and total changed statements, and the untested blocks.
func tally(blocks []block, added difflines.Lines) (covered, total int, missed []block) {
	for _, b := range blocks {
		if !touches(b, added[b.file]) {
			continue
		}
		total += b.stmt
		if b.hit {
			covered += b.stmt
		} else {
			missed = append(missed, b)
		}
	}
	return covered, total, missed
}

func report(blocks []block, added difflines.Lines, stdout, stderr io.Writer) (int, error) {
	covered, total, missed := tally(blocks, added)
	if total == 0 {
		_, err := fmt.Fprintln(stdout, "changed-coverage: no changed statements")
		return 0, wrapWrite(err)
	}
	pct := 100 * float64(covered) / float64(total)
	if pct >= threshold {
		_, err := fmt.Fprintf(stdout, "changed-coverage: %.1f%% of %d changed statements\n", pct, total)
		return 0, wrapWrite(err)
	}
	for _, b := range missed {
		if _, err := fmt.Fprintf(stderr, "%s:%d: coverage/changed-lines [error]: no test runs this block. "+
			"Fix: add a test that exercises it.\n", b.file, b.start); err != nil {
			return 0, wrapWrite(err)
		}
	}
	_, err := fmt.Fprintf(stderr, "changed-coverage: %.1f%% of %d changed statements, need %.0f%%\n", pct, total, threshold)
	return 1, wrapWrite(err)
}

func wrapWrite(err error) error {
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
