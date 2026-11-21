package egress

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func boolPtr(v bool) *bool { return &v }

func TestIsRestrictedIP(t *testing.T) {
	restricted := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.5",
		"192.168.1.10",
		"169.254.169.254",
		"100.64.0.1",
		"198.18.0.1",
		"0.0.0.0",
		"::1",
		"::ffff:127.0.0.1",
		"::ffff:169.254.169.254",
		"fc00::1",
		"fe80::1",
	}
	for _, s := range restricted {
		ip := netip.MustParseAddr(s)
		if !IsRestrictedIP(ip) {
			t.Errorf("IsRestrictedIP(%s) = false, want true", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "140.82.121.4", "2606:4700:4700::1111"}
	for _, s := range allowed {
		ip := netip.MustParseAddr(s)
		if IsRestrictedIP(ip) {
			t.Errorf("IsRestrictedIP(%s) = true, want false", s)
		}
	}
}

func TestConfigMatchHostAndPathTraversal(t *testing.T) {
	cfg, err := ParseConfig([]byte(`
rules:
  - name: npm
    hosts: ["*.npmjs.org"]
    ports: [443]
    mode: tunnel
  - name: repo
    hosts: ["github.com"]
    ports: [443]
    mode: mitm
    methods: ["GET", "POST"]
    path_prefixes: ["/acme/rocket.git"]
`))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if _, ok := cfg.MatchHostPort("registry.npmjs.org", 443); !ok {
		t.Error("wildcard did not match registry.npmjs.org")
	}
	if _, ok := cfg.MatchHostPort("npmjs.org", 443); ok {
		t.Error("wildcard matched bare apex npmjs.org")
	}
	if _, ok := cfg.MatchHostPort("evil-npmjs.org", 443); ok {
		t.Error("wildcard matched lookalike evil-npmjs.org")
	}
	if _, ok := cfg.MatchHostPort("registry.npmjs.org", 80); ok {
		t.Error("matched disallowed port 80")
	}

	r, ok := cfg.MatchHostPort("github.com", 443)
	if !ok {
		t.Fatal("github.com did not match")
	}
	if !r.AllowsRequest("GET", "/acme/rocket.git/info/refs") {
		t.Error("allowed path rejected")
	}
	if r.AllowsRequest("DELETE", "/acme/rocket.git/info/refs") {
		t.Error("disallowed method DELETE permitted")
	}
	if r.AllowsRequest("GET", "/acme/other.git/info/refs") {
		t.Error("disallowed repo path permitted")
	}
	if r.AllowsRequest("GET", "/acme/rocket.git/../other.git/info/refs") {
		t.Error("path traversal bypassed path_prefixes")
	}
}

func TestProxyMITMSecretInjectionAndScrubbing(t *testing.T) {
	const (
		realAnthropic = "sk-ant-real-secret-value-987654"
		realGitHub    = "ghp_RealGitHubSecretToken123456"
		phAnthropic   = "nightshift:secret:ANTHROPIC_API_KEY"
		phGitHub      = "nightshift:secret:GITHUB_TOKEN"
	)

	// Upstream HTTPS server simulating both API header auth, Git Basic auth,
	// query auth, and a hostile echo endpoint that reflects secrets back.
	var gotAuth, gotAPIKey, gotQuery string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		gotQuery = r.URL.Query().Get("key")

		switch r.URL.Path {
		case "/acme/rocket.git/info/refs":
			w.Header().Set("X-Echo", gotAuth)
			_, _ = io.WriteString(w, "ok")
		case "/echo":
			// Reflect raw, base64, URL-encoded, and JSON-escaped secret forms.
			w.Header().Set("X-Leaked", realAnthropic)
			payload := strings.Join([]string{
				"raw=" + realAnthropic,
				"b64=" + base64.StdEncoding.EncodeToString([]byte(realAnthropic)),
				"url=" + url.QueryEscape(realAnthropic),
			}, "\n")
			_, _ = io.WriteString(w, payload)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	defer upstream.Close()

	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	upHost, upPortStr, _ := net.SplitHostPort(u.Host)
	upPort, _ := strconv.Atoi(upPortStr)

	ca, _, _, err := GenerateCA("Test Proxy CA")
	if err != nil {
		t.Fatal(err)
	}

	var auditBuf bytes.Buffer
	cfg := Config{
		BlockPrivateIPs: boolPtr(false), // allow 127.0.0.1 for httptest upstream
		ScrubResponses:  boolPtr(true),
		Rules: []Rule{
			{
				Name:         "allowed-mitm",
				Hosts:        []string{upHost},
				Ports:        []int{upPort},
				Mode:         ModeMITM,
				Methods:      []string{"GET", "POST"},
				PathPrefixes: []string{"/acme/rocket.git", "/echo"},
				Secrets:      []string{"ANTHROPIC_API_KEY", "GITHUB_TOKEN"},
			},
		},
		Secrets: map[string]SecretBinding{
			"ANTHROPIC_API_KEY": {Placeholder: phAnthropic, Value: realAnthropic},
			"GITHUB_TOKEN":      {Placeholder: phGitHub, Value: realGitHub},
		},
	}

	proxy, err := NewProxy(cfg, Options{
		CA:          ca,
		UpstreamTLS: &tls.Config{InsecureSkipVerify: true},
		AuditWriter: &auditBuf,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	proxyURL, _ := url.Parse(proxySrv.URL)
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.CertPEM()) {
		t.Fatal("failed to append proxy CA cert")
	}
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}

	// 1. Authorized Git Smart-HTTP Basic Auth placeholder swap.
	req, _ := http.NewRequest("GET", upstream.URL+"/acme/rocket.git/info/refs?key="+url.QueryEscape(phAnthropic), nil)
	req.SetBasicAuth("x-access-token", phGitHub)
	req.Header.Set("X-Api-Key", phAnthropic)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("MITM request failed: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+realGitHub))
	if gotAuth != wantBasic {
		t.Errorf("Basic auth not swapped: got %q, want %q", gotAuth, wantBasic)
	}
	if gotAPIKey != realAnthropic {
		t.Errorf("X-Api-Key = %q, want real secret", gotAPIKey)
	}
	if gotQuery != realAnthropic {
		t.Errorf("query key = %q, want real secret", gotQuery)
	}

	// 2. Hostile echo response must scrub raw, base64, and URL-encoded forms.
	req, _ = http.NewRequest("GET", upstream.URL+"/echo", nil)
	req.Header.Set("X-Api-Key", phAnthropic)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("echo request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Contains(string(body), realAnthropic) ||
		strings.Contains(string(body), base64.StdEncoding.EncodeToString([]byte(realAnthropic))) ||
		strings.Contains(resp.Header.Get("X-Leaked"), realAnthropic) {
		t.Errorf("secret leaked in scrubbed response: header=%q body=%q", resp.Header.Get("X-Leaked"), string(body))
	}
	if !strings.Contains(string(body), redactedMarker) {
		t.Errorf("expected [REDACTED] in scrubbed body: %q", string(body))
	}

	// 3. Disallowed path is blocked with 403.
	req, _ = http.NewRequest("GET", upstream.URL+"/other/repo.git/info/refs", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("blocked path request failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("disallowed path status = %d, want 403", resp.StatusCode)
	}

	// 4. Disallowed host is blocked on CONNECT before any TLS handshake.
	req, _ = http.NewRequest("GET", "https://evil.example.com/", nil)
	resp, err = client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("disallowed host unexpectedly succeeded with status %d", resp.StatusCode)
	}

	// 5. Admin handler serves /healthz, /ca.crt, and /events.
	adminSrv := httptest.NewServer(proxy.AdminHandler())
	defer adminSrv.Close()

	hResp, err := http.Get(adminSrv.URL + "/healthz")
	if err != nil || hResp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz failed: err=%v resp=%v", err, hResp)
	}
	_ = hResp.Body.Close()

	caResp, err := http.Get(adminSrv.URL + "/ca.crt")
	if err != nil || caResp.StatusCode != http.StatusOK {
		t.Fatalf("/ca.crt failed: err=%v resp=%v", err, caResp)
	}
	caBytes, _ := io.ReadAll(caResp.Body)
	_ = caResp.Body.Close()
	if !strings.Contains(string(caBytes), "BEGIN CERTIFICATE") || strings.Contains(string(caBytes), "PRIVATE KEY") {
		t.Errorf("unexpected /ca.crt body: %q", string(caBytes))
	}

	evResp, err := http.Get(adminSrv.URL + "/events")
	if err != nil || evResp.StatusCode != http.StatusOK {
		t.Fatalf("/events failed: err=%v resp=%v", err, evResp)
	}
	var events []AuditEvent
	if err := json.NewDecoder(evResp.Body).Decode(&events); err != nil {
		t.Fatalf("decode /events: %v", err)
	}
	_ = evResp.Body.Close()
	if len(events) < 3 {
		t.Errorf("expected at least 3 audit events, got %d", len(events))
	}
}

func TestProxyNeverInjectsSecretForUnauthorizedHost(t *testing.T) {
	const (
		realSecret = "sk-ant-top-secret-999"
		phSecret   = "nightshift:secret:ANTHROPIC_API_KEY"
	)
	var seenHeader string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeader = r.Header.Get("X-Api-Key")
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	upHost, upPortStr, _ := net.SplitHostPort(u.Host)
	upPort, _ := strconv.Atoi(upPortStr)

	ca, _, _, err := GenerateCA("Test CA")
	if err != nil {
		t.Fatal(err)
	}
	// Rule allows upHost via MITM, but does NOT bind ANTHROPIC_API_KEY to it.
	cfg := Config{
		BlockPrivateIPs: boolPtr(false),
		Rules: []Rule{
			{Name: "other-host", Hosts: []string{upHost}, Ports: []int{upPort}, Mode: ModeMITM},
		},
		Secrets: map[string]SecretBinding{
			"ANTHROPIC_API_KEY": {Placeholder: phSecret, Value: realSecret},
		},
	}
	proxy, err := NewProxy(cfg, Options{CA: ca, UpstreamTLS: &tls.Config{InsecureSkipVerify: true}})
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	proxyURL, _ := url.Parse(proxySrv.URL)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.CertPEM())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}
	req, _ := http.NewRequest("GET", upstream.URL+"/steal", nil)
	req.Header.Set("X-Api-Key", phSecret)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()
	if seenHeader == realSecret {
		t.Fatal("real secret was injected for an unauthorized host!")
	}
	if seenHeader != phSecret {
		t.Errorf("expected untouched placeholder %q, got %q", phSecret, seenHeader)
	}
}

func TestProxyTunnelModePassThroughAndSSRFBlock(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tunneled")
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	upHost, upPortStr, _ := net.SplitHostPort(u.Host)
	upPort, _ := strconv.Atoi(upPortStr)

	// 1. With BlockPrivateIPs=true (default), tunneling to 127.0.0.1 is blocked.
	strictProxy, err := NewProxy(Config{
		Rules: []Rule{{Name: "local", Hosts: []string{upHost}, Ports: []int{upPort}, Mode: ModeTunnel}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	strictSrv := httptest.NewServer(strictProxy)
	defer strictSrv.Close()
	strictURL, _ := url.Parse(strictSrv.URL)
	strictClient := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(strictURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	if resp, err := strictClient.Get(upstream.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatalf("SSRF guard allowed tunnel to 127.0.0.1 (status %d)", resp.StatusCode)
	}

	// 2. With BlockPrivateIPs=false, tunnel mode passes TLS through without MITM.
	openProxy, err := NewProxy(Config{
		BlockPrivateIPs: boolPtr(false),
		Rules:           []Rule{{Name: "local", Hosts: []string{upHost}, Ports: []int{upPort}, Mode: ModeTunnel}},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	openSrv := httptest.NewServer(openProxy)
	defer openSrv.Close()
	openURL, _ := url.Parse(openSrv.URL)
	openClient := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(openURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := openClient.Get(upstream.URL)
	if err != nil {
		t.Fatalf("tunnel request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "tunneled" {
		t.Errorf("tunnel body = %q, want tunneled", string(body))
	}
}
