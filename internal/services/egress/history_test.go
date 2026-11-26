package egress

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryCountsByDayAndSurvivesARestart(t *testing.T) {
	now := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
	u := newUsageStore()
	u.now = func() time.Time { return now }
	u.recordDetailed("S", "a@example.com", 200, 0, TokenMetrics{Total: 100})
	u.recordDetailed("S", "a@example.com", 429, 0, TokenMetrics{})
	now = now.Add(2 * time.Minute) // the next day
	u.recordDetailed("S", "a@example.com", 200, 0, TokenMetrics{Total: 7})

	path := filepath.Join(t.TempDir(), "history.json")
	if err := u.save(path); err != nil {
		t.Fatal(err)
	}
	again := newUsageStore()
	again.now = u.now
	if err := again.load(path); err != nil {
		t.Fatal(err)
	}
	got := again.history()
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
	u.record("S", "a", 200)
	now = now.AddDate(0, 0, historyDays)
	if got := u.history(); len(got) != 0 {
		t.Errorf("history = %+v, want the old day gone", got)
	}
}

func TestHistorySaveSkipsUnchangedAndLoadRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	u := newUsageStore()
	if err := u.load(filepath.Join(dir, "missing.json")); err != nil {
		t.Errorf("a missing file is an empty history, got %v", err)
	}
	path := filepath.Join(dir, "history.json")
	if err := u.save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("nothing changed, so nothing must be written")
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := u.load(path); err == nil {
		t.Error("a broken file must be reported, so it is not overwritten")
	}
}
