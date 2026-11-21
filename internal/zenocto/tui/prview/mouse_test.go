package prview_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/store"
)

func TestMouseClicksSwitchPRViewTabs(t *testing.T) {
	m := detailed(held(sampleDetail()), 160, 24)

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	tabLine := lines[3]

	for _, want := range []string{"Commits", "Checks", "Files", "Conversation"} {
		col := strings.Index(tabLine, want)
		if col < 0 {
			t.Fatalf("tab %q not found in row 3: %q", want, tabLine)
		}
		var cmd tea.Cmd
		m, cmd = m.Update(tea.MouseClickMsg{X: col + 1, Y: 3, Button: tea.MouseLeft})
		_ = cmd
		if got := currentTab(t, m.View()); got != want {
			t.Errorf("clicking %q at (%d, 3) landed on %q", want, col+1, got)
		}
	}
}

func TestMouseWheelScrollsMainAndStepsSide(t *testing.T) {
	d := sampleDetail()
	for i := range 30 {
		c := gh.Comment{
			Kind:   gh.CommentIssue,
			ID:     fmt.Sprintf("C_%d", i),
			Author: gh.Actor{Login: "bob"},
			Body:   fmt.Sprintf("Comment number %d\nwith extra lines\nto force scrolling", i),
		}
		d.Timeline = append(d.Timeline, gh.TimelineItem{
			Kind:    gh.TimelineComment,
			Actor:   c.Author,
			Comment: &c,
		})
	}
	m := detailed(held(d), 160, 20)

	before := ansi.Strip(m.View())
	m, _ = m.Update(tea.MouseWheelMsg{X: 100, Y: 10, Button: tea.MouseWheelDown})
	after := ansi.Strip(m.View())
	if after == before {
		t.Error("wheel down over main pane did not scroll the conversation")
	}

	// Switch to Files tab and click/wheel in the side column.
	m = press(m, "]", "]", "]")
	files := store.Files{
		Loaded: true,
		Files: []gh.ChangedFile{
			{Path: "alpha.go", Status: gh.FileModified, Hunks: []gh.Hunk{{Header: "@@ -1 +1 @@", Lines: []gh.DiffLine{{Kind: gh.DiffAdded, New: 1, Content: "a"}}}}},
			{Path: "beta.go", Status: gh.FileModified, Hunks: []gh.Hunk{{Header: "@@ -1 +1 @@", Lines: []gh.DiffLine{{Kind: gh.DiffAdded, New: 1, Content: "b"}}}}},
		},
	}
	m.SetFiles(files)

	// Wheel down in side pane moves selection to beta.go.
	m, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 7, Button: tea.MouseWheelDown})
	if !strings.Contains(ansi.Strip(m.View()), "beta.go") {
		t.Error("wheel down in Files side column did not advance selection")
	}

	// Clicking row 0 (alpha.go) selects alpha.go again.
	lead := m.HeadLead()
	m, _ = m.Update(tea.MouseClickMsg{X: 5, Y: lead + 1, Button: tea.MouseLeft})
	if !strings.Contains(ansi.Strip(m.View()), "alpha.go") {
		t.Error("clicking alpha.go in Files side column did not select it")
	}
}

func dividerCol(line string) int {
	// Classic chrome draws "││" between panes; flat chrome draws a single "│" away from column 0.
	if idx := strings.Index(line, "││"); idx >= 0 {
		return ansi.StringWidth(line[:idx]) + 1
	}
	for i := 1; i < len(line); i++ {
		if strings.HasPrefix(line[i:], "│") {
			return ansi.StringWidth(line[:i])
		}
	}
	return -1
}

func TestMouseDragResizesPRViewDivider(t *testing.T) {
	m := detailed(held(sampleDetail()), 160, 24)
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	lead := m.HeadLead()
	initialDiv := dividerCol(lines[lead+1])
	if initialDiv <= 0 {
		t.Fatalf("could not find pane divider in %q", lines[lead+1])
	}

	// Grab divider and drag right by 10 columns.
	m, _ = m.Update(tea.MouseClickMsg{X: initialDiv, Y: lead + 2, Button: tea.MouseLeft})
	m, _ = m.Update(tea.MouseMotionMsg{X: initialDiv + 10, Y: lead + 2, Button: tea.MouseLeft})
	m, _ = m.Update(tea.MouseReleaseMsg{X: initialDiv + 10, Y: lead + 2, Button: tea.MouseLeft})

	afterLines := strings.Split(ansi.Strip(m.View()), "\n")
	newDiv := dividerCol(afterLines[lead+1])
	if newDiv <= initialDiv {
		t.Errorf("divider stayed at %d after dragging right from %d", newDiv, initialDiv)
	}

	// Motion after release must not move divider again.
	m, _ = m.Update(tea.MouseMotionMsg{X: initialDiv, Y: lead + 2, Button: tea.MouseLeft})
	finalLines := strings.Split(ansi.Strip(m.View()), "\n")
	if got := dividerCol(finalLines[lead+1]); got != newDiv {
		t.Errorf("divider moved to %d after release, want %d", got, newDiv)
	}
}

func TestMouseClickFocusesPaneAndSelectsCommitsAndChecks(t *testing.T) {
	d := sampleDetail()
	d.Commits = []gh.Commit{
		{SHA: "1111111", Short: "1111111", Headline: "First commit"},
		{SHA: "2222222", Short: "2222222", Headline: "Second commit"},
	}
	d.Rollup = gh.CheckRollup{
		Checks: []gh.Check{
			{Name: "build", Workflow: "CI", RunID: 1, JobID: 10, State: gh.CheckStateSuccess},
			{Name: "test", Workflow: "CI", RunID: 1, JobID: 11, State: gh.CheckStateFailure},
		},
	}
	m := detailed(held(d), 160, 24)
	lead := m.HeadLead()

	// Switch to Commits tab via click.
	tabLine := strings.Split(ansi.Strip(m.View()), "\n")[3]
	m, _ = m.Update(tea.MouseClickMsg{X: strings.Index(tabLine, "Commits") + 1, Y: 3, Button: tea.MouseLeft})
	if got := currentTab(t, m.View()); got != "Commits" {
		t.Fatalf("tab = %q, want Commits", got)
	}
	// Click second commit (commitRowHeight = 2 -> row index 2 inside side body).
	var cmd tea.Cmd
	m, cmd = m.Update(tea.MouseClickMsg{X: 5, Y: lead + 1 + 2, Button: tea.MouseLeft})
	if cmd == nil {
		t.Error("clicking second commit did not arm commit diff fetch")
	}

	// Switch to Checks tab via click.
	m, _ = m.Update(tea.MouseClickMsg{X: strings.Index(tabLine, "Checks") + 1, Y: 3, Button: tea.MouseLeft})
	if got := currentTab(t, m.View()); got != "Checks" {
		t.Fatalf("tab = %q, want Checks", got)
	}
	// Click parent workflow row (row 0) to fold it.
	beforeFold := ansi.Strip(m.View())
	m, _ = m.Update(tea.MouseClickMsg{X: 5, Y: lead + 1, Button: tea.MouseLeft})
	afterFold := ansi.Strip(m.View())
	if !strings.Contains(beforeFold, "▾") || !strings.Contains(afterFold, "▸") {
		t.Errorf("clicking workflow group row did not toggle fold")
	}
}
