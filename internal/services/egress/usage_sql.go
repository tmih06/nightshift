package egress

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

func usageDBStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS usage_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		occurred_at INTEGER NOT NULL,
		binding TEXT NOT NULL,
		label TEXT NOT NULL,
		status INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL,
		model TEXT NOT NULL,
		input_tokens INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		cache_read_tokens INTEGER NOT NULL,
		cache_write_tokens INTEGER NOT NULL,
		reasoning_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL
	)`,
		`CREATE INDEX IF NOT EXISTS usage_events_time
		ON usage_events (occurred_at)`,
		`CREATE TABLE IF NOT EXISTS usage_account_totals (
		binding TEXT NOT NULL,
		label TEXT NOT NULL,
		requests INTEGER NOT NULL,
		ok_count INTEGER NOT NULL,
		client_error_count INTEGER NOT NULL,
		rate_limited_count INTEGER NOT NULL,
		server_error_count INTEGER NOT NULL,
		input_tokens INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		cache_read_tokens INTEGER NOT NULL,
		cache_write_tokens INTEGER NOT NULL,
		reasoning_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		latency_ms_total INTEGER NOT NULL,
		last_request_ms INTEGER NOT NULL,
		PRIMARY KEY (binding, label)
	)`,
		`CREATE TABLE IF NOT EXISTS usage_model_totals (
		binding TEXT NOT NULL,
		label TEXT NOT NULL,
		model TEXT NOT NULL,
		requests INTEGER NOT NULL,
		input_tokens INTEGER NOT NULL,
		output_tokens INTEGER NOT NULL,
		cache_read_tokens INTEGER NOT NULL,
		cache_write_tokens INTEGER NOT NULL,
		reasoning_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		PRIMARY KEY (binding, label, model)
	)`,
	}
}

func usageDBRollupStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS usage_windows (
		binding TEXT NOT NULL,
		label TEXT NOT NULL,
		window INTEGER NOT NULL,
		requests INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		PRIMARY KEY (binding, label, window)
	)`,
		`INSERT OR IGNORE INTO usage_windows (binding, label, window, requests, total_tokens)
		SELECT binding, label, occurred_at / 600000, COUNT(*), COALESCE(SUM(total_tokens), 0)
		FROM usage_events GROUP BY binding, label, occurred_at / 600000`,
		`CREATE TABLE IF NOT EXISTS usage_days (
		binding TEXT NOT NULL,
		label TEXT NOT NULL,
		date TEXT NOT NULL,
		requests INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		PRIMARY KEY (binding, label, date)
	)`,
	}
}

func usageDBMetaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS usage_meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
		`INSERT OR IGNORE INTO usage_meta (key, value) VALUES ('schema_version', '1')`,
	}
}

const usageDBTimeout = 5 * time.Second

func usageDBContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, usageDBTimeout)
}

func closeUsageDB(db *sql.DB) {
	if err := db.Close(); err != nil {
		log.Printf("egress usage database: close: %v", err)
	}
}

func closeUsageRows(rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		log.Printf("egress usage database: close rows: %v", err)
	}
}

func rollbackUsageTx(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Printf("egress usage database: rollback: %v", err)
	}
}

// openSQLiteUsageStore opens the durable usage store at path. The database is
// the owner of usage state; the in-memory maps remain only as a test fallback.
func openSQLiteUsageStore(path, legacyHistoryPath string) (*usageStore, error) {
	if path == "" {
		return nil, errors.New("usage database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create usage database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_journal_mode=wal&_synchronous=FULL")
	if err != nil {
		return nil, fmt.Errorf("open usage database %s: %w", path, err)
	}
	keep := false
	defer func() {
		if !keep {
			closeUsageDB(db)
		}
	}()
	db.SetMaxOpenConns(1)

	if err := configureUsageDB(db, path); err != nil {
		return nil, err
	}
	u := &usageStore{
		now:  time.Now,
		m:    map[usageKey]*usageSeries{},
		days: map[usageKey]map[string]DayUsage{},
		db:   db,
	}
	if err := u.importLegacyHistory(legacyHistoryPath); err != nil {
		return nil, err
	}
	keep = true
	return u, nil
}

func configureUsageDB(db *sql.DB, path string) error {
	ctx, cancel := usageDBContext(context.Background())
	defer cancel()
	if err := enableWAL(ctx, db); err != nil {
		return err
	}
	if err := execUsageStatements(ctx, db, usageDBStatements(), "create usage database schema"); err != nil {
		return err
	}
	if err := execUsageStatements(ctx, db, usageDBRollupStatements(), "create usage database rollups"); err != nil {
		return err
	}
	if err := execUsageStatements(ctx, db, usageDBMetaStatements(), "create usage database metadata"); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("protect usage database %s: %w", path, err)
	}
	return nil
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	var journal string
	if err := db.QueryRowContext(ctx, `PRAGMA journal_mode = WAL`).Scan(&journal); err != nil {
		return fmt.Errorf("enable usage database WAL: %w", err)
	}
	if journal != "wal" {
		return fmt.Errorf("enable usage database WAL: journal mode is %s", journal)
	}
	return nil
}

func execUsageStatements(ctx context.Context, db *sql.DB, statements []string, what string) error {
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	return nil
}

func (u *usageStore) close() error {
	if u.db == nil {
		return nil
	}
	if err := u.db.Close(); err != nil {
		return fmt.Errorf("close usage database: %w", err)
	}
	return nil
}

func statusDeltas(status int) (ok, clientError, rateLimited, serverError int64) {
	switch {
	case status == 429:
		rateLimited = 1
	case status >= 500:
		serverError = 1
	case status >= 400:
		clientError = 1
	default:
		ok = 1
	}
	return ok, clientError, rateLimited, serverError
}

type usageEvent struct {
	occurredAt  time.Time
	binding     string
	label       string
	status      int
	latencyMS   int64
	tokens      TokenMetrics
	ok          int64
	clientError int64
	rateLimited int64
	serverError int64
}

func newUsageEvent(now time.Time, secret, label string, obs usageObservation) usageEvent {
	tok := obs.tokens
	tok.Input = max(0, tok.Input)
	tok.Output = max(0, tok.Output)
	tok.CacheRead = max(0, tok.CacheRead)
	tok.CacheWrite = max(0, tok.CacheWrite)
	tok.Reasoning = max(0, tok.Reasoning)
	tok.Total = max(0, tok.Total)
	ok, clientError, rateLimited, serverError := statusDeltas(obs.status)
	return usageEvent{
		occurredAt: now.UTC(), binding: secret, label: label, status: obs.status,
		latencyMS: max(0, obs.latencyMS), tokens: tok, ok: ok,
		clientError: clientError, rateLimited: rateLimited, serverError: serverError,
	}
}

func (u *usageStore) insertEvent(parent context.Context, secret, label string, obs usageObservation) error {
	ctx, cancel := usageDBContext(context.WithoutCancel(parent))
	defer cancel()
	ev := newUsageEvent(u.now(), secret, label, obs)
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage event: %w", err)
	}
	defer rollbackUsageTx(tx)

	steps := []func(context.Context, *sql.Tx) error{
		ev.insertRaw,
		ev.upsertAccount,
		ev.upsertDay,
		ev.upsertWindow,
		ev.pruneEventWindows,
		ev.upsertModel,
	}
	for _, step := range steps {
		if err := step(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage event: %w", err)
	}
	return nil
}

func (ev usageEvent) insertRaw(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_events (
		occurred_at, binding, label, status, latency_ms, model,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		reasoning_tokens, total_tokens
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.occurredAt.UnixMilli(), ev.binding, ev.label, ev.status, ev.latencyMS, ev.tokens.Model,
		ev.tokens.Input, ev.tokens.Output, ev.tokens.CacheRead, ev.tokens.CacheWrite,
		ev.tokens.Reasoning, ev.tokens.Total)
	if err != nil {
		return fmt.Errorf("insert usage event: %w", err)
	}
	return nil
}

func (ev usageEvent) upsertAccount(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_account_totals (
		binding, label, requests, ok_count, client_error_count, rate_limited_count,
		server_error_count, input_tokens, output_tokens, cache_read_tokens,
		cache_write_tokens, reasoning_tokens, total_tokens, latency_ms_total,
		last_request_ms
	) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (binding, label) DO UPDATE SET
		requests = requests + 1,
		ok_count = ok_count + excluded.ok_count,
		client_error_count = client_error_count + excluded.client_error_count,
		rate_limited_count = rate_limited_count + excluded.rate_limited_count,
		server_error_count = server_error_count + excluded.server_error_count,
		input_tokens = input_tokens + excluded.input_tokens,
		output_tokens = output_tokens + excluded.output_tokens,
		cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
		cache_write_tokens = cache_write_tokens + excluded.cache_write_tokens,
		reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
		total_tokens = total_tokens + excluded.total_tokens,
		latency_ms_total = latency_ms_total + excluded.latency_ms_total,
		last_request_ms = max(last_request_ms, excluded.last_request_ms)`,
		ev.binding, ev.label, ev.ok, ev.clientError, ev.rateLimited, ev.serverError,
		ev.tokens.Input, ev.tokens.Output, ev.tokens.CacheRead,
		ev.tokens.CacheWrite, ev.tokens.Reasoning, ev.tokens.Total,
		ev.latencyMS, ev.occurredAt.UnixMilli())
	if err != nil {
		return fmt.Errorf("update account usage totals: %w", err)
	}
	return nil
}

func (ev usageEvent) upsertDay(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_days
		(binding, label, date, requests, total_tokens) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT (binding, label, date) DO UPDATE SET
			requests = requests + 1,
			total_tokens = total_tokens + excluded.total_tokens`,
		ev.binding, ev.label, ev.occurredAt.Format(historyDate), ev.tokens.Total)
	if err != nil {
		return fmt.Errorf("update daily usage totals: %w", err)
	}
	return nil
}

func (ev usageEvent) upsertWindow(ctx context.Context, tx *sql.Tx) error {
	window := ev.occurredAt.Unix() / int64(usageBucket.Seconds())
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_windows
		(binding, label, window, requests, total_tokens) VALUES (?, ?, ?, 1, ?)
	ON CONFLICT (binding, label, window) DO UPDATE SET
		requests = requests + 1,
		total_tokens = total_tokens + excluded.total_tokens`,
		ev.binding, ev.label, window, ev.tokens.Total)
	if err != nil {
		return fmt.Errorf("update usage windows: %w", err)
	}
	return nil
}

func (ev usageEvent) pruneEventWindows(ctx context.Context, tx *sql.Tx) error {
	firstWindow := ev.occurredAt.Unix()/int64(usageBucket.Seconds()) - usageBuckets + 1
	cutoff := firstWindow * int64(usageBucket/time.Millisecond)
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_events WHERE occurred_at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune raw usage events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_windows WHERE window < ?`, firstWindow); err != nil {
		return fmt.Errorf("prune usage windows: %w", err)
	}
	return nil
}

func (ev usageEvent) upsertModel(ctx context.Context, tx *sql.Tx) error {
	if ev.tokens.Model == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_model_totals (
		binding, label, model, requests, input_tokens, output_tokens,
		cache_read_tokens, cache_write_tokens, reasoning_tokens, total_tokens
	) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?)
	ON CONFLICT (binding, label, model) DO UPDATE SET
		requests = requests + 1,
		input_tokens = input_tokens + excluded.input_tokens,
		output_tokens = output_tokens + excluded.output_tokens,
		cache_read_tokens = cache_read_tokens + excluded.cache_read_tokens,
		cache_write_tokens = cache_write_tokens + excluded.cache_write_tokens,
		reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
		total_tokens = total_tokens + excluded.total_tokens`,
		ev.binding, ev.label, ev.tokens.Model, ev.tokens.Input, ev.tokens.Output,
		ev.tokens.CacheRead, ev.tokens.CacheWrite, ev.tokens.Reasoning, ev.tokens.Total)
	if err != nil {
		return fmt.Errorf("update model usage totals: %w", err)
	}
	return nil
}

func (u *usageStore) readDBAccountTotals(ctx context.Context) (map[usageKey]*usageSeries, error) {
	ctx, cancel := usageDBContext(ctx)
	defer cancel()
	rows, err := u.db.QueryContext(ctx, `SELECT binding, label, requests, ok_count,
		client_error_count, rate_limited_count, server_error_count,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		reasoning_tokens, total_tokens, latency_ms_total, last_request_ms
		FROM usage_account_totals`)
	if err != nil {
		return nil, fmt.Errorf("read account usage totals: %w", err)
	}
	defer closeUsageRows(rows)

	series := map[usageKey]*usageSeries{}
	for rows.Next() {
		var a AccountUsage
		var latencyTotal, lastRequestMS int64
		if err := rows.Scan(&a.Binding, &a.Label, &a.Requests, &a.OK,
			&a.ClientError, &a.RateLimited, &a.ServerError, &a.InputTokens,
			&a.OutputTokens, &a.CacheRead, &a.CacheWrite, &a.Reasoning,
			&a.TotalTokens, &latencyTotal, &lastRequestMS); err != nil {
			return nil, fmt.Errorf("scan account usage totals: %w", err)
		}
		a.LastRequest = time.UnixMilli(lastRequestMS).UTC()
		series[usageKey{a.Binding, a.Label}] = &usageSeries{
			AccountUsage:   a,
			totalLatencyMS: latencyTotal,
			windows:        map[int64]int{},
			tokenWindows:   map[int64]int64{},
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read account usage totals: %w", err)
	}
	return series, nil
}

func (u *usageStore) addDBModelTotals(ctx context.Context, series map[usageKey]*usageSeries) error {
	ctx, cancel := usageDBContext(ctx)
	defer cancel()
	rows, err := u.db.QueryContext(ctx, `SELECT binding, label, model, requests,
		input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
		reasoning_tokens, total_tokens FROM usage_model_totals`)
	if err != nil {
		return fmt.Errorf("read model usage totals: %w", err)
	}
	defer closeUsageRows(rows)

	for rows.Next() {
		var binding, label, model string
		var m ModelUsage
		if err := rows.Scan(&binding, &label, &model, &m.Requests, &m.Input,
			&m.Output, &m.CacheRead, &m.CacheWrite, &m.Reasoning, &m.Total); err != nil {
			return fmt.Errorf("scan model usage totals: %w", err)
		}
		s := series[usageKey{binding, label}]
		if s == nil {
			continue
		}
		if s.ByModel == nil {
			s.ByModel = map[string]ModelUsage{}
		}
		s.ByModel[model] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read model usage totals: %w", err)
	}
	return nil
}

func (u *usageStore) addDBRecent(ctx context.Context, series map[usageKey]*usageSeries) error {
	ctx, cancel := usageDBContext(ctx)
	defer cancel()
	nowWindow := u.window(u.now())
	firstWindow := nowWindow - usageBuckets + 1
	rows, err := u.db.QueryContext(ctx, `SELECT binding, label, window, requests, total_tokens
		FROM usage_windows WHERE window >= ?`, firstWindow)
	if err != nil {
		return fmt.Errorf("read recent usage windows: %w", err)
	}
	defer closeUsageRows(rows)

	for rows.Next() {
		var binding, label string
		var window, tokens int64
		var requests int
		if err := rows.Scan(&binding, &label, &window, &requests, &tokens); err != nil {
			return fmt.Errorf("scan recent usage windows: %w", err)
		}
		s := series[usageKey{binding, label}]
		if s == nil {
			continue
		}
		s.windows[window] = requests
		if tokens > 0 {
			s.tokenWindows[window] = tokens
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read recent usage windows: %w", err)
	}
	return nil
}

func (u *usageStore) snapshotDB(ctx context.Context) []AccountUsage {
	series, err := u.readDBAccountTotals(ctx)
	if err == nil {
		err = u.addDBModelTotals(ctx, series)
	}
	if err == nil {
		err = u.addDBRecent(ctx, series)
	}
	if err != nil {
		log.Printf("egress usage database: %v", err)
		u.setReadError(err)
		return nil
	}
	u.setReadError(nil)
	now := u.window(u.now())
	out := make([]AccountUsage, 0, len(series))
	for _, s := range series {
		out = append(out, buildUsageSnapshot(s, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastRequest.After(out[j].LastRequest) })
	return out
}

type usageDayRow struct {
	binding  string
	label    string
	date     string
	requests int
	total    int64
}

func (u *usageStore) queryUsageDays(ctx context.Context, cutoffDate string) ([]usageDayRow, error) {
	ctx, cancel := usageDBContext(ctx)
	defer cancel()
	rows, err := u.db.QueryContext(ctx, `SELECT binding, label, date, requests, total_tokens
		FROM usage_days WHERE date >= ?`, cutoffDate)
	if err != nil {
		return nil, fmt.Errorf("read usage history: %w", err)
	}
	defer closeUsageRows(rows)
	return scanUsageDayRows(rows)
}

func scanUsageDayRows(rows *sql.Rows) ([]usageDayRow, error) {
	var out []usageDayRow
	for rows.Next() {
		var r usageDayRow
		if err := rows.Scan(&r.binding, &r.label, &r.date, &r.requests, &r.total); err != nil {
			return nil, fmt.Errorf("scan usage history: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read usage history: %w", err)
	}
	return out, nil
}

func (u *usageStore) historyDB(ctx context.Context) []AccountHistory {
	now := u.now().UTC()
	earliest := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -historyDays+1)
	rows, err := u.queryUsageDays(ctx, earliest.Format(historyDate))
	if err != nil {
		log.Printf("egress usage database: %v", err)
		u.setReadError(err)
		return nil
	}
	u.setReadError(nil)

	byAccount := map[usageKey]map[string]DayUsage{}
	for _, r := range rows {
		key := usageKey{r.binding, r.label}
		days := byAccount[key]
		if days == nil {
			days = map[string]DayUsage{}
			byAccount[key] = days
		}
		days[r.date] = DayUsage{Date: r.date, Requests: r.requests, Total: r.total}
	}

	out := make([]AccountHistory, 0, len(byAccount))
	for key, days := range byAccount {
		h := AccountHistory{Binding: key.secret, Label: key.label, Days: make([]DayUsage, 0, len(days))}
		for _, d := range days {
			h.Days = append(h.Days, d)
		}
		out = appendSortedAccountHistory(out, h)
	}
	sortAccountHistories(out)
	return out
}

func (u *usageStore) importLegacyHistory(path string) error {
	if path == "" {
		return nil
	}
	ctx, cancel := usageDBContext(context.Background())
	defer cancel()
	imported, err := legacyHistoryImported(ctx, u.db)
	if err != nil || imported {
		return err
	}
	saved, found, err := readLegacyHistoryFile(path)
	if err != nil || !found {
		return err
	}
	return u.writeLegacyHistory(ctx, saved)
}

func legacyHistoryImported(ctx context.Context, db *sql.DB) (bool, error) {
	var imported string
	err := db.QueryRowContext(ctx, `SELECT value FROM usage_meta WHERE key = 'legacy_history_imported'`).Scan(&imported)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy history migration state: %w", err)
	}
	return true, nil
}

func readLegacyHistoryFile(path string) ([]AccountHistory, bool, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read legacy usage history: %w", err)
	}
	var saved []AccountHistory
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, false, fmt.Errorf("parse legacy usage history %s: %w", path, err)
	}
	return saved, true, nil
}

func (u *usageStore) writeLegacyHistory(ctx context.Context, saved []AccountHistory) error {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy usage history import: %w", err)
	}
	defer rollbackUsageTx(tx)
	if err := insertLegacyHistoryRows(ctx, tx, saved); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_meta (key, value) VALUES ('legacy_history_imported', '1')`); err != nil {
		return fmt.Errorf("mark legacy usage history imported: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy usage history import: %w", err)
	}
	return nil
}

func insertLegacyHistoryRows(ctx context.Context, tx *sql.Tx, saved []AccountHistory) error {
	for _, h := range saved {
		for _, d := range h.Days {
			if err := insertLegacyHistoryRow(ctx, tx, h, d); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertLegacyHistoryRow(ctx context.Context, tx *sql.Tx, h AccountHistory, d DayUsage) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_days
		(binding, label, date, requests, total_tokens) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT (binding, label, date) DO UPDATE SET
		requests = requests + excluded.requests,
		total_tokens = total_tokens + excluded.total_tokens`,
		h.Binding, h.Label, d.Date, d.Requests, max(0, d.Total)); err != nil {
		return fmt.Errorf("import legacy usage history: %w", err)
	}
	return nil
}
