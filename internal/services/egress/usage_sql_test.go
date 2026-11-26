package egress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTestSQLiteUsageStore(t *testing.T, dbPath, legacyPath string) *usageStore {
	t.Helper()
	u, err := openSQLiteUsageStore(dbPath, legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func closeTestUsageStore(t *testing.T, u *usageStore) {
	t.Helper()
	if err := u.close(); err != nil {
		t.Errorf("close usage store: %v", err)
	}
}

func closeTestProxy(t *testing.T, p *Proxy) {
	t.Helper()
	if err := p.Close(); err != nil {
		t.Errorf("close proxy: %v", err)
	}
}

func TestSQLiteUsagePersistsEveryRequestAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	u := openTestSQLiteUsageStore(t, dbPath, "")
	u.now = func() time.Time { return now }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, latencyMS: 120, tokens: TokenMetrics{Model: "model-a", Input: 10, Output: 5, Total: 15}})
	now = now.Add(10 * time.Minute)
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 429, latencyMS: 80, tokens: TokenMetrics{Model: "model-a", Input: 2, Output: 1, Total: 3}})
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 503, latencyMS: 0, tokens: TokenMetrics{}})
	if err := u.close(); err != nil {
		t.Fatal(err)
	}

	again := openTestSQLiteUsageStore(t, dbPath, "")
	again.now = func() time.Time { return now }
	defer closeTestUsageStore(t, again)
	var eventCount int
	if err := again.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM usage_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 3 {
		t.Errorf("usage_events holds %d rows, want every one of the 3 requests", eventCount)
	}

	got := again.snapshot(t.Context())
	if len(got) != 1 {
		t.Fatalf("usage = %+v, want one account", got)
	}
	a := got[0]
	if a.Requests != 3 || a.OK != 1 || a.RateLimited != 1 || a.ServerError != 1 {
		t.Errorf("status counts = %+v, want three requests split 1 ok, 1 rate limited, 1 server error", a)
	}
	if a.InputTokens != 12 || a.OutputTokens != 6 || a.TotalTokens != 18 {
		t.Errorf("tokens = input %d output %d total %d, want 12/6/18", a.InputTokens, a.OutputTokens, a.TotalTokens)
	}
	if !a.LastRequest.Equal(now.UTC()) {
		t.Errorf("last request = %s, want %s", a.LastRequest, now.UTC())
	}
	m := a.ByModel["model-a"]
	if m.Requests != 2 || m.Input != 12 || m.Output != 6 || m.Total != 18 {
		t.Errorf("model usage = %+v, want two model-a requests with all their tokens", m)
	}
	recent := 0
	for _, n := range a.Recent {
		recent += n
	}
	if recent != 3 {
		t.Errorf("recent windows hold %d requests, want all three", recent)
	}
	if a.Recent[usageBuckets-1] != 2 || a.Recent[usageBuckets-2] != 1 {
		t.Errorf("recent window tail = %v, want one request in the prior window and two in the last", a.Recent[usageBuckets-3:])
	}
	if a.RecentTokens[usageBuckets-1] != 3 || a.RecentTokens[usageBuckets-2] != 15 {
		t.Errorf("recent token tail = %v, want 15 tokens in the prior window and 3 in the last", a.RecentTokens[usageBuckets-3:])
	}

	history := again.history(t.Context())
	if len(history) != 1 || len(history[0].Days) != 1 {
		t.Fatalf("history = %+v, want one day", history)
	}
	day := history[0].Days[0]
	if day.Date != "2026-03-01" || day.Requests != 3 || day.Total != 18 {
		t.Errorf("day = %+v, want 2026-03-01 with 3 requests and 18 tokens", day)
	}
}

func TestSQLiteUsageKeepsEventsOutsideTheHistoryCalendar(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -historyDays-5)

	u := openTestSQLiteUsageStore(t, dbPath, "")
	u.now = func() time.Time { return old }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, latencyMS: 10, tokens: TokenMetrics{Total: 20}})
	if err := u.close(); err != nil {
		t.Fatal(err)
	}

	again := openTestSQLiteUsageStore(t, dbPath, "")
	again.now = func() time.Time { return now }
	defer closeTestUsageStore(t, again)
	got := again.snapshot(t.Context())
	if len(got) != 1 || got[0].Requests != 1 || got[0].TotalTokens != 20 {
		t.Errorf("usage = %+v, want the old event kept in all-time usage", got)
	}
	if history := again.history(t.Context()); len(history) != 0 {
		t.Errorf("history = %+v, want only the 53-week calendar window", history)
	}
}

func TestSQLiteUsageImportsLegacyJSONOnce(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "usage.db")
	legacyPath := filepath.Join(dir, "usage-history.json")
	legacy := []AccountHistory{{
		Binding: "S",
		Label:   "a@example.com",
		Days:    []DayUsage{{Date: "2026-03-01", Requests: 2, Total: 30}},
	}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)

	u := openTestSQLiteUsageStore(t, dbPath, legacyPath)
	u.now = func() time.Time { return now }
	if err := u.close(); err != nil {
		t.Fatal(err)
	}
	again := openTestSQLiteUsageStore(t, dbPath, legacyPath)
	again.now = func() time.Time { return now }
	defer closeTestUsageStore(t, again)

	history := again.history(t.Context())
	if len(history) != 1 || len(history[0].Days) != 1 {
		t.Fatalf("history = %+v, want one imported day", history)
	}
	day := history[0].Days[0]
	if day.Requests != 2 || day.Total != 30 {
		t.Errorf("imported day = %+v, want exactly two requests and thirty tokens", day)
	}
}

func TestSQLiteUsageHandlesConcurrentRecords(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	u := openTestSQLiteUsageStore(t, dbPath, "")
	u.now = func() time.Time { return now }
	defer closeTestUsageStore(t, u)

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, latencyMS: 1, tokens: TokenMetrics{Total: 1}})
			}
		}()
	}
	wg.Wait()

	got := u.snapshot(t.Context())
	if len(got) != 1 || got[0].Requests != 100 || got[0].TotalTokens != 100 {
		t.Errorf("usage = %+v, want all 100 concurrent requests and tokens", got)
	}
}

func TestProxyUsageReloadsSQLiteStore(t *testing.T) {
	dir := t.TempDir()
	token, label := filepath.Join(dir, "REAL_TOKEN"), filepath.Join(dir, "LABEL_TOKEN")
	for path, value := range map[string]string{token: "real-1", label: "a@example.com\n"} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := Config{
		Rules:   []Rule{{Name: "api", Hosts: []string{"api.example.test"}, Ports: []int{443}, Mode: ModeTunnel}},
		Secrets: map[string]SecretBinding{"TOKEN": {Placeholder: "nightshift:secret:TOKEN", File: token, LabelFile: label}},
	}
	dbPath := filepath.Join(dir, "usage.db")

	p, err := NewProxy(cfg, Options{SkipAutoCA: true, UsageDBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	p.record(t.Context(), AuditEvent{Action: "ALLOW", Status: 200, SecretsInjected: []string{"TOKEN"}})
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := NewProxy(cfg, Options{SkipAutoCA: true, UsageDBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestProxy(t, again)
	got := again.Usage(t.Context())
	if len(got) != 1 || got[0].Label != "a@example.com" || got[0].Requests != 1 {
		t.Errorf("usage after reopening the proxy = %+v, want the recorded request", got)
	}
}

func TestSQLiteUsagePrunesRawEventsAfterTheRecentWindow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	old := now.Add(-25 * time.Hour)

	u := openTestSQLiteUsageStore(t, dbPath, "")
	u.now = func() time.Time { return old }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, tokens: TokenMetrics{Total: 20}})
	u.now = func() time.Time { return now }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, tokens: TokenMetrics{Total: 30}})

	var rawEvents int
	if err := u.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM usage_events`).Scan(&rawEvents); err != nil {
		t.Fatal(err)
	}
	if rawEvents != 1 {
		t.Errorf("usage_events holds %d rows, want only the event inside the recent window", rawEvents)
	}
	got := u.snapshot(t.Context())
	if len(got) != 1 || got[0].Requests != 2 || got[0].TotalTokens != 50 {
		t.Errorf("usage = %+v, want both events kept in all-time aggregates", got)
	}
	history := u.history(t.Context())
	if len(history) != 1 || len(history[0].Days) != 2 {
		t.Errorf("history = %+v, want both days kept in daily aggregates", history)
	}
	closeTestUsageStore(t, u)
}

func TestUsageEndpointReportsADegradedDatabase(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "REAL_TOKEN")
	if err := os.WriteFile(token, []byte("real-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Rules:   []Rule{{Name: "api", Hosts: []string{"api.example.test"}, Ports: []int{443}, Mode: ModeTunnel}},
		Secrets: map[string]SecretBinding{"TOKEN": {Placeholder: "nightshift:secret:TOKEN", File: token}},
	}
	p, err := NewProxy(cfg, Options{SkipAutoCA: true, UsageDBPath: filepath.Join(dir, "usage.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p.record(t.Context(), AuditEvent{Action: "ALLOW", Status: 200, SecretsInjected: []string{"TOKEN"}})
	if p.UsageError() == nil {
		t.Fatal("UsageError is nil after a write to a closed database")
	}
	rec := httptest.NewRecorder()
	p.AdminHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/usage", http.NoBody))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/usage status = %d, want 503 when the database is degraded", rec.Code)
	}
}

func TestSQLiteUsageBackfillsWindowsFromRawEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	now := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	u := openTestSQLiteUsageStore(t, dbPath, "")
	u.now = func() time.Time { return now }
	u.recordDetailed(t.Context(), "S", "a@example.com", usageObservation{status: 200, tokens: TokenMetrics{Total: 20}})
	if _, err := u.db.ExecContext(t.Context(), `DELETE FROM usage_windows`); err != nil {
		t.Fatal(err)
	}
	closeTestUsageStore(t, u)

	again := openTestSQLiteUsageStore(t, dbPath, "")
	again.now = func() time.Time { return now }
	got := again.snapshot(t.Context())
	if len(got) != 1 || got[0].Recent[usageBuckets-1] != 1 || got[0].RecentTokens[usageBuckets-1] != 20 {
		t.Errorf("usage after reopening = %+v, want the raw event backfilled into recent windows", got)
	}
	closeTestUsageStore(t, again)
}
