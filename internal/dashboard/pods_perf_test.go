package dashboard

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tmih06/nightshift/internal/cluster"
)

func podFleetModel(count int) model {
	m := fullPodModel()
	m.rowCache = &rowCacheEntry{}
	m.inspectDocs = map[inspectKey]inspectCache{}
	pods := m.boxes
	m.boxes = make([]cluster.Sandbox, count)
	for i := range m.boxes {
		m.boxes[i] = pods[i%len(pods)]
		m.boxes[i].Name = fmt.Sprintf("%s-%03d", m.boxes[i].Name, i)
	}
	m.selectPod(m.boxes[0].Name)
	return m
}

func TestTableRendersOnlyVisibleRows(t *testing.T) {
	m := podFleetModel(80)
	m.tableView(m.layout())
	if got := len(m.rowCache.rows); got == 0 || got > m.layout().tableH/itemH+1 {
		t.Fatalf("rendered %d pods for a %d-line viewport", got, m.layout().tableH)
	}
	first := m.cachedRow(0, 68)
	m.selectPod(m.boxes[1].Name)
	m.cachedRow(0, 68)
	m.selectPod(m.boxes[0].Name)
	if got := m.cachedRow(0, 68); &got[0] != &first[0] {
		t.Error("selection changes redrew an unchanged pod")
	}
	m.boxes[0].Phase = "Failed"
	m.dataRev++
	if got := m.cachedRow(0, 68); &got[0] == &first[0] || !slices.Equal(got, m.renderRow(0, 68, true)) {
		t.Error("fleet revision did not update the rendered pod")
	}
}

func TestTableViewportMatchesFullRendering(t *testing.T) {
	for _, width := range []int{60, 80, 120, 160} {
		for section := range sectionCount {
			m := podFleetModel(18)
			m.width, m.height, m.section = width, 19, section
			for _, index := range m.navigationIndices() {
				m.selectPod(m.boxes[index].Name)
				checkTableViewport(t, m)
			}
		}
	}
}

func checkTableViewport(t *testing.T, m model) {
	t.Helper()
	l := m.layout()
	inner := max(l.tableW-2, 1)
	entries := m.tableEntries()
	var all []string
	for _, e := range entries {
		if e.isHeading {
			all = append(all, m.renderHeadingRow(e.heading, inner))
		} else {
			all = append(all, m.renderRow(e.boxIdx, inner, m.boxes[e.boxIdx].Name == m.selected)...)
		}
	}
	start := m.tableLineOffset(entries, l)
	want := []string{m.tableHeader(inner), ""}
	want = append(want, all[start:min(start+l.tableH-tableHeaderH, len(all))]...)
	for len(want) < l.tableH {
		want = append(want, "")
	}
	for i := range want {
		want[i] = padTo(want[i], l.tableW)
	}
	if got := m.tableView(l); !slices.Equal(got, want) {
		t.Errorf("width=%d section=%d selected=%s: viewport differs from full rendering", m.width, m.section, m.selected)
	}
}

func TestInspectCacheUsesContent(t *testing.T) {
	for _, mode := range []sidebarMode{paneConfigs, paneLogs} {
		t.Run(subTabLabel(mode), func(t *testing.T) {
			m := podFleetModel(8)
			m.selected, m.sidebarMode = m.boxes[2].Name, mode
			m, _ = m.Update(logsMsg{pod: m.selected, out: "original log"})
			width := m.layout().docW
			first := m.cachedInspectLines(width)
			m, _ = m.Update(loadedMsg{boxes: m.boxes, pools: m.pools, metricsOK: true})
			m, _ = m.Update(logsMsg{pod: m.selected, out: "original log"})
			m, _ = m.Update(logsMsg{pod: m.boxes[3].Name, out: "another pod's log"})
			if got := m.cachedInspectLines(width); &got[0] != &first[0] {
				t.Error("unchanged document was rendered again after refresh")
			}
			m.boxes[2].ExtraYAML = "## changed config"
			m, _ = m.Update(loadedMsg{boxes: m.boxes, pools: m.pools, metricsOK: true})
			m, _ = m.Update(logsMsg{pod: m.selected, out: "changed log"})
			got := m.cachedInspectLines(width)
			if &got[0] == &first[0] || !strings.Contains(strings.Join(got, "\n"), "changed") {
				t.Error("changed content did not invalidate the document")
			}
			if resized := m.cachedInspectLines(width - 10); &resized[0] == &got[0] {
				t.Error("width change did not invalidate the document")
			}
		})
	}
}

func TestLogCacheTracksLoadingErrorsAndRecovery(t *testing.T) {
	m := podFleetModel(8)
	m.selected = m.boxes[2].Name
	if got := strings.Join(m.cachedInspectLines(60), "\n"); !strings.Contains(got, "press r") {
		t.Errorf("initial logs: %q", got)
	}
	m.maybeFetchLogs()
	if got := strings.Join(m.cachedInspectLines(60), "\n"); !strings.Contains(got, "loading logs") {
		t.Errorf("loading logs: %q", got)
	}
	m, _ = m.Update(logsMsg{pod: m.selected, err: errors.New("unavailable")})
	if got := strings.Join(m.cachedInspectLines(60), "\n"); !strings.Contains(got, "log error: unavailable") {
		t.Errorf("failed logs: %q", got)
	}
	m, _ = m.Update(logsMsg{pod: m.selected})
	if got := strings.Join(m.cachedInspectLines(60), "\n"); !strings.Contains(got, "no log output yet") {
		t.Errorf("recovered empty logs: %q", got)
	}
}

func TestRowCacheTracksSessionAttach(t *testing.T) {
	m := podFleetModel(8)
	m.selectPod(m.boxes[1].Name)
	before := m.cachedRow(1, 68)
	m, _ = m.Update(attachReadyMsg{pod: m.selected})
	after := m.cachedRow(1, 68)
	if &after[0] == &before[0] || !slices.Equal(after, m.renderRow(1, 68, true)) {
		t.Error("session attachment did not invalidate the pod row")
	}
}

func TestPodCachesTrackTheme(t *testing.T) {
	m := podFleetModel(8)
	doc := m.cachedInspectLines(60)
	row := m.cachedRow(0, 68)
	original := th
	defer useTheme(original)
	useTheme(original)
	if got := m.cachedInspectLines(60); &got[0] == &doc[0] {
		t.Error("document cache ignored theme revision")
	}
	if got := m.cachedRow(0, 68); &got[0] == &row[0] {
		t.Error("row cache ignored theme revision")
	}
}
