package prview_test

import (
	"strings"
	"testing"

	"github.com/tmih06/nightshift/internal/zenocto/tui/prview"
)

func finishDiff(m prview.Model) prview.Model {
	if cmd := m.TakeDiffRender(); cmd != nil {
		m, _ = m.Update(cmd())
	}
	return m
}

func TestLargeFileRenderFollowsTheLatestSelection(t *testing.T) {
	files := bigDiff(3, 200)
	for i, label := range []string{"FIRST_FILE", "SECOND_FILE", "THIRD_FILE"} {
		files[i].Path = label + ".go"
		files[i].Hunks[0].Lines[0].Content = "// " + label
	}
	m := detailed(held(sampleDetail()), 180, 44)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "]", "]", "]", "1")
	first := m.TakeDiffRender()
	if first == nil || !strings.Contains(stripANSI(m.View()), "Preparing the diff") {
		t.Fatal("large diff was painted on the input loop")
	}

	m = press(m, "j", "j")
	m.SetSize(155, 36)
	if m.TakeDiffRender() != nil {
		t.Fatal("started a second worker while preparing the first file")
	}
	m, _ = m.Update(first())
	if out := stripANSI(m.View()); strings.Contains(out, "// FIRST_FILE") || !strings.Contains(out, "Preparing the diff") {
		t.Fatal("an older result replaced the latest selection")
	}
	m = finishDiff(m)
	if out := stripANSI(m.View()); !strings.Contains(out, "// THIRD_FILE") || strings.Contains(out, "// SECOND_FILE") {
		t.Fatalf("did not render the final selection at the new size:\n%s", out)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines != 36 {
		t.Fatalf("rendered %d lines after resize, want 36", lines)
	}
}

func TestLargeDiffJumpLandsWhenTheThreadIsReady(t *testing.T) {
	d := sampleDetail()
	files := bigDiff(1, 200)
	d.Threads[3].Path = files[0].Path
	d.Threads[3].Line = 140
	m := walked(detailed(held(d), 200, jumpHeight), tabOther)
	m.SetFiles(loadedFiles(files, 0))
	m = press(m, "enter")
	if !strings.Contains(stripANSI(m.View()), "Preparing the diff") {
		t.Fatal("thread jump did not queue its large diff")
	}
	m = finishDiff(m)
	if !strings.Contains(stripANSI(m.View()), "Is r free after the move?") {
		t.Fatal("thread jump did not resume after background rendering")
	}
}

func TestLargeCommitDiscardsAReplacedPatch(t *testing.T) {
	m := onCommits(180, 44)
	sha := sampleCommits()[0].SHA
	files := bigDiff(3, 200)
	files[0].Hunks[0].Lines[0].Content = "// OLD_PATCH"
	m.SetCommitFiles(sha, commitDiff(files))
	old := m.TakeDiffRender()
	if old == nil {
		t.Fatal("large commit was painted on the input loop")
	}

	files = bigDiff(3, 200)
	files[0].Hunks[0].Lines[0].Content = "// NEW_PATCH"
	m.SetCommitFiles(sha, commitDiff(files))
	m, _ = m.Update(old())
	if out := stripANSI(m.View()); strings.Contains(out, "OLD_PATCH") || !strings.Contains(out, "Preparing the diff") {
		t.Fatal("an obsolete patch replaced the refresh")
	}
	m = finishDiff(m)
	if !strings.Contains(stripANSI(m.View()), "NEW_PATCH") {
		t.Fatal("refreshed patch did not render")
	}
}
