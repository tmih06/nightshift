package egress

import (
	"testing"
	"time"
)

func TestHistoryCountsByDay(t *testing.T) {
	now := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
	u := newUsageStore()
	u.now = func() time.Time { return now }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, latencyMS: 0, tokens: TokenMetrics{Total: 100}})
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 429, latencyMS: 0, tokens: TokenMetrics{}})
	now = now.Add(2 * time.Minute) // the next day
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, latencyMS: 0, tokens: TokenMetrics{Total: 7}})

	got := u.history(t.Context())
	if len(got) != 1 || got[0].Label != "a@example.com" {
		t.Fatalf("history = %+v, want one account", got)
	}
	want := []DayUsage{{Date: "2026-03-01", Requests: 2, Total: 100}, {Date: "2026-03-02", Requests: 1, Total: 7}}
	if len(got[0].Days) != len(want) {
		t.Fatalf("days = %+v, want %+v", got[0].Days, want)
	}
	for i := range want {
		if got[0].Days[i] != want[i] {
			t.Errorf("day %d = %+v, want %+v", i, got[0].Days[i], want[i])
		}
	}
}

func TestHistoryDropsDaysOlderThanTheCalendar(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := newUsageStore()
	u.now = func() time.Time { return now }
	u.record(t.Context(), "S", "a", 200)
	now = now.AddDate(0, 0, historyDays)
	if got := u.history(t.Context()); len(got) != 0 {
		t.Errorf("history = %+v, want the old day gone", got)
	}
}
