package accountsview

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ago is how long before now t was, as "3m ago", or "never" for a zero time.
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return span(now.Sub(t)) + " ago"
}

// until is how long until t, as "in 54m", or "expired 3m ago" once it passed.
func until(now, t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := t.Sub(now)
	if d <= 0 {
		return "expired " + span(-d) + " ago"
	}
	return "in " + span(d)
}

func span(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 && h < 10 {
			return fmt.Sprintf("%dh%02dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

const sparkBlocks = " ▁▂▃▄▅▆▇█"

// spark draws counts as a row of width cells, summing neighbors when there are
// more counts than cells. A window with requests always shows at least a low
// block, so light use stays visible next to a burst.
func spark(counts []int, width int) string {
	if width <= 0 || len(counts) == 0 {
		return ""
	}
	cells := make([]int, width)
	for i, n := range counts {
		cells[i*width/len(counts)] += n
	}
	peak := 0
	for _, n := range cells {
		peak = max(peak, n)
	}
	blocks := []rune(sparkBlocks)
	var b strings.Builder
	for _, n := range cells {
		switch {
		case n == 0 || peak == 0:
			b.WriteRune(blocks[0])
		default:
			b.WriteRune(blocks[max(1, n*(len(blocks)-1)/peak)])
		}
	}
	return b.String()
}

// spark64 draws int64 counts as a row of width cells, mirroring spark.
func spark64(counts []int64, width int) string {
	if width <= 0 || len(counts) == 0 {
		return ""
	}
	cells := make([]int64, width)
	for i, n := range counts {
		cells[i*width/len(counts)] += n
	}
	var peak int64
	for _, n := range cells {
		peak = max(peak, n)
	}
	blocks := []rune(sparkBlocks)
	var b strings.Builder
	for _, n := range cells {
		if n == 0 || peak == 0 {
			b.WriteRune(blocks[0])
			continue
		}
		idx := int(n * int64(len(blocks)-1) / peak)
		b.WriteRune(blocks[max(1, min(len(blocks)-1, idx))])
	}
	return b.String()
}

// compactCount formats a token count compactly (e.g. 12.5K, 1.4M).
func compactCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return strconv.FormatInt(n, 10)
	}
}

// quotaBar draws a filled/empty bar of width cells for a 0..100 percentage.
func quotaBar(pct float64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := int(pct*float64(width)/100 + 0.5)
	filled = max(0, min(width, filled))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// wrap breaks s into lines of at most width runes, with no regard for words,
// so a long address stays whole when copied back out of a narrow box.
func wrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	var out []string
	for len(runes) > width {
		out = append(out, string(runes[:width]))
		runes = runes[width:]
	}
	return append(out, string(runes))
}
