package repoview

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	ghctx "github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func newBenchRepoModel(b *testing.B, withReadmes bool) *Model {
	b.Helper()
	const numRepos = 50
	fc := newFakeClient()
	fc.repos = make([]Repo, numRepos)
	fc.overviews = make(map[string]Repo, numRepos)
	fc.branches = make(map[string][]Branch, numRepos)
	now := time.Now()

	sampleReadme := func(n int) string {
		return fmt.Sprintf(`# Project %d

A full-featured service and CLI repository #%d with documentation, architecture notes, and examples.

## Quick start

`+"```go"+`
package main

import "fmt"

func main() {
	for i := 0; i < %d; i++ {
		fmt.Println("running task", i)
	}
}
`+"```"+`

## Architecture

- Keeps **state** isolated per worker and updates asynchronously
- Supports `+"`config.yml`"+`, environment overrides, and structured logs
- Ships unit tests, integration tests, and benchmarks

`, n, n, n) + strings.Repeat("\nDetailed operational notes explaining scaling, retries, timeouts, and observability.\n", 8)
	}

	for i := range numRepos {
		fn := fmt.Sprintf("alice/project-%02d", i)
		r := Repo{
			Owner:         "alice",
			Name:          fmt.Sprintf("project-%02d", i),
			FullName:      fn,
			Description:   fmt.Sprintf("Repository number %d for navigation performance benchmark", i),
			DefaultBranch: "main",
			Language:      "Go",
			Stars:         10 + i,
			Forks:         2,
			OpenIssues:    3,
			URL:           "https://github.com/" + fn,
			Visibility:    "public",
			Topics:        []string{"go", "tui", "sandbox"},
			UpdatedAt:     now.Add(-time.Duration(i) * time.Hour),
		}
		if withReadmes {
			r.Readme = sampleReadme(i)
		}
		fc.repos[i] = r
		fc.overviews[fn] = r
		fc.branches[fn] = []Branch{
			{Name: "main", SHA: "abcdef1234567", IsDefault: true, UpdatedAt: now},
			{Name: fmt.Sprintf("feat/work-%d", i), SHA: "1234567abcdef", UpdatedAt: now.Add(-time.Hour)},
		}
	}

	th := *ghtheme.DefaultTheme
	styles := ghctx.InitStyles(th)
	m := New(th, styles, Options{
		CurrentRepo: "alice/project-00",
		PinsPath:    filepath.Join(b.TempDir(), "pins.json"),
		Client:      fc,
	})
	m.SetSandboxes([]ActiveSandbox{
		{Pod: "pi-project-00-1", Volume: "repo-alice-project-00-main", Phase: "Running"},
		{Pod: "codex-project-05-1", Volume: "repo-alice-project-05-main", Phase: "Running"},
	})
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 44})
	m.loaded = true
	m.viewer = "alice"
	m.repos = append([]Repo(nil), fc.repos...)
	if withReadmes {
		for _, r := range fc.repos {
			rc := m.getOrCreateCache(r.FullName)
			rc.overviewLoaded = true
			rc.branchesLoaded = true
			rc.branches = fc.branches[r.FullName]
		}
	}
	m.expandedRepos = map[string]bool{}
	m.ensureNavCursorValid()
	m.syncSelectionFromNav()
	_ = m.View()
	return m
}

func BenchmarkRepoNavKeyToFrame(b *testing.B) {
	m := newBenchRepoModel(b, true)
	down := tea.KeyPressMsg{Text: "j"}
	up := tea.KeyPressMsg{Text: "k"}
	b.ResetTimer()
	for b.Loop() {
		m.Update(down)
		_ = m.View()
		m.Update(up)
		_ = m.View()
	}
}

func BenchmarkRepoNavWheelToFrame(b *testing.B) {
	m := newBenchRepoModel(b, true)
	down := tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelUp}
	b.ResetTimer()
	for b.Loop() {
		m.Update(down)
		_ = m.View()
		m.Update(up)
		_ = m.View()
	}
}

func BenchmarkRepoViewOnly(b *testing.B) {
	m := newBenchRepoModel(b, true)
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkRepoNavColdSweep20(b *testing.B) {
	down := tea.KeyPressMsg{Text: "j"}
	for b.Loop() {
		b.StopTimer()
		m := newBenchRepoModel(b, false)
		b.StartTimer()
		fetches := 0
		for range 20 {
			_, cmd := m.Update(down)
			if cmd != nil {
				fetches++
			}
			_ = m.View()
		}
		b.ReportMetric(float64(fetches), "cmds/sweep")
	}
}

func BenchmarkRepoNavColdReadme(b *testing.B) {
	m := newBenchRepoModel(b, true)
	block := "\n```go\nfunc process(items []string) int {\n\ttotal := 0\n\tfor _, item := range items {\n\t\ttotal += len(item)\n\t}\n\treturn total\n}\n```\n\n" +
		"- Bullet point describing **architecture**, `config.yml`, and edge cases.\n" +
		"- Follow-up item covering rollout and verification.\n"
	bigTail := strings.Repeat(block, 15)
	down := tea.KeyPressMsg{Text: "j"}
	up := tea.KeyPressMsg{Text: "k"}
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		idx := (i % 2) ^ 1
		m.repos[idx].Readme = fmt.Sprintf("# Repo %d\n\nOverview paragraph %d.\n", i, i) + bigTail
		b.StartTimer()
		if i%2 == 0 {
			m.Update(down)
		} else {
			m.Update(up)
		}
		_ = m.View()
	}
}
