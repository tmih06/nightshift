package accountsview

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// richSnapshot gives the first account many quota windows and a long history.
func richSnapshot() v1.Accounts {
	snap := sampleSnapshot()
	a := &snap.Accounts[0]
	reset := testNow().Add(time.Hour)
	a.Quota = &v1.AccountQuota{Windows: []v1.QuotaWindow{
		{Name: "5h", Label: "5-hour window", UsedPercent: 80, WindowMinutes: 300, ResetsAt: reset},
		{Name: "7d_sonnet", Label: "7-day Sonnet", UsedPercent: 12},
		{Name: "7d_opus", Label: "7-day Opus", UsedPercent: 0.4},
	}}
	a.History = []v1.DayUsage{{Date: "2025-12-31", Requests: 3, Total: 3000}, {Date: "2026-01-01", Requests: 1, Total: 500}}
	snap.Accounts[1].History = []v1.DayUsage{{Date: "2026-01-01", Requests: 4, Total: 700}}
	return snap
}

func TestQuotaWindowsAreGroupedByModelFamily(t *testing.T) {
	for _, c := range []struct {
		provider string
		labels   []string
		want     []string
	}{
		{"claude", []string{"5-hour window", "7-day Sonnet", "7-day Opus"}, []string{familyAccount, familyOpus, familySonnet}},
		{"antigravity", []string{"Gemini 3 Flash", "Tab Flash", "Claude Opus 4.6", "GPT-OSS 120B", "Gemini 3 Pro"}, []string{familyClaude, familyGPT, familyGemini, familyOther}},
		{"codex", []string{"5-hour window", "Requests / min"}, []string{familyAccount}},
	} {
		windows := make([]v1.QuotaWindow, len(c.labels))
		for i, l := range c.labels {
			windows[i] = v1.QuotaWindow{Label: l}
			if strings.Contains(l, "Opus") {
				windows[i].UsedPercent = 90
			}
		}
		got := make([]string, 0, len(c.want))
		for _, g := range groupQuota(c.provider, windows) {
			got = append(got, g.family)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: families = %v, want %v", c.provider, got, c.want)
		}
	}
}

func TestAntigravityQuotaWindowsKeepAgyPools(t *testing.T) {
	windows := []v1.QuotaWindow{
		{Name: "gemini-weekly", Label: "Weekly Limit Remaining", Group: "Gemini Models", UsedPercent: 25},
		{Name: "gemini-5h", Label: "Five Hour Limit Remaining", Group: "Gemini Models", UsedPercent: 75},
		{Name: "3p-weekly", Label: "Weekly Limit Remaining", Group: "Claude and GPT models", UsedPercent: 50},
		{Name: "3p-5h", Label: "Five Hour Limit Remaining", Group: "Claude and GPT models", UsedPercent: 10},
	}
	groups := groupQuota("antigravity", windows)
	if len(groups) != 2 || groups[0].family != "Gemini Models" || groups[1].family != "Claude and GPT models" {
		t.Fatalf("groups = %+v, want agy's two pools in order", groups)
	}
	for i, g := range groups {
		if len(g.windows) != 2 || g.windows[0].Name != windows[i*2].Name || g.windows[1].Name != windows[i*2+1].Name {
			t.Errorf("group %d windows = %+v, want the provider's weekly then 5h order", i, g.windows)
		}
	}
}

func TestAntigravityQuotaRowShowsRemainingShare(t *testing.T) {
	m := newTestModel(&fakeService{})
	row := m.quotaRow(testNow(), v1.QuotaWindow{
		Name:        "gemini-weekly",
		Label:       "Weekly Limit Remaining",
		Group:       "Gemini Models",
		UsedPercent: 25,
	}, quotaColumns{label: 24, meter: 20, pane: 80}, true)
	plainRow := ansi.Strip(row)
	if !strings.Contains(plainRow, "Weekly Limit Remaining") || !strings.Contains(plainRow, "75%") || strings.Contains(plainRow, "25%") {
		t.Errorf("remaining quota row = %q", plainRow)
	}
}

func TestAmountQuotaRowShowsAmount(t *testing.T) {
	m := newTestModel(&fakeService{})
	row := m.quotaRow(testNow(), v1.QuotaWindow{
		Name: "credits", Label: "Credits", Status: "amount", Remaining: 1500,
	}, quotaColumns{label: 12, meter: 20, pane: 80}, true)
	if plainRow := ansi.Strip(row); !strings.Contains(plainRow, "1500 remaining") {
		t.Errorf("amount quota row = %q", plainRow)
	}
}

func TestUnknownQuotaDoesNotReadAsEmptyOrFull(t *testing.T) {
	m := newTestModel(&fakeService{})
	row := m.quotaRow(testNow(), v1.QuotaWindow{
		Name:          "gemini-5h",
		Label:         "Five Hour Limit Used",
		Status:        "unknown",
		ResetsAt:      testNow().Add(2 * time.Hour),
		WindowMinutes: 300,
	}, quotaColumns{label: 20, meter: 20, pane: 80}, false)
	plainRow := ansi.Strip(row)
	if !strings.Contains(plainRow, "limits not available") || strings.Contains(plainRow, "0%") || strings.Contains(plainRow, "100%") {
		t.Errorf("unknown quota row = %q", plainRow)
	}
}

func TestDetailShowsFamiliesPaceAndCalendar(t *testing.T) {
	m := shown(t, &fakeService{snap: richSnapshot()})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 50})
	view := plain(m)
	for _, want := range []string{
		"▍Account limits", "▍Sonnet", "▍Opus", "<1%", "ahead of pace",
		"Activity", "Mon", "Dec", "Less", "More",
		"3.5K tokens · 4 requests", "2 active days · streak 2d (best 2d) · busiest Dec 31, 3.0K",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	m = press(t, m, "v")
	if view = plain(m); !strings.Contains(view, "all accounts") || !strings.Contains(view, "4.2K tokens · 8 requests") {
		t.Errorf("v must add up every account's days:\n%s", view)
	}
}

func TestCalendarShadesTheBusiestDaysDarkest(t *testing.T) {
	day := func(d int, tokens int64) v1.DayUsage {
		return v1.DayUsage{Date: testNow().AddDate(0, 0, -d).Format(dateLayout), Requests: 1, Total: tokens}
	}
	c := newCalendar(testNow(), []v1.DayUsage{day(4, 10), day(3, 20), day(2, 30), day(1, 1_000_000), day(0, 0)}, 200)
	if c.weeks != calendarWeeks || c.first.Weekday() != time.Sunday {
		t.Errorf("calendar = %d weeks from a %v, want %d from a Sunday", c.weeks, c.first.Weekday(), calendarWeeks)
	}
	today := c.today
	for d, want := range []int{1, 4, 3, 2, 1} {
		if got := c.level(today.AddDate(0, 0, -d)); got != want {
			t.Errorf("level %d days ago = %d, want %d", d, got, want)
		}
	}
	if got := c.level(today.AddDate(0, 0, -10)); got != 0 {
		t.Errorf("an idle day has level %d", got)
	}
}

func TestCalendarFitsItsWidth(t *testing.T) {
	m := shown(t, &fakeService{snap: richSnapshot()})
	a, _ := m.selected()
	for _, width := range []int{40, 47, 63, 109, 200} {
		c := newCalendar(testNow(), a.History, width)
		for _, l := range append(c.grid(m.st.faint), m.st.faint.Render(c.months())) {
			if w := lipgloss.Width(l); w > max(width, calendarGutter+c.weeks*2) {
				t.Errorf("width %d: %d cells: %q", width, w, ansi.Strip(l))
			}
		}
	}
}

func TestDetailScrollsAndEveryLineFits(t *testing.T) {
	for _, size := range [][2]int{{120, 24}, {70, 30}} {
		m := shown(t, &fakeService{snap: richSnapshot()})
		m, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if strings.Contains(plain(m), "primary account") {
			t.Fatalf("%v: the end of the details must start out of view", size)
		}
		for range 20 {
			m = press(t, m, "j", "k") // leaving an account returns to its top
			m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: size[0] - 2, Y: size[1] - 3})
		}
		for range 40 {
			m, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: size[0] - 2, Y: size[1] - 3})
		}
		if m.cursor != 0 || !strings.Contains(plain(m), "primary account") {
			t.Errorf("%v: the wheel over the details must scroll them to the end:\n%s", size, plain(m))
		}
		for i, l := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%v: line %d is %d cells wide", size, i, w)
			}
		}
		m = press(t, m, "j")
		if m.scroll != 0 {
			t.Errorf("%v: scroll = %d on another account, want 0", size, m.scroll)
		}
	}
}

// A style wrapped around a whole row ends at the first inner reset, so every
// part of the selected row has to carry the highlight itself.
func TestSelectedRowIsHighlightedToTheEnd(t *testing.T) {
	m := shown(t, &fakeService{snap: richSnapshot()})
	for _, width := range []int{40, 54, 80} {
		a, _ := m.selected()
		row := m.accountRow(a, true, width)
		for _, part := range strings.SplitAfter(row, "\x1b[m") {
			if ansi.Strip(part) != "" && !strings.Contains(part, "48;") {
				t.Errorf("width %d: %q is not highlighted", width, part)
			}
		}
	}
}

func TestMeterCells(t *testing.T) {
	for _, c := range []struct {
		pct        float64
		full, half int
	}{{0, 0, 0}, {0.1, 0, 1}, {45, 4, 1}, {100, 10, 0}, {250, 10, 0}} {
		if full, half := meterCells(c.pct, 10); full != c.full || half != c.half {
			t.Errorf("meterCells(%v) = %d, %d, want %d, %d", c.pct, full, half, c.full, c.half)
		}
	}
}
