package egress

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExtractTokenMetricsAcrossProviders(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		want        TokenMetrics
	}{
		{
			name:        "anthropic sse",
			contentType: "text/event-stream",
			body: strings.Join([]string{
				"event: message_start",
				`data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":1200,"cache_creation_input_tokens":400,"cache_read_input_tokens":3600,"output_tokens":1}}}`,
				"event: message_delta",
				`data: {"type":"message_delta","usage":{"output_tokens":350}}`,
			}, "\n"),
			want: TokenMetrics{
				Model: "claude-sonnet-4-6", Input: 1200, Output: 350,
				CacheRead: 3600, CacheWrite: 400, Total: 5550,
			},
		},
		{
			name:        "codex responses sse",
			contentType: "text/event-stream",
			body:        "event: response.completed\n" + `data: {"type":"response.completed","response":{"model":"gpt-5.4","usage":{"input_tokens":2000,"output_tokens":600,"total_tokens":2600,"input_tokens_details":{"cached_tokens":1500},"output_tokens_details":{"reasoning_tokens":250}}}}`,
			want: TokenMetrics{
				Model: "gpt-5.4", Input: 2000, Output: 600,
				CacheRead: 1500, Reasoning: 250, Total: 2600,
			},
		},
		{
			name:        "gemini antigravity json",
			contentType: "application/json",
			body:        `{"response":{"modelVersion":"gemini-2.5-pro","usageMetadata":{"promptTokenCount":900,"candidatesTokenCount":240,"cachedContentTokenCount":500,"thoughtsTokenCount":120,"totalTokenCount":1260}}}`,
			want: TokenMetrics{
				Model: "gemini-2.5-pro", Input: 900, Output: 240,
				CacheRead: 500, Reasoning: 120, Total: 1260,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTokenMetrics(tc.contentType, []byte(tc.body))
			if got != tc.want {
				t.Errorf("extractTokenMetrics = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExtractHeaderQuotaParsesAnthropicAndCodexHeaders(t *testing.T) {
	now := time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC)
	hdr := http.Header{}
	hdr.Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.42")
	hdr.Set("Anthropic-Ratelimit-Unified-5h-Reset", "2026-11-26T07:00:00Z")
	hdr.Set("Anthropic-Ratelimit-Unified-5h-Status", "allowed")
	hdr.Set("Anthropic-Ratelimit-Unified-7d-Utilization", "0.18")
	hdr.Set("X-Codex-Plan-Type", "pro")
	hdr.Set("X-Codex-Primary-Used-Percent", "35")
	hdr.Set("X-Codex-Primary-Window-Minutes", "300")
	hdr.Set("X-Codex-Primary-Reset-After-Seconds", "7200")

	plan, wins := extractHeaderQuota(now, hdr)
	if plan != "pro" {
		t.Errorf("plan = %q, want pro", plan)
	}
	if len(wins) != 3 {
		t.Fatalf("got %d windows (%+v), want 3", len(wins), wins)
	}
	if wins[0].Name != "5h" || wins[0].UsedPercent != 42 || wins[0].ResetsAt.Hour() != 7 {
		t.Errorf("wins[0] = %+v", wins[0])
	}
	if wins[2].UsedPercent != 35 || !wins[2].ResetsAt.Equal(now.Add(2*time.Hour)) {
		t.Errorf("wins[2] = %+v", wins[2])
	}
}

func TestActiveQuotaFetcherFetchesAllThreeProviders(t *testing.T) {
	now := time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC)
	var gotCodexAcctID, gotAgyUA, gotAgyProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/claude":
			if r.Header.Get("Authorization") != "Bearer tok-claude" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, err := io.WriteString(w, `{"five_hour":{"utilization":42.0,"resets_at":"2026-11-26T05:00:00Z"},"seven_day":{"utilization":15.0,"resets_at":"2026-12-01T00:00:00Z"},"extra_usage":{"is_enabled":true,"monthly_limit":5000,"used_credits":1200}}`)
			if err != nil {
				t.Errorf("write claude: %v", err)
			}
		case "/codex":
			gotCodexAcctID = r.Header.Get("ChatGPT-Account-Id")
			_, err := io.WriteString(w, `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":65,"limit_window_seconds":18000,"reset_after_seconds":3600},"secondary_window":{"used_percent":20,"limit_window_seconds":604800,"reset_after_seconds":86400}},"credits":{"has_credits":true,"balance":"14.50"}}`)
			if err != nil {
				t.Errorf("write codex: %v", err)
			}
		case "/agy-load":
			_, err := io.WriteString(w, `{"cloudaicompanionProject":"project-42","paidTier":{"name":"Google AI Pro"},"currentTier":{"name":"Antigravity"}}`)
			if err != nil {
				t.Errorf("write agy load: %v", err)
			}
		case "/agy":
			gotAgyUA = r.Header.Get("User-Agent")
			var body struct {
				Project string `json:"project"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode agy request: %v", err)
			}
			gotAgyProject = body.Project
			_, err := io.WriteString(w, `{"groups":[{"displayName":"Gemini Models","buckets":[{"bucketId":"gemini-weekly","displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.75,"resetTime":"2026-12-03T07:03:48Z"},{"bucketId":"gemini-5h","displayName":"Five Hour Limit Remaining","window":"5h","remainingFraction":0.25,"resetTime":"2026-11-26T06:00:00Z"},{"bucketId":"gemini-empty","displayName":"Five Hour Limit Remaining","window":"5h","resetTime":"2026-11-26T08:00:00Z"},{"bucketId":"gemini-unknown","displayName":"Five Hour Limit Remaining","window":"5h"},{"bucketId":"credits","displayName":"Credits","window":"weekly","remainingAmount":"1500","resetTime":"2026-12-01T00:00:00Z"},{"bucketId":"gemini-disabled","displayName":"Five Hour Limit Remaining","window":"5h","disabled":true,"remainingFraction":0.9,"resetTime":"2026-11-26T09:00:00Z"}]},{"displayName":"Claude and GPT models","buckets":[{"bucketId":"3p-weekly","displayName":"Weekly Limit Remaining","window":"weekly","remainingFraction":0.5,"resetTime":"2026-12-02T15:55:37Z"},{"bucketId":"3p-5h","displayName":"Five Hour Limit Remaining","window":"5h","remainingFraction":1,"resetTime":"2026-11-26T07:00:00Z"}]}]}`)
			if err != nil {
				t.Errorf("write agy: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	claudePool := filepath.Join(dir, "POOL_CLAUDE")
	codexPool := filepath.Join(dir, "POOL_CODEX")
	agyPool := filepath.Join(dir, "POOL_AGY")
	writePool(t, claudePool, []poolEntry{{Name: "claude-a.json", Label: "c@example.com", Token: "tok-claude"}})
	writePool(t, codexPool, []poolEntry{{Name: "codex-a.json", Label: "x@example.com", Token: "tok-codex", Companion: map[string]string{"CODEX_ACCOUNT_ID": "acct-99"}}})
	writePool(t, agyPool, []poolEntry{{Name: "agy-a.json", Label: "g@example.com", Token: "tok-agy"}})

	cfg := Config{
		BlockPrivateIPs: boolPtr(false),
		Secrets: map[string]SecretBinding{
			"CLAUDE_ACCESS_TOKEN": {PoolFile: claudePool},
			"CODEX_ACCESS_TOKEN":  {PoolFile: codexPool},
			"AGY_ACCESS_TOKEN":    {PoolFile: agyPool},
		},
	}
	p, err := NewProxy(cfg, Options{
		SkipAutoCA: true,
		Now:        func() time.Time { return now },
		QuotaEndpoints: QuotaEndpoints{
			Claude:          srv.URL + "/claude",
			Codex:           srv.URL + "/codex",
			Antigravity:     srv.URL + "/agy",
			AntigravityLoad: srv.URL + "/agy-load",

			ClaudeModels:      srv.URL + "/claude-models",
			CodexModels:       srv.URL + "/codex-models",
			AntigravityModels: srv.URL + "/agy-models",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	p.AdminHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/quota/refresh", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /quota/refresh = %d: %s", rec.Code, rec.Body.String())
	}
	var quotas []AccountQuota
	if err := json.Unmarshal(rec.Body.Bytes(), &quotas); err != nil {
		t.Fatal(err)
	}
	if len(quotas) != 3 {
		t.Fatalf("got %d quotas, want 3: %+v", len(quotas), quotas)
	}
	if gotCodexAcctID != "acct-99" {
		t.Errorf("ChatGPT-Account-Id = %q, want acct-99", gotCodexAcctID)
	}
	if !strings.HasPrefix(gotAgyUA, "antigravity/") {
		t.Errorf("Antigravity User-Agent = %q, want antigravity/ prefix", gotAgyUA)
	}
	if gotAgyProject != "project-42" {
		t.Errorf("Antigravity quota project = %q, want the loadCodeAssist project", gotAgyProject)
	}

	byBinding := map[string]AccountQuota{}
	for _, q := range quotas {
		byBinding[q.Binding] = q
	}
	cl := byBinding["CLAUDE_ACCESS_TOKEN"]
	if len(cl.Windows) != 2 || cl.Windows[0].UsedPercent != 42 || cl.CreditsBalance != "1200/5000" || cl.NextResetAt.Hour() != 5 {
		t.Errorf("claude quota = %+v", cl)
	}
	cx := byBinding["CODEX_ACCESS_TOKEN"]
	if cx.Plan != "plus" || cx.CreditsBalance != "14.50" || len(cx.Windows) != 2 || cx.Windows[0].UsedPercent != 65 || !cx.NextResetAt.Equal(now.Add(time.Hour)) {
		t.Errorf("codex quota = %+v", cx)
	}
	ag := byBinding["AGY_ACCESS_TOKEN"]
	if ag.Plan != "Google AI Pro" || len(ag.Windows) != 7 {
		t.Fatalf("agy quota = %+v", ag)
	}
	for i, want := range []QuotaWindow{
		{Name: "gemini-weekly", Label: "Weekly Limit Remaining", Group: "Gemini Models", UsedPercent: 25, WindowMinutes: 10080},
		{Name: "gemini-5h", Label: "Five Hour Limit Remaining", Group: "Gemini Models", UsedPercent: 75, WindowMinutes: 300},
		{Name: "gemini-empty", Label: "Five Hour Limit Remaining", Group: "Gemini Models", UsedPercent: 100, WindowMinutes: 300},
		{Name: "gemini-unknown", Label: "Five Hour Limit Remaining", Group: "Gemini Models", WindowMinutes: 300, Status: "unknown"},
		{Name: "credits", Label: "Credits", Group: "Gemini Models", WindowMinutes: 10080, Status: "amount", Remaining: 1500},
		{Name: "3p-weekly", Label: "Weekly Limit Remaining", Group: "Claude and GPT models", UsedPercent: 50, WindowMinutes: 10080},
		{Name: "3p-5h", Label: "Five Hour Limit Remaining", Group: "Claude and GPT models", UsedPercent: 0, WindowMinutes: 300},
	} {
		got := ag.Windows[i]
		if got.Name != want.Name || got.Label != want.Label || got.Group != want.Group || got.UsedPercent != want.UsedPercent || got.WindowMinutes != want.WindowMinutes || got.Status != want.Status || got.Remaining != want.Remaining {
			t.Errorf("agy window %d = %+v, want %+v", i, got, want)
		}
	}
	if !ag.NextResetAt.Equal(time.Date(2026, 11, 26, 6, 0, 0, 0, time.UTC)) {
		t.Errorf("agy next reset = %v", ag.NextResetAt)
	}
}

func TestAntigravityQuotaOmitsProjectWhenLoadNamesNone(t *testing.T) {
	var quotaBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/load" {
			if _, err := io.WriteString(w, `{}`); err != nil {
				t.Errorf("write load: %v", err)
			}
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read quota body: %v", err)
		}
		quotaBody = string(raw)
		if _, err := io.WriteString(w, `{"groups":[]}`); err != nil {
			t.Errorf("write quota: %v", err)
		}
	}))
	defer srv.Close()
	qs := newQuotaStore(nil, nil, QuotaEndpoints{Antigravity: srv.URL + "/quota", AntigravityLoad: srv.URL + "/load"})
	var out AccountQuota
	if err := qs.fetchAntigravity(t.Context(), resolvedAccount{value: "tok"}, &out); err != nil {
		t.Fatal(err)
	}
	if quotaBody != `{}` {
		t.Errorf("quota body = %q, want no project key", quotaBody)
	}
}

func TestProxyMITMTracksTokensHeadersAndAnalytics(t *testing.T) {
	now := time.Date(2026, 11, 26, 2, 0, 0, 0, time.UTC)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Utilization", "0.55")
		w.Header().Set("Anthropic-Ratelimit-Unified-5h-Reset", "2026-11-26T06:00:00Z")
		_, err := io.WriteString(w, `{"model":"claude-sonnet-4-6","usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":3000,"cache_creation_input_tokens":500}}`)
		if err != nil {
			t.Errorf("write upstream: %v", err)
		}
	}))
	defer upstream.Close()

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upHost, upPortStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	upPort, err := strconv.Atoi(upPortStr)
	if err != nil {
		t.Fatal(err)
	}
	ca, _, _, err := GenerateCA("Analytics CA")
	if err != nil {
		t.Fatal(err)
	}
	poolPath := filepath.Join(t.TempDir(), "POOL_CLAUDE")
	writePool(t, poolPath, []poolEntry{{Name: "claude-a.json", Label: "a@example.com", Token: "tok-a"}})

	p, err := NewProxy(Config{
		BlockPrivateIPs: boolPtr(false),
		ScrubResponses:  boolPtr(true),
		Rules: []Rule{{
			Name: "anthropic", Hosts: []string{upHost}, Ports: []int{upPort},
			Mode: ModeMITM, Secrets: []string{"CLAUDE_ACCESS_TOKEN"},
		}},
		Secrets: map[string]SecretBinding{
			"CLAUDE_ACCESS_TOKEN": {Placeholder: "nightshift:secret:CLAUDE_ACCESS_TOKEN", PoolFile: poolPath},
		},
	}, Options{
		CA:          ca,
		UpstreamTLS: upstreamTLSConfig(t, upstream),
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(p)
	defer proxySrv.Close()
	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	certs := x509.NewCertPool()
	certs.AppendCertsFromPEM(ca.CertPEM())
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: certs},
	}}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL+"/v1/messages", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer nightshift:secret:CLAUDE_ACCESS_TOKEN")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	p.AdminHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/analytics", http.NoBody))
	var an AnalyticsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &an); err != nil {
		t.Fatal(err)
	}
	if an.Requests != 1 || an.InputTokens != 1000 || an.OutputTokens != 200 || an.CacheRead != 3000 || an.CacheWrite != 500 || an.TotalTokens != 4700 {
		t.Errorf("analytics tokens = %+v", an)
	}
	if an.ByModel["claude-sonnet-4-6"].Requests != 1 || an.CacheHitPercent < 66 {
		t.Errorf("analytics model/cache = %+v", an)
	}
	if len(an.Quotas) != 1 || len(an.Quotas[0].Windows) != 1 || an.Quotas[0].Windows[0].UsedPercent != 55 {
		t.Errorf("analytics header quota = %+v", an.Quotas)
	}
}
