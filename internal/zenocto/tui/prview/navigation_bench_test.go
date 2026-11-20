package prview_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/tui/comp"
)

// Use the flat panes Nightshift ships, rather than the classic test chrome.
func nightshiftChrome(b *testing.B) {
	b.Helper()
	was := comp.ClassicChrome
	comp.ClassicChrome = false
	b.Cleanup(func() { comp.ClassicChrome = was })
}

func BenchmarkConversationKeyToFrame(b *testing.B) {
	nightshiftChrome(b)
	d := sampleDetail()
	for i := range 60 {
		d.Timeline = append(d.Timeline, commented(fmt.Sprintf("reviewer-%d", i), time.Now().Add(-time.Hour),
			fmt.Sprintf("Review note %d\n\n", i)+strings.Repeat("This is a detailed note about the proposed change, with some **markdown**.\n\n", 5)))
	}
	m := detailed(held(d), 180, 44)
	for b.Loop() {
		m = press(m, "}")
		_ = m.View()
		m = press(m, "{")
		_ = m.View()
	}
}

func BenchmarkDiffLineKeyToFrame(b *testing.B) {
	nightshiftChrome(b)
	m := detailed(held(sampleDetail()), 180, 44)
	m.SetFiles(loadedFiles(bigDiff(1, 1800), 0))
	m = finishDiff(press(m, "]", "]", "]", "2"))
	m = press(m, "j", "j")
	for b.Loop() {
		m = press(m, "j")
		_ = m.View()
		m = press(m, "k")
		_ = m.View()
	}
}

// Delivers a multi-file patch from the store, after the commit network wait.
// Measures the immediate frame; the worker is not included.
func BenchmarkCommitPatchToFrame(b *testing.B) {
	nightshiftChrome(b)
	files := commitDiff(bigDiff(12, 200))
	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		m := onCommits(180, 44)
		b.StartTimer()
		m.SetCommitFiles(sampleCommits()[0].SHA, files)
		_ = m.TakeDiffRender()
		_ = m.View()
	}
}

// Measures input handling and the immediate frame before background work.
func BenchmarkColdDiffKeyToFrame(b *testing.B) {
	nightshiftChrome(b)
	files := bigDiff(32, 200)
	for i, f := range files {
		for j := range f.Hunks[0].Lines {
			line := &f.Hunks[0].Lines[j]
			line.Content += fmt.Sprintf(" // file %d", i)
			if j%7 == 1 {
				line.Kind = gh.DiffRemoved
			}
		}
	}
	m := detailed(held(sampleDetail()), 180, 44)
	for i := 0; b.Loop(); i++ {
		if i%(len(files)-1) == 0 {
			b.StopTimer()
			m = detailed(held(sampleDetail()), 180, 44)
			m.SetFiles(loadedFiles(files, 0))
			m = press(m, "]", "]", "]", "1")
			b.StartTimer()
		}
		m = press(m, "j")
		_ = m.TakeDiffRender()
		_ = m.View()
	}
}
