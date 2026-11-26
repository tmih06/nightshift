package egress

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	u.unsaved = true
}

// history returns every account's days inside the retention window, and drops
// the ones that fell out of it.
func (u *usageStore) history() []AccountHistory {
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
		sort.Slice(h.Days, func(i, j int) bool { return h.Days[i].Date < h.Days[j].Date })
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Binding != out[j].Binding {
			return out[i].Binding < out[j].Binding
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// load reads the daily counts saved at path. A missing file is an empty history.
func (u *usageStore) load(path string) error {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read usage history: %w", err)
	}
	var saved []AccountHistory
	if err := json.Unmarshal(raw, &saved); err != nil {
		return fmt.Errorf("parse usage history %s: %w", path, err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, h := range saved {
		days := make(map[string]DayUsage, len(h.Days))
		for _, d := range h.Days {
			days[d.Date] = d
		}
		u.days[usageKey{h.Binding, h.Label}] = days
	}
	return nil
}

// save writes the daily counts to path if they changed since the last save. It
// replaces the file in one step, so a crash leaves the previous one whole.
func (u *usageStore) save(path string) error {
	u.mu.Lock()
	if !u.unsaved {
		u.mu.Unlock()
		return nil
	}
	snap := u.historyLocked()
	u.unsaved = false
	u.mu.Unlock()
	if err := replaceJSON(path, snap); err != nil {
		u.mu.Lock()
		u.unsaved = true // try again at the next save
		u.mu.Unlock()
		return fmt.Errorf("save usage history: %w", err)
	}
	return nil
}

func replaceJSON(path string, v any) (err error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// History reports the daily requests and tokens of every account.
func (p *Proxy) History() []AccountHistory { return p.usage.history() }

// LoadHistory restores the daily counts saved at path. Call it before serving.
func (p *Proxy) LoadHistory(path string) error { return p.usage.load(path) }

// SaveHistory writes the daily counts to path if they changed.
func (p *Proxy) SaveHistory(path string) error { return p.usage.save(path) }
