package accountsview

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Meters are drawn with a heavy line, so meters on neighboring rows stay apart
// where solid blocks would merge into one.
const (
	meterLine = '━'
	meterHalf = '╸'
	meterTick = '┃'
)

// meterCells splits a meter of width cells at pct percent into full cells and
// half of the next one. Any use at all shows at least half a cell.
func meterCells(pct float64, width int) (full, half int) {
	units := int(max(0, min(100, pct))*float64(width)*2/100 + 0.5)
	if units == 0 && pct > 0 {
		units = 1
	}
	return units / 2, units % 2
}

// meter draws pct percent of width cells in fill over a track. A mark from 0
// to 1 puts a tick that far along; a negative one draws none.
func (m Model) meter(pct float64, width int, fill lipgloss.Style, mark float64) string {
	if width <= 0 {
		return ""
	}
	full, half := meterCells(pct, width)
	tick := -1
	if mark >= 0 {
		tick = min(width-1, int(mark*float64(width)))
	}
	// The track and tick take fill's background, such as the list's selection.
	bg := fill.GetBackground()
	styles := [...]lipgloss.Style{fill, lipgloss.NewStyle().Foreground(trackColor()).Background(bg), m.st.title.Background(bg)}
	const filled, track, marked = 0, 1, 2
	var b, run strings.Builder
	kind := filled
	// Cells of one kind share one escape sequence.
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(styles[kind].Render(run.String()))
			run.Reset()
		}
	}
	for i := range width {
		k, r := track, meterLine
		switch {
		case i == tick:
			k, r = marked, meterTick
		case i < full:
			k = filled
		case i == full && half > 0:
			k, r = filled, meterHalf
		}
		if k != kind {
			flush()
			kind = k
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

// severity colors a share of a limit that is used up.
func (m Model) severity(pct float64) lipgloss.Style {
	switch {
	case pct >= 90:
		return m.st.bad
	case pct >= 70:
		return m.st.warn
	default:
		return m.st.ok
	}
}

// share is a bar of width cells split between parts in proportion, each in its
// own color. Every part above zero gets a cell while there is room.
func share(parts []int64, styles []lipgloss.Style, width int) string {
	var total int64
	for _, p := range parts {
		total += max(0, p)
	}
	if total == 0 || width <= 0 {
		return ""
	}
	cells := make([]int, len(parts))
	used, largest := 0, 0
	for i, p := range parts {
		if p <= 0 {
			continue
		}
		cells[i] = max(1, int(float64(p)*float64(width)/float64(total)))
		used += cells[i]
		if cells[i] > cells[largest] {
			largest = i
		}
	}
	cells[largest] = max(0, cells[largest]+width-used)
	var b strings.Builder
	for i, n := range cells {
		if n > 0 {
			b.WriteString(styles[i].Render(strings.Repeat(string(meterLine), n)))
		}
	}
	return b.String()
}
