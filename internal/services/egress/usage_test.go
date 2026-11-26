package egress

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageCountsByStatusAndWindow(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	u := newUsageStore()
	u.now = func() time.Time { return now }
	for _, status := range []int{200, 200, 404, 429, 503} {
		u.record(t.Context(), "CLAUDE_ACCESS_TOKEN", "a@example.com", status)
	}
	now = now.Add(25 * time.Minute)
	u.record(t.Context(), "CLAUDE_ACCESS_TOKEN", "a@example.com", 200)
	u.record(t.Context(), "CLAUDE_ACCESS_TOKEN", "b@example.com", 200)

	got := map[string]AccountUsage{}
	for _, s := range u.snapshot(t.Context()) {
		got[s.Label] = s
	}
	a := got["a@example.com"]
	if a.Requests != 6 || a.OK != 3 || a.ClientError != 1 || a.RateLimited != 1 || a.ServerError != 1 {
		t.Errorf("a@example.com = %+v, want 6 requests: 3 ok, 1 client error, 1 rate limited, 1 server error", a)
	}
	if len(a.Recent) != usageBuckets {
		t.Fatalf("recent has %d windows, want %d", len(a.Recent), usageBuckets)
	}
	if a.Recent[usageBuckets-1] != 1 || a.Recent[usageBuckets-3] != 5 {
		t.Errorf("windows end ...%v, want 5 requests two windows before the last one", a.Recent[usageBuckets-5:])
	}
	if b := got["b@example.com"]; b.Requests != 1 {
		t.Errorf("a second account must be counted apart: %+v", b)
	}

	// Windows older than a day fall off.
	now = now.Add(25 * time.Hour)
	u.record(t.Context(), "CLAUDE_ACCESS_TOKEN", "a@example.com", 200)
	for _, s := range u.snapshot(t.Context()) {
		if s.Label != "a@example.com" {
			continue
		}
		sum := 0
		for _, n := range s.Recent {
			sum += n
		}
		if sum != 1 {
			t.Errorf("a day later the recent windows hold %d requests, want only the new one", sum)
		}
	}
}

// A rotated token must be counted against the account that was active when
// the request was made, so the label follows the secret through a reload.
func TestProxyUsageFollowsTheAccountLabel(t *testing.T) {
	dir := t.TempDir()
	token, label := filepath.Join(dir, "REAL_TOKEN"), filepath.Join(dir, "LABEL_TOKEN")
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(token, "real-1")
	write(label, "first@example.com\n")
	cfg := Config{
		Rules:   []Rule{{Name: "api", Hosts: []string{"api.example.test"}, Ports: []int{443}, Mode: ModeTunnel}},
		Secrets: map[string]SecretBinding{"TOKEN": {Placeholder: "nightshift:secret:TOKEN", File: token, LabelFile: label}},
	}
	p, err := NewProxy(cfg, Options{SkipAutoCA: true})
	if err != nil {
		t.Fatal(err)
	}
	p.record(t.Context(), AuditEvent{Action: "ALLOW", Status: 200, SecretsInjected: []string{"TOKEN"}})
	p.record(t.Context(), AuditEvent{Action: "DENY", Status: 403, SecretsInjected: []string{"TOKEN"}})

	write(token, "real-2")
	write(label, "second@example.com")
	if changed, err := p.ReloadSecrets(); err != nil || !changed {
		t.Fatalf("reload after switching account = (%v, %v)", changed, err)
	}
	p.record(t.Context(), AuditEvent{Action: "ALLOW", Status: 429, SecretsInjected: []string{"TOKEN"}})

	counts := map[string]int{}
	for _, s := range p.Usage(t.Context()) {
		counts[s.Label] += s.Requests
	}
	if counts["first@example.com"] != 1 || counts["second@example.com"] != 1 || len(counts) != 2 {
		t.Errorf("usage by account = %v, want one request each and denied requests uncounted", counts)
	}

	rec := httptest.NewRecorder()
	p.AdminHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/usage", http.NoBody))
	var served []AccountUsage
	if err := json.Unmarshal(rec.Body.Bytes(), &served); err != nil || len(served) != 2 {
		t.Errorf("/usage = %q (%v), want two accounts", rec.Body.String(), err)
	}
}
