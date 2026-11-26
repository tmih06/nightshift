package app

import (
	"testing"
	"unsafe"

	tea "charm.land/bubbletea/v2"
)

type unrelatedMsg struct{}

// BenchmarkAnUnrelatedMessageThroughThePullRequest is the cost every
// gh-dash message pays once a pull request has been opened.
func BenchmarkAnUnrelatedMessageThroughThePullRequest(b *testing.B) {
	t := &testing.T{}
	m, _ := openOn(t)
	_ = unsafe.Sizeof(*m.pr) // ~95 KB: why prWants keeps gh-dash's traffic away
	b.ResetTimer()
	for range b.N {
		_ = m.updatePR(unrelatedMsg{})
	}
}

func BenchmarkThePullRequestFrame(b *testing.B) {
	t := &testing.T{}
	m, _ := openOn(t)
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}

var _ = tea.Batch
