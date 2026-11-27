package accountsview

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

const (
	quotaStatusUnknown = "unknown"
	quotaStatusAmount  = "amount"
)

// quotaGroup is the quota windows of one model family or provider-defined pool.
type quotaGroup struct {
	family   string
	explicit bool    // the provider named this pool itself
	order    int     // first appearance among the provider's windows
	known    bool    // at least one window reported its share
	peak     float64 // the most used known window's percentage
	windows  []v1.QuotaWindow
}

// groupQuota sorts windows into families: the account's own limits first, then
// the families nearest their limit. A provider-defined pool (as Antigravity's
// Gemini and Claude & GPT pools) keeps the order the provider gave; per-model
// windows go most used first, and the account's keep their given order.
func groupQuota(provider string, windows []v1.QuotaWindow) []quotaGroup {
	var groups []quotaGroup
	at := map[string]int{}
	for i := range windows {
		w := windows[i]
		f := w.Group
		explicit := f != ""
		if !explicit {
			f = quotaFamily(provider, w.Name, w.Label)
		}
		n, ok := at[f]
		if !ok {
			n = len(groups)
			at[f] = n
			groups = append(groups, quotaGroup{family: f, explicit: explicit, order: len(groups)})
		}
		if w.Status != quotaStatusUnknown {
			groups[n].known = true
			groups[n].peak = max(groups[n].peak, w.UsedPercent)
		}
		groups[n].windows = append(groups[n].windows, w)
	}
	rank := func(g quotaGroup) int {
		switch g.family {
		case familyAccount:
			return 0
		case familyOther:
			return 2
		}
		return 1
	}
	slices.SortStableFunc(groups, func(a, b quotaGroup) int {
		if a.explicit != b.explicit {
			if a.explicit {
				return -1
			}
			return 1
		}
		if a.explicit {
			return cmp.Compare(a.order, b.order)
		}
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(b.peak, a.peak), strings.Compare(a.family, b.family))
	})
	for _, g := range groups {
		if g.family != familyAccount && !g.explicit {
			slices.SortStableFunc(g.windows, func(a, b v1.QuotaWindow) int { return cmp.Compare(b.UsedPercent, a.UsedPercent) })
		}
	}
	return groups
}

// elapsed is how far through its window now is, from 0 to 1, or -1 when the
// provider did not say how long the window is or when it ends.
func elapsed(now time.Time, w v1.QuotaWindow) float64 {
	if w.WindowMinutes <= 0 || !w.ResetsAt.After(now) {
		return -1
	}
	length := time.Duration(w.WindowMinutes) * time.Minute
	return max(0, min(1, 1-float64(w.ResetsAt.Sub(now))/float64(length)))
}

// quotaMeta is the plan, the credits, and the age of a quota reading.
func quotaMeta(now time.Time, q *v1.AccountQuota) string {
	var meta []string
	if q.Plan != "" {
		meta = append(meta, q.Plan)
	}
	if q.CreditsBalance != "" {
		meta = append(meta, "credits "+q.CreditsBalance)
	}
	if !q.FetchedAt.IsZero() {
		src := cmp.Or(q.Source, "updated")
		meta = append(meta, src+" "+ago(now, q.FetchedAt))
	}
	return strings.Join(meta, " · ")
}

// quotaColumns are the widths of a quota row's label and meter in a pane.
type quotaColumns struct{ label, meter, pane int }

func columnsFor(width int, windows []v1.QuotaWindow) quotaColumns {
	label := 8
	for i := range windows {
		label = max(label, lipgloss.Width(windowLabel(windows[i])))
	}
	label = min(label, max(10, min(30, width*2/5)))
	// indent, label, gap, meter, " 100%", "  resets 23h59m"
	return quotaColumns{label: label, meter: max(6, min(44, width-label-22)), pane: width}
}

func windowLabel(w v1.QuotaWindow) string { return cmp.Or(w.Label, w.Name) }

func (m Model) quotaBlock(a v1.Account, width int) []string {
	s := m.st
	q := a.Quota
	if q == nil {
		return []string{m.section("Quota", "", width), s.faint.Render("No quota reading yet — press r to fetch live quotas.")}
	}
	now := m.now()
	out := []string{m.section("Quota", quotaMeta(now, q), width)}
	if q.Error != "" {
		out = append(out, s.warn.Render("fetch error: "+q.Error))
	}
	if q.NextResetAt.After(now) {
		out = append(out, s.faint.Render("next reset "+until(now, q.NextResetAt)+" ("+q.NextResetAt.UTC().Format("15:04Z")+")"))
	}
	groups := groupQuota(a.Provider, q.Windows)
	cols := columnsFor(width, q.Windows)
	paced := false
	for _, g := range groups {
		out = append(out, m.familyHeading(g))
		remaining := a.Provider == "antigravity"
		for i := range g.windows {
			paced = paced || !remaining && elapsed(now, g.windows[i]) >= 0
			out = append(out, m.quotaRow(now, g.windows[i], cols, remaining))
		}
	}
	if paced {
		out = append(out, s.faint.Render("┃ time gone in the window: a fill past it is ahead of pace"))
	}
	return out
}

func (m Model) familyHeading(g quotaGroup) string {
	name := lipgloss.NewStyle().Foreground(familyColor(g.family)).Bold(true).Render("▍" + g.family)
	if len(g.windows) < 2 || !g.known || g.explicit {
		return name
	}
	return name + m.st.faint.Render(fmt.Sprintf("  %d limits · peak %.0f%%", len(g.windows), g.peak))
}

// quotaRow is one window: its label, meter, share, and when it resets,
// then the counts and the pace where the pane has room. Antigravity shows
// the share remaining, as agy does; the other providers show the share used.
func (m Model) quotaRow(now time.Time, w v1.QuotaWindow, cols quotaColumns, remaining bool) string {
	s := m.st
	if row, special := m.specialQuotaRow(now, w, cols); special {
		return row
	}
	style := m.severity(w.UsedPercent)
	mark := elapsed(now, w)
	share := w.UsedPercent
	if remaining {
		share = 100 - w.UsedPercent
		mark = -1
	}
	ahead := mark >= 0 && w.UsedPercent >= 30 && w.UsedPercent > mark*100+15
	if ahead && w.UsedPercent < 70 {
		style = s.warn
	}
	line := " " + s.text.Render(pad(windowLabel(w), cols.label)) + " " +
		m.meter(share, cols.meter, style, mark) + style.Render(" "+percent(share))
	if w.ResetsAt.After(now) {
		line += s.faint.Render("  resets " + pad(span(w.ResetsAt.Sub(now)), 6))
	}
	var extra []string
	if w.Limit > 0 {
		extra = append(extra, s.faint.Render(compactCount(w.Used)+"/"+compactCount(w.Limit)))
	}
	if ahead {
		extra = append(extra, s.warn.Render("ahead of pace"))
	}
	for _, e := range extra {
		if lipgloss.Width(line)+1+lipgloss.Width(e) <= cols.pane {
			line += " " + e
		}
	}
	return line
}

func (m Model) specialQuotaRow(now time.Time, w v1.QuotaWindow, cols quotaColumns) (string, bool) {
	switch w.Status {
	case quotaStatusUnknown:
		return m.unknownQuotaRow(now, w, cols), true
	case quotaStatusAmount:
		return m.amountQuotaRow(now, w, cols), true
	default:
		return "", false
	}
}

func (m Model) amountQuotaRow(now time.Time, w v1.QuotaWindow, cols quotaColumns) string {
	s := m.st
	line := " " + s.text.Render(pad(windowLabel(w), cols.label)) + " " + s.faint.Render(fmt.Sprintf("%d remaining", w.Remaining))
	if w.ResetsAt.After(now) {
		line += s.faint.Render("  resets " + pad(span(w.ResetsAt.Sub(now)), 6))
	}
	return line
}

func (m Model) unknownQuotaRow(now time.Time, w v1.QuotaWindow, cols quotaColumns) string {
	s := m.st
	line := " " + s.text.Render(pad(windowLabel(w), cols.label)) + " " + s.faint.Render("limits not available")
	if w.ResetsAt.After(now) {
		line += s.faint.Render("  resets " + pad(span(w.ResetsAt.Sub(now)), 6))
	}
	return line
}

// percent is a share in four cells. A little use does not read as none.
func percent(pct float64) string {
	if pct > 0 && pct < 1 {
		return " <1%"
	}
	return fmt.Sprintf("%3.0f%%", pct)
}

// peakQuota is the most used window of an account's quota, for the list.
func peakQuota(q *v1.AccountQuota) (float64, bool) {
	if q == nil || len(q.Windows) == 0 {
		return 0, false
	}
	peak := 0.0
	known := false
	for i := range q.Windows {
		if q.Windows[i].Status == quotaStatusUnknown || q.Windows[i].Status == quotaStatusAmount {
			continue
		}
		known = true
		peak = max(peak, q.Windows[i].UsedPercent)
	}
	return peak, known
}
