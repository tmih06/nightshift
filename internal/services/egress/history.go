package egress

import (
	"context"
	"sort"
	"time"
)

const (
	// historyDays is how far back daily counts are kept: 53 weeks, a full
	// contribution calendar.
	historyDays = 53 * 7
	historyDate = time.DateOnly
)

// DayUsage counts the requests and tokens of one UTC day.
type DayUsage struct {
	Date     string `json:"date"` // YYYY-MM-DD
	Requests int    `json:"requests"`
	Total    int64  `json:"total,omitempty"`
}

// AccountHistory is the daily usage of one secret for one account, oldest day
// first. Days without a request are left out.
type AccountHistory struct {
	Binding string     `json:"binding"`
	Label   string     `json:"label"`
	Days    []DayUsage `json:"days"`
}

// addDay counts one request on now's day. The caller holds u.mu.
func (u *usageStore) addDay(k usageKey, now time.Time, tokens int64) {
	days := u.days[k]
	if days == nil {
		days = map[string]DayUsage{}
		u.days[k] = days
	}
	date := now.UTC().Format(historyDate)
	d := days[date]
	d.Date = date
	d.Requests++
	d.Total += max(0, tokens)
	days[date] = d
}

// history returns every account's days inside the retention window, and drops
// the ones that fell out of it.
func (u *usageStore) history(ctx context.Context) []AccountHistory {
	if u.db != nil {
		return u.historyDB(ctx)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.historyLocked()
}

func (u *usageStore) historyLocked() []AccountHistory {
	cutoff := u.now().UTC().AddDate(0, 0, -historyDays).Format(historyDate)
	out := make([]AccountHistory, 0, len(u.days))
	for k, days := range u.days {
		h := AccountHistory{Binding: k.secret, Label: k.label, Days: make([]DayUsage, 0, len(days))}
		for date, d := range days {
			if date <= cutoff {
				delete(days, date)
				continue
			}
			h.Days = append(h.Days, d)
		}
		if len(h.Days) == 0 {
			delete(u.days, k)
			continue
		}
		out = appendSortedAccountHistory(out, h)
	}
	sortAccountHistories(out)
	return out
}

func appendSortedAccountHistory(out []AccountHistory, h AccountHistory) []AccountHistory {
	sort.Slice(h.Days, func(i, j int) bool { return h.Days[i].Date < h.Days[j].Date })
	return append(out, h)
}

func sortAccountHistories(out []AccountHistory) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Binding != out[j].Binding {
			return out[i].Binding < out[j].Binding
		}
		return out[i].Label < out[j].Label
	})
}

// History reports the daily requests and tokens of every account.
func (p *Proxy) History(ctx context.Context) []AccountHistory { return p.usage.history(ctx) }
