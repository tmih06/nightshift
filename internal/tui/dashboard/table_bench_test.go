package dashboard

import "testing"

func BenchmarkTableHeader(b *testing.B) {
	m := baseModel()
	m.rowCache = &rowCacheEntry{}
	for b.Loop() {
		_ = m.tableHeader(68)
	}
}

func BenchmarkRenderSandboxRow(b *testing.B) {
	m := baseModel()
	m.boxes[0].Template = "claude-code-template"
	for b.Loop() {
		_ = m.renderRow(0, 68, true)
	}
}
