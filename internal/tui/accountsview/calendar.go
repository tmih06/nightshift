package accountsview

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

const (
	calendarWeeks  = 53
	calendarGutter = 4 // the weekday names left of the grid
	calendarCell   = "■"
	dateLayout     = time.DateOnly
)

// calendar is the daily usage of the weeks ending with today's, as a
// contribution graph draws it: a column a week, Sunday on top.
type calendar struct {
	first, today time.Time // first is a Sunday
	weeks        int
	days         map[time.Time]v1.DayUsage
	steps        [3]int64 // the values that end levels 1, 2, and 3
	byTokens     bool
}

// sumDays adds up the daily usage of several accounts.
func sumDays(accounts []v1.Account) []v1.DayUsage {
	byDate := map[string]v1.DayUsage{}
	for i := range accounts {
		for _, d := range accounts[i].History {
			sum := byDate[d.Date]
			sum.Date = d.Date
			sum.Requests += d.Requests
			sum.Total += d.Total
			byDate[d.Date] = sum
		}
	}
	out := make([]v1.DayUsage, 0, len(byDate))
	for _, d := range byDate {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b v1.DayUsage) int { return strings.Compare(a.Date, b.Date) })
	return out
}

// newCalendar lays history out over as many weeks as fit in width. Days are
// UTC, as the proxy counts them.
func newCalendar(now time.Time, history []v1.DayUsage, width int) calendar {
	y, mo, d := now.UTC().Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	c := calendar{today: today, weeks: max(4, min(calendarWeeks, (width-calendarGutter+1)/2)), days: map[time.Time]v1.DayUsage{}}
	c.first = today.AddDate(0, 0, -int(today.Weekday())-7*(c.weeks-1))
	for _, u := range history {
		day, err := time.Parse(dateLayout, u.Date)
		if err != nil || day.Before(c.first) || day.After(today) {
			continue
		}
		c.days[day] = u
		c.byTokens = c.byTokens || u.Total > 0
	}
	// Levels split the busy days into quarters, so one huge day does not
	// leave every other one looking idle.
	var values []int64
	for _, u := range c.days {
		if v := c.value(u); v > 0 {
			values = append(values, v)
		}
	}
	slices.Sort(values)
	if n := len(values); n > 0 {
		c.steps = [3]int64{values[(n-1)/4], values[(n-1)/2], values[(n-1)*3/4]}
	}
	return c
}

// value is what a day is shaded by: tokens, or requests when no response in
// view reported any.
func (c calendar) value(u v1.DayUsage) int64 {
	if c.byTokens {
		return u.Total
	}
	return int64(u.Requests)
}

// level is a day's shade, 0 for idle through 4 for the busiest quarter. A day
// with requests is never drawn idle.
func (c calendar) level(day time.Time) int {
	u, ok := c.days[day]
	if !ok || u.Requests == 0 && u.Total == 0 {
		return 0
	}
	v := c.value(u)
	switch {
	case v > c.steps[2]:
		return 4
	case v > c.steps[1]:
		return 3
	case v > c.steps[0]:
		return 2
	case v > 0 && c.steps[0] == c.steps[2]:
		return 4 // every busy day is alike
	default:
		return 1
	}
}

// months names each month above the first week that starts in it, leaving out
// a name that would run into the one before.
func (c calendar) months() string {
	row := []byte(strings.Repeat(" ", c.weeks*2+2))
	free, last := 0, time.Month(0)
	for w := range c.weeks {
		start := c.first.AddDate(0, 0, 7*w)
		if start.Month() == last {
			continue
		}
		next := c.first.AddDate(0, 0, 7*(w+2))
		// A month that is nearly over would only crowd out the next name.
		if at := w * 2; at >= free && (w > 0 || next.Month() == start.Month()) {
			copy(row[at:], start.Format("Jan"))
			free = at + 4
		}
		last = start.Month()
	}
	return strings.TrimRight(string(row), " ")
}

// totals are the figures under the grid.
type totals struct {
	requests             int
	tokens               int64
	active, streak, best int
	busiest              time.Time
}

func (c calendar) totals() totals {
	var t totals
	run := 0
	var peak int64
	for day := c.first; !day.After(c.today); day = day.AddDate(0, 0, 1) {
		u, ok := c.days[day]
		if !ok || u.Requests == 0 {
			// Today is not over, so an idle one does not end the streak.
			if !day.Equal(c.today) {
				run = 0
			}
			continue
		}
		run++
		t.active++
		t.best = max(t.best, run)
		t.requests += u.Requests
		t.tokens += u.Total
		if v := c.value(u); v > peak {
			peak, t.busiest = v, day
		}
	}
	t.streak = run
	return t
}

func heatStyle(level int) lipgloss.Style { return lipgloss.NewStyle().Foreground(heatColor(level)) }

// grid is the seven weekday rows. Days still to come are left blank.
func (c calendar) grid(label lipgloss.Style) []string {
	var cells [5]string
	for i := range cells {
		cells[i] = heatStyle(i).Render(calendarCell)
	}
	names := [7]string{1: "Mon", 3: "Wed", 5: "Fri"}
	out := make([]string, 0, len(names))
	for wd, name := range names {
		var b strings.Builder
		b.WriteString(label.Render(pad(name, calendarGutter)))
		for w := range c.weeks {
			day := c.first.AddDate(0, 0, 7*w+wd)
			if day.After(c.today) {
				break
			}
			if w > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(cells[c.level(day)])
		}
		out = append(out, b.String())
	}
	return out
}

// legend is the calendar's shades, from idle to busiest.
func (m Model) legend() string {
	var b strings.Builder
	b.WriteString(m.st.faint.Render(strings.Repeat(" ", calendarGutter) + "Less "))
	for i := range 5 {
		b.WriteString(heatStyle(i).Render(calendarCell) + " ")
	}
	b.WriteString(m.st.faint.Render("More"))
	return b.String()
}

// activityBlock is the contribution calendar of the selected account, or of
// every account together.
func (m Model) activityBlock(a v1.Account, width int) []string {
	s := m.st
	history, scope := a.History, "this account · v for all"
	if m.allAccounts {
		history, scope = sumDays(m.snap.Accounts), "all accounts · v for this one"
	}
	c := newCalendar(m.now(), history, width)
	out := []string{m.section("Activity", scope, width), s.faint.Render(strings.Repeat(" ", calendarGutter) + c.months())}
	out = append(out, c.grid(s.faint)...)
	out = append(out, m.legend())
	t := c.totals()
	if t.active == 0 {
		return append(out, s.faint.Render("No requests recorded yet. Each day the proxy carries one is kept for a year."))
	}
	head := s.title.Render(compactCount(int64(t.requests))) + s.faint.Render(" requests")
	if c.byTokens {
		head = s.title.Render(compactCount(t.tokens)) + s.faint.Render(" tokens · ") + head
	}
	out = append(out, head+s.faint.Render(fmt.Sprintf(" in %d weeks", c.weeks)))
	busiest := c.days[t.busiest]
	peak := compactCount(c.value(busiest))
	if !c.byTokens {
		peak += " req"
	}
	return append(out, s.faint.Render(fmt.Sprintf("%d active days · streak %dd (best %dd) · busiest %s, %s",
		t.active, t.streak, t.best, t.busiest.Format("Jan 2"), peak)))
}
