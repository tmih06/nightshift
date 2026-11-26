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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func upstreamTLSConfig(t *testing.T, upstream *httptest.Server) *tls.Config {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
}

func newPooledTestClient(t *testing.T, upstream *httptest.Server, poolPath string, nowFn func() time.Time) (*Proxy, *http.Client) {
	t.Helper()
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
	ca, _, _, err := GenerateCA("Pool Test CA")
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		BlockPrivateIPs: boolPtr(false),
		ScrubResponses:  boolPtr(true),
		Rules: []Rule{{
			Name:    "codex",
			Hosts:   []string{upHost},
			Ports:   []int{upPort},
			Mode:    ModeMITM,
			Secrets: []string{"CODEX_ACCESS_TOKEN"},
		}},
		Secrets: map[string]SecretBinding{
			"CODEX_ACCESS_TOKEN": {
				Placeholder: "nightshift:secret:CODEX_ACCESS_TOKEN",
				PoolFile:    poolPath,
			},
		},
	}
	p, err := NewProxy(cfg, Options{
		CA:              ca,
		UpstreamTLS:     upstreamTLSConfig(t, upstream),
		Now:             nowFn,
		DefaultCooldown: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	proxyURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	certs := x509.NewCertPool()
	certs.AppendCertsFromPEM(ca.CertPEM())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: certs},
		},
	}
	return p, client
}

func writePool(t *testing.T, path string, entries []poolEntry) {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProxyFallbackChainFailsOverAndRespectsCooldowns(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	var bodies []string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, rerr := io.ReadAll(r.Body)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusBadRequest)
			return
		}
		auth := r.Header.Get("Authorization")
		acctID := r.Header.Get("ChatGPT-Account-Id")
		mu.Lock()
		calls = append(calls, auth+"|"+acctID)
		bodies = append(bodies, string(body))
		mu.Unlock()

		switch auth {
		case "Bearer tok-1":
			w.Header().Set("Retry-After", "120")
			http.Error(w, "quota exceeded", http.StatusTooManyRequests)
		case "Bearer tok-2":
			// Echo both tokens to verify scrubbing covers every account in the pool.
			if _, err := io.WriteString(w, "ok from acct2 leak=tok-1 leak2=tok-2"); err != nil {
				t.Errorf("write upstream response: %v", err)
			}
		default:
			http.Error(w, "unexpected token "+auth, http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	poolPath := filepath.Join(t.TempDir(), "POOL_CODEX_ACCESS_TOKEN")
	writePool(t, poolPath, []poolEntry{
		{Name: "codex-a.json", Label: "a@example.com", Token: "tok-1", Companion: map[string]string{"CODEX_ACCOUNT_ID": "acct-id-1"}},
		{Name: "codex-b.json", Label: "b@example.com", Token: "tok-2", Companion: map[string]string{"CODEX_ACCOUNT_ID": "acct-id-2"}},
	})
	proxy, client := newPooledTestClient(t, upstream, poolPath, func() time.Time { return now })

	sendReq := func(payload string) (int, string) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL+"/backend-api/responses", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer nightshift:secret:CODEX_ACCESS_TOKEN")
		req.Header.Set("ChatGPT-Account-Id", "nightshift:secret:CODEX_ACCOUNT_ID")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			t.Fatal(cerr)
		}
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}

	// 1. First request tries acct1 (429), fails over to acct2 (200) with the same POST body and paired account_id.
	status, body := sendReq(`{"prompt":"hello"}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", status, body)
	}
	if strings.Contains(body, "tok-1") || strings.Contains(body, "tok-2") || !strings.Contains(body, redactedMarker) {
		t.Errorf("response scrubbing must redact all pool tokens, got %q", body)
	}

	// 2. Second request skips acct1 while it is in its 120s Retry-After cooldown and goes straight to acct2.
	status, _ = sendReq(`{"prompt":"second"}`)
	if status != http.StatusOK {
		t.Fatalf("second request status = %d, want 200", status)
	}

	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	gotBodies := append([]string(nil), bodies...)
	mu.Unlock()
	wantCalls := []string{
		"Bearer tok-1|acct-id-1",
		"Bearer tok-2|acct-id-2",
		"Bearer tok-2|acct-id-2",
	}
	if strings.Join(gotCalls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want %v", gotCalls, wantCalls)
	}
	if gotBodies[0] != `{"prompt":"hello"}` || gotBodies[1] != `{"prompt":"hello"}` {
		t.Errorf("failover did not replay the POST body: %v", gotBodies)
	}

	// 3. Usage credits 429 to a@example.com and 2x 200 to b@example.com.
	usageByLabel := map[string]AccountUsage{}
	for _, u := range proxy.Usage() {
		usageByLabel[u.Label] = u
	}
	if usageByLabel["a@example.com"].RateLimited != 1 || usageByLabel["a@example.com"].Requests != 1 {
		t.Errorf("a@example.com usage = %+v, want 1 rate_limited", usageByLabel["a@example.com"])
	}
	if usageByLabel["b@example.com"].OK != 2 || usageByLabel["b@example.com"].Requests != 2 {
		t.Errorf("b@example.com usage = %+v, want 2 ok", usageByLabel["b@example.com"])
	}

	// 4. /pools reports acct1 cooling down for 120s and acct2 active_now.
	rec := httptest.NewRecorder()
	proxy.AdminHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/pools", http.NoBody))
	var pools []PoolAccountStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &pools); err != nil || len(pools) != 2 {
		t.Fatalf("/pools = %q (%v), want 2 entries", rec.Body.String(), err)
	}
	if pools[0].Name != "codex-a.json" || pools[0].ActiveNow || !pools[0].CooldownUntil.Equal(now.Add(120*time.Second)) || pools[0].Failovers != 1 {
		t.Errorf("pools[0] = %+v", pools[0])
	}
	if pools[1].Name != "codex-b.json" || !pools[1].ActiveNow || !pools[1].CooldownUntil.IsZero() {
		t.Errorf("pools[1] = %+v", pools[1])
	}

	// 5. After cooldown expires, priority #1 is tried again.
	now = now.Add(121 * time.Second)
	status, _ = sendReq(`{"prompt":"after-cooldown"}`)
	if status != http.StatusOK {
		t.Fatalf("after cooldown status = %d, want 200", status)
	}
	mu.Lock()
	after := calls[3:]
	mu.Unlock()
	if len(after) != 2 || after[0] != "Bearer tok-1|acct-id-1" || after[1] != "Bearer tok-2|acct-id-2" {
		t.Errorf("after cooldown expired, expected retry of #1 then #2, got %v", after)
	}
}

func TestReloadClearsUnauthorizedCooldownWhenTokenRefreshes(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		mu.Lock()
		calls = append(calls, auth)
		mu.Unlock()
		switch auth {
		case "Bearer expired-1":
			http.Error(w, "expired", http.StatusUnauthorized)
		case "Bearer fresh-1", "Bearer tok-2":
			if _, err := io.WriteString(w, "ok"); err != nil {
				t.Errorf("write upstream response: %v", err)
			}
		default:
			http.Error(w, "bad", http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	poolPath := filepath.Join(t.TempDir(), "POOL_CODEX_ACCESS_TOKEN")
	writePool(t, poolPath, []poolEntry{
		{Name: "codex-a.json", Label: "a@example.com", Token: "expired-1"},
		{Name: "codex-b.json", Label: "b@example.com", Token: "tok-2"},
	})
	proxy, client := newPooledTestClient(t, upstream, poolPath, func() time.Time { return now })

	send := func() int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL+"/v1", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer nightshift:secret:CODEX_ACCESS_TOKEN")
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
		return resp.StatusCode
	}

	// First request fails over on 401 from expired-1 to tok-2.
	if got := send(); got != http.StatusOK {
		t.Fatalf("first status = %d, want 200", got)
	}
	// Refresh account 1's token and reload: its 401 cooldown must clear immediately.
	writePool(t, poolPath, []poolEntry{
		{Name: "codex-a.json", Label: "a@example.com", Token: "fresh-1"},
		{Name: "codex-b.json", Label: "b@example.com", Token: "tok-2"},
	})
	if changed, err := proxy.ReloadSecrets(); err != nil || !changed {
		t.Fatalf("ReloadSecrets = (%v, %v), want (true, nil)", changed, err)
	}
	if got := send(); got != http.StatusOK {
		t.Fatalf("after refresh status = %d, want 200", got)
	}

	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()
	want := []string{"Bearer expired-1", "Bearer tok-2", "Bearer fresh-1"}
	if strings.Join(gotCalls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", gotCalls, want)
	}
}
