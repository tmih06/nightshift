// Package difflines finds the lines a change adds, from saved git output.
package difflines

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var hunkRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Lines maps a repo-relative path to its added line numbers.
type Lines map[string]map[int]bool

// Parse reads a zero-context unified diff.
func Parse(raw string) Lines {
	out := Lines{}
	current := ""
	for _, line := range strings.Split(raw, "\n") {
		if name, ok := strings.CutPrefix(line, "+++ b/"); ok {
			current = name
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			current = ""
			continue
		}
		m := hunkRE.FindStringSubmatch(line)
		if m == nil || current == "" {
			continue
		}
		addRange(out, current, m[1], m[2])
	}
	return out
}

func addRange(out Lines, path, startText, countText string) {
	start, err := strconv.Atoi(startText)
	if err != nil {
		return
	}
	count := 1
	if countText != "" {
		if count, err = strconv.Atoi(countText); err != nil {
			return
		}
	}
	for n := start; n < start+count; n++ {
		mark(out, path, n)
	}
}

func mark(out Lines, path string, n int) {
	if out[path] == nil {
		out[path] = map[int]bool{}
	}
	out[path][n] = true
}

// Load reads the diff saved in dir and counts untracked files as fully added.
func Load(root *os.Root, dir string) (Lines, error) {
	raw, err := root.ReadFile(dir + "/diff")
	if err != nil {
		return nil, fmt.Errorf("read saved diff: %w", err)
	}
	out := Parse(string(raw))
	untracked, err := root.ReadFile(dir + "/untracked")
	if err != nil {
		return nil, fmt.Errorf("read untracked list: %w", err)
	}
	for _, path := range strings.Fields(string(untracked)) {
		content, readErr := root.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read untracked file: %w", readErr)
		}
		for n := range strings.Count(string(content), "\n") + 1 {
			mark(out, path, n+1)
		}
	}
	return out, nil
}
