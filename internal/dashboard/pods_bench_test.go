package dashboard

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Include View: selection invalidates rendering, not just navigation state.
func BenchmarkPodsNavigation(b *testing.B) {
	for _, count := range []int{8, 80} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			m := podFleetModel(count)
			for b.Loop() {
				m, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
				_ = m.View()
				m, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
				_ = m.View()
			}
		})
	}
}

func BenchmarkPodsRefresh(b *testing.B) {
	for _, mode := range []sidebarMode{paneConfigs, paneLogs} {
		b.Run(subTabLabel(mode), func(b *testing.B) {
			m := podFleetModel(8)
			m.selected = m.boxes[2].Name
			m.sidebarMode = mode
			m.boxes[2].PodYAML = strings.Repeat("spec:\n  replicas: 2\n  image: nightshift:latest\n", 80)
			raw := strings.Repeat(`{"time":"2026-11-22T09:12:22Z","action":"ALLOW","method":"CONNECT","host":"registry.npmjs.org","port":443,"rule":"npm","duration_ms":20}`+"\n", podLogTailLines)
			m, _ = m.Update(logsMsg{pod: m.selected, out: raw})
			msg := loadedMsg{boxes: m.boxes, pools: m.pools, metricsOK: true}
			for b.Loop() {
				m, _ = m.Update(msg)
				m, _ = m.Update(logsMsg{pod: m.selected, out: raw})
				_ = m.View()
			}
		})
	}
}
