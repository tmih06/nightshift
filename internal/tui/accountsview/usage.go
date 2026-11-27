package accountsview

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

func (m Model) usageBlock(a v1.Account, width int) []string {
	s := m.st
	if m.snap.UsageError != "" {
		return []string{m.section("Usage", "", width), s.warn.Render("unavailable: " + m.snap.UsageError)}
	}
	u := a.Usage
	if u == nil {
		note := "No request has been recorded for this account's token."
		if !a.Active && a.Priority > 1 {
			note = fmt.Sprintf("Not active yet (fallback #%d). Press u to use it, or K/J to reorder.", a.Priority)
		} else if !a.Active {
			note = "Not active, so no requests. Press u to use it."
		}
		return []string{m.section("Usage", "", width), s.faint.Render(note)}
	}
	now := m.now()
	out := []string{m.section("Usage", "all recorded usage", width)}
	reqLine := fmt.Sprintf("%s requests   %s ok", s.title.Render(strconv.Itoa(u.Requests)), s.ok.Render(strconv.Itoa(u.OK)))
	if u.AvgLatencyMS > 0 {
		reqLine += s.faint.Render(fmt.Sprintf("   avg %dms", u.AvgLatencyMS))
	}
	out = append(out, reqLine)
	if bad := m.errorSummary(u); bad != "" {
		out = append(out, share(
			[]int64{int64(u.OK), int64(u.RateLimited), int64(u.ClientError), int64(u.ServerError)},
			[]lipgloss.Style{s.ok, s.warn, s.warn.Faint(true), s.bad}, min(48, width)), bad)
	}
	out = append(out, m.tokenLines(u, width)...)
	out = append(out, s.faint.Render("last request "+ago(now, u.LastAt)))
	spw := max(12, min(72, width))
	out = append(out, "", s.faint.Render("requests (last 24h)"), s.ok.Render(spark(u.Recent, spw)))
	if len(u.RecentVolume) > 0 {
		out = append(out, s.faint.Render("tokens (last 24h)"), s.ok.Render(spark64(u.RecentVolume, spw)))
	}
	return append(out, m.modelLines(a.Provider, u, width)...)
}

func (m Model) errorSummary(u *v1.AccountUsage) string {
	s := m.st
	var bad []string
	if u.RateLimited > 0 {
		bad = append(bad, s.warn.Render(fmt.Sprintf("%d rate limited", u.RateLimited)))
	}
	if u.ClientError > 0 {
		bad = append(bad, s.warn.Render(fmt.Sprintf("%d client errors", u.ClientError)))
	}
	if u.ServerError > 0 {
		bad = append(bad, s.bad.Render(fmt.Sprintf("%d server errors", u.ServerError)))
	}
	return strings.Join(bad, s.faint.Render("  ·  "))
}

// tokenLines are the token counts, with a bar of how they split into cache
// reads, cache writes, fresh input, and output.
func (m Model) tokenLines(u *v1.AccountUsage, width int) []string {
	if u.Total == 0 && u.Input == 0 && u.Output == 0 {
		return nil
	}
	s := m.st
	out := []string{fmt.Sprintf("%s %s   %s %s   %s %s",
		s.faint.Render("In"), s.text.Render(compactCount(u.Input)),
		s.faint.Render("Out"), s.text.Render(compactCount(u.Output)),
		s.faint.Render("Total"), s.title.Render(compactCount(u.Total)))}
	fg := func(l, d string) lipgloss.Style { return lipgloss.NewStyle().Foreground(adaptive(l, d)) }
	kinds := []lipgloss.Style{fg("#0b7a5f", "#2fc7a1"), fg("#8a6d1d", "#d9b45b"), fg("#1a63d1", "#6ea8fe"), fg("#9c3d78", "#d787b7")}
	if bar := share([]int64{u.CacheRead, u.CacheWrite, u.Input, u.Output}, kinds, min(48, width)); bar != "" {
		var legend []string
		for i, name := range []string{"cache read", "cache write", "input", "output"} {
			legend = append(legend, kinds[i].Render("■")+s.faint.Render(" "+name))
		}
		out = append(out, bar, strings.Join(legend, "  "))
	}
	var extra []string
	if u.CacheRead > 0 || u.CacheWrite > 0 {
		cache := fmt.Sprintf("Cache %s read · %s write", compactCount(u.CacheRead), compactCount(u.CacheWrite))
		if u.CacheHitPercent > 0 {
			cache += fmt.Sprintf(" (%.1f%% hit)", u.CacheHitPercent)
		}
		extra = append(extra, cache)
	}
	if u.Reasoning > 0 {
		extra = append(extra, "Reasoning "+compactCount(u.Reasoning))
	}
	if len(extra) > 0 {
		out = append(out, s.faint.Render(strings.Join(extra, "  ·  ")))
	}
	return out
}

// modelRow is one model's usage.
type modelRow struct {
	name string
	u    v1.ModelUsage
}

// modelGroup is the usage of one model family.
type modelGroup struct {
	family   string
	requests int
	total    int64
	models   []modelRow
}

// groupModels sorts the models into families, busiest first.
func groupModels(provider string, byModel map[string]v1.ModelUsage) []modelGroup {
	at := map[string]*modelGroup{}
	for name, u := range byModel {
		f := cmp.Or(modelFamily(provider, name), familyOther)
		g := at[f]
		if g == nil {
			g = &modelGroup{family: f}
			at[f] = g
		}
		g.requests += u.Requests
		g.total += u.Total
		g.models = append(g.models, modelRow{name: name, u: u})
	}
	out := make([]modelGroup, 0, len(at))
	for _, g := range at {
		slices.SortFunc(g.models, func(a, b modelRow) int {
			return cmp.Or(cmp.Compare(b.u.Total, a.u.Total), strings.Compare(a.name, b.name))
		})
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b modelGroup) int {
		return cmp.Or(cmp.Compare(b.total, a.total), cmp.Compare(b.requests, a.requests), strings.Compare(a.family, b.family))
	})
	return out
}

// modelLines are each family's share of the account's tokens, then its models.
func (m Model) modelLines(provider string, u *v1.AccountUsage, width int) []string {
	groups := groupModels(provider, u.ByModel)
	if len(groups) == 0 {
		return nil
	}
	s := m.st
	var all int64
	for _, g := range groups {
		all += g.total
	}
	out := []string{"", s.faint.Render("models by family")}
	bar := max(6, min(30, width-34))
	for _, g := range groups {
		style := lipgloss.NewStyle().Foreground(familyColor(g.family))
		pct := 0.0
		if all > 0 {
			pct = float64(g.total) * 100 / float64(all)
		}
		out = append(out, style.Bold(true).Render("▍"+pad(g.family, 8))+" "+m.meter(pct, bar, style, -1)+
			style.Render(fmt.Sprintf(" %3.0f%%", pct))+s.faint.Render(fmt.Sprintf("  %s tok", compactCount(g.total))))
		for _, r := range g.models {
			out = append(out, "  "+s.text.Render(pad(r.name, min(26, max(12, width-30))))+"  "+
				s.faint.Render(fmt.Sprintf("%d req · %s in · %s out · %s cache",
					r.u.Requests, compactCount(r.u.Input), compactCount(r.u.Output), compactCount(r.u.CacheRead))))
		}
	}
	return out
}
