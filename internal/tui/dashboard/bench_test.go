package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func benchBase() model {
	m := baseModel()
	m.rowCache = &rowCacheEntry{}
	m.inspectDocs = map[inspectKey]inspectCache{}
	m.width, m.height = 150, 42
	p := m.pools["agent-pool"]
	p.TemplateYAML = strings.Repeat("apiVersion: v1\nkind: Pod\nspec:\n  replicas: 2\n", 80)
	m.pools["agent-pool"] = p
	return m
}

func BenchmarkViewWarm(b *testing.B) {
	m := benchBase()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkViewClaimedShell(b *testing.B) {
	m := benchBase()
	m.selected = "agent-pool-bkl2p"
	m.sessions["agent-pool-bkl2p"] = true
	m.shellPod = "agent-pool-bkl2p"
	for i := 0; i < 40; i++ {
		m.shellLines = append(m.shellLines, "\x1b[32mnode@pod\x1b[0m:~/workspace$ ls -la some/long/path/here")
	}
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkMoveSelection(b *testing.B) {
	m := benchBase()
	for b.Loop() {
		m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	}
}
