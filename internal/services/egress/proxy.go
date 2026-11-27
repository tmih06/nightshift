package egress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// auditAllow is the Action of a request the policy let through.
const auditAllow = "ALLOW"

// AuditEvent records one allowed or denied outbound egress request.
type AuditEvent struct {
	Time            time.Time `json:"time"`
	Action          string    `json:"action"` // ALLOW or DENY
	Mode            string    `json:"mode"`   // http, tunnel, or mitm
	Client          string    `json:"client"`
	Method          string    `json:"method"`
	Host            string    `json:"host"`
	Port            int       `json:"port"`
	Path            string    `json:"path,omitempty"`
	Rule            string    `json:"rule,omitempty"`
	Status          int       `json:"status,omitempty"`
	SecretsInjected []string  `json:"secrets_injected,omitempty"`
	Account         string    `json:"account,omitempty"`
	Failover        bool      `json:"failover,omitempty"`
	Scrubbed        bool      `json:"scrubbed,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
}

// Options tunes a Proxy instance (all fields optional).
type Options struct {
	CA                     *CA
	Getenv                 func(string) string
	UpstreamTLS            *tls.Config // optional custom upstream TLS config (used in tests)
	AuditWriter            io.Writer   // optional JSONL audit sink
	EventRingMax           int
	SkipAutoCA             bool
	PlainHTTPDefault       bool
	Now                    func() time.Time // optional clock override (used in tests)
	DefaultCooldown        time.Duration    // optional failover cooldown override
	MaxRetryBodyBytes      int64            // max request body buffered for failover retries
	QuotaEndpoints         QuotaEndpoints   // optional provider quota URL overrides
	UsageDBPath            string           // optional SQLite database that persists all usage
	LegacyUsageHistoryPath string           // optional pre-SQLite JSON history imported once
}

// Proxy is the HTTP/HTTPS egress proxy handler.
type Proxy struct {
	cfg               Config
	ca                *CA
	dialer            *SafeDialer
	secrets           atomic.Pointer[SecretEngine]
	getenv            func(string) string
	usage             *usageStore
	pool              *poolState
	quota             *quotaStore
	maxRetryBodyBytes int64
	tr                *http.Transport

	auditMu sync.Mutex
	auditW  io.Writer
	ring    []AuditEvent
	ringMax int
}

// proxyCA is the CA the proxy signs with: the one given, else the one the
// config names, else a new one when a rule intercepts TLS. It is nil when none
// is needed or opts says not to make one.
func proxyCA(cfg Config, opts Options) (*CA, error) {
	ca := opts.CA
	if ca != nil || opts.SkipAutoCA {
		return ca, nil
	}
	if cfg.CA.CertFile != "" && cfg.CA.KeyFile != "" {
		return LoadCA(cfg.CA.CertFile, cfg.CA.KeyFile)
	}
	if !needsMITM(cfg) {
		return ca, nil
	}
	ca, _, _, err := GenerateCA("Nightshift Egress Proxy CA")
	return ca, err
}

// NewProxy constructs a validated Proxy from cfg and opts.
func NewProxy(cfg Config, opts Options) (*Proxy, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	secrets, err := NewSecretEngine(cfg.Secrets, opts.Getenv)
	if err != nil {
		return nil, err
	}

	ca, err := proxyCA(cfg, opts)
	if err != nil {
		return nil, err
	}

	dialer := NewSafeDialer(cfg.ShouldBlockPrivateIPs())
	var tlsCfg *tls.Config
	if opts.UpstreamTLS != nil {
		tlsCfg = opts.UpstreamTLS.Clone()
	}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          64,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
	}

	ringMax := opts.EventRingMax
	if ringMax <= 0 {
		ringMax = 200
	}
	maxBody := opts.MaxRetryBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxRetryBodyBytes
	}
	usage := newUsageStore()
	if opts.UsageDBPath != "" {
		usage, err = openSQLiteUsageStore(opts.UsageDBPath, opts.LegacyUsageHistoryPath)
		if err != nil {
			return nil, err
		}
	}
	if opts.Now != nil {
		usage.now = opts.Now
	}

	p := &Proxy{
		cfg:               cfg,
		ca:                ca,
		dialer:            dialer,
		getenv:            opts.Getenv,
		usage:             usage,
		pool:              newPoolState(opts.Now, opts.DefaultCooldown),
		quota:             newQuotaStore(opts.Now, tr, opts.QuotaEndpoints),
		maxRetryBodyBytes: maxBody,
		tr:                tr,
		auditW:            opts.AuditWriter,
		ringMax:           ringMax,
	}
	p.secrets.Store(secrets)
	return p, nil
}

func needsMITM(cfg Config) bool {
	for _, r := range cfg.Rules {
		if r.Mode == ModeMITM {
			return true
		}
	}
	return false
}

// CA returns the proxy's MITM CA (nil when no MITM rules exist and none was set).
func (p *Proxy) CA() *CA { return p.ca }

// Close releases resources owned by the proxy.
func (p *Proxy) Close() error { return p.usage.close() }

// Pools returns the live fallback-chain and cooldown state of every loaded
// secret's accounts.
func (p *Proxy) Pools() []PoolAccountStatus { return p.pool.snapshot(p.secrets.Load()) }

// Events returns a snapshot of the most recent audit events.
func (p *Proxy) Events() []AuditEvent {
	p.auditMu.Lock()
	defer p.auditMu.Unlock()
	out := make([]AuditEvent, len(p.ring))
	copy(out, p.ring)
	return out
}

func (p *Proxy) record(ctx context.Context, ev AuditEvent) {
	p.recordTelemetry(ctx, ev, nil, TokenMetrics{})
}

func (p *Proxy) recordTelemetry(ctx context.Context, ev AuditEvent, bindings []injectedBinding, tok TokenMetrics) {
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.Action == auditAllow {
		p.recordUsage(ctx, ev, bindings, tok)
	}
	p.appendAudit(ev)
}

func (p *Proxy) recordUsage(ctx context.Context, ev AuditEvent, bindings []injectedBinding, tok TokenMetrics) {
	if len(bindings) > 0 {
		for _, b := range bindings {
			p.usage.recordDetailed(ctx, b.secret, b.label, usageObservation{status: ev.Status, latencyMS: ev.DurationMS, tokens: tok})
		}
		return
	}
	engine := p.secrets.Load()
	for _, name := range ev.SecretsInjected {
		label := engine.Label(name)
		if ev.Account != "" && len(ev.SecretsInjected) == 1 {
			label = ev.Account
		}
		p.usage.recordDetailed(ctx, name, label, usageObservation{status: ev.Status, latencyMS: ev.DurationMS, tokens: tok})
	}
}

func (p *Proxy) appendAudit(ev AuditEvent) {
	p.auditMu.Lock()
	p.ring = append(p.ring, ev)
	if len(p.ring) > p.ringMax {
		p.ring = p.ring[len(p.ring)-p.ringMax:]
	}
	w := p.auditW
	p.auditMu.Unlock()
	if w != nil {
		if b, err := json.Marshal(ev); err == nil {
			if _, werr := w.Write(append(b, '\n')); werr != nil {
				return
			}
		}
	}
}

// AdminHandler exposes health, public CA cert, usage, pool failover state,
// live quotas, analytics, and recent audit events on a separate admin port
// that sandbox pods cannot reach.
func (p *Proxy) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/ca.crt", func(w http.ResponseWriter, _ *http.Request) {
		if p.ca == nil {
			http.Error(w, "no CA configured", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		_, _ = w.Write(p.ca.CertPEM())
	})
	mux.HandleFunc("/usage", func(w http.ResponseWriter, r *http.Request) {
		writeUsageJSON(w, p, p.Usage(r.Context()), "encode usage")
	})
	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		writeUsageJSON(w, p, p.History(r.Context()), "encode history")
	})
	mux.HandleFunc("/pools", func(w http.ResponseWriter, _ *http.Request) {
		writeAdminJSON(w, p.Pools(), "encode pools")
	})
	mux.HandleFunc("/quota", func(w http.ResponseWriter, _ *http.Request) {
		writeAdminJSON(w, p.Quota(), "encode quota")
	})
	mux.HandleFunc("/quota/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeAdminJSON(w, p.RefreshQuota(r.Context()), "encode quota")
	})
	mux.HandleFunc("/analytics", func(w http.ResponseWriter, r *http.Request) {
		writeUsageJSON(w, p, p.Analytics(r.Context()), "encode analytics")
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, _ *http.Request) {
		writeAdminJSON(w, p.Events(), "encode events")
	})
	return mux
}

func writeUsageJSON(w http.ResponseWriter, p *Proxy, v any, errMsg string) {
	if err := p.UsageError(); err != nil {
		http.Error(w, "usage database degraded: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeAdminJSON(w, v, errMsg)
}

func writeAdminJSON(w http.ResponseWriter, v any, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, errMsg, http.StatusInternalServerError)
	}
}

// ServeHTTP handles both HTTP forward proxy requests and HTTPS CONNECT tunnels.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleHTTP(w, r)
}

func splitHostPortDefault(hostPort string, defaultPort int) (string, int, error) {
	if strings.Contains(hostPort, ":") {
		h, pStr, err := net.SplitHostPort(hostPort)
		if err != nil {
			return "", 0, err
		}
		port, err := strconv.Atoi(pStr)
		if err != nil || port <= 0 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port %q", pStr)
		}
		return strings.ToLower(h), port, nil
	}
	return strings.ToLower(hostPort), defaultPort, nil
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	hostPort := r.URL.Host
	if hostPort == "" {
		hostPort = r.Host
	}
	host, port, err := splitHostPortDefault(hostPort, 80)
	if err != nil || host == "" {
		http.Error(w, "bad proxy target", http.StatusBadRequest)
		return
	}
	rule, ok := p.cfg.MatchHostPort(host, port)
	if !ok {
		p.record(r.Context(), AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Status: http.StatusForbidden, Reason: "host/port not allowed",
		})
		http.Error(w, "egress denied by policy", http.StatusForbidden)
		return
	}
	if !rule.AllowsRequest(r.Method, r.URL.Path) {
		p.record(r.Context(), AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Rule: rule.Name, Status: http.StatusForbidden, Reason: "method/path not allowed",
		})
		http.Error(w, "egress method or path denied by policy", http.StatusForbidden)
		return
	}

	scheme := r.URL.Scheme
	if scheme == "" {
		scheme = "http"
	}
	fr := forwardReq{
		ctx: r.Context(), req: r, host: host, port: port,
		hostPort: net.JoinHostPort(host, strconv.Itoa(port)),
		scheme:   scheme, mode: "http", clientAddr: r.RemoteAddr,
		rule: rule, started: started,
	}
	resp, bindings, err := p.roundTripWithFailover(fr)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "egress blocked:") {
			status = http.StatusForbidden
		}
		p.record(r.Context(), AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Rule: rule.Name, Status: status, Reason: err.Error(),
			DurationMS: time.Since(started).Milliseconds(),
		})
		http.Error(w, err.Error(), status)
		return
	}
	defer resp.Body.Close()

	tok, scrubbed, err := p.inspectResponse(resp, bindings)
	if err != nil {
		http.Error(w, "failed reading upstream response", http.StatusBadGateway)
		return
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	p.recordTelemetry(r.Context(), AuditEvent{
		Action: "ALLOW", Mode: "http", Client: r.RemoteAddr,
		Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
		Rule: rule.Name, Status: resp.StatusCode,
		SecretsInjected: bindingSecrets(bindings), Account: primaryAccountLabel(bindings),
		Scrubbed:   scrubbed,
		DurationMS: time.Since(started).Milliseconds(),
	}, bindings, tok)
}

func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	host, port, err := splitHostPortDefault(r.Host, 443)
	if err != nil || host == "" {
		http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
		return
	}
	rule, ok := p.cfg.MatchHostPort(host, port)
	if !ok {
		p.record(r.Context(), AuditEvent{
			Action: "DENY", Mode: "tunnel", Client: r.RemoteAddr,
			Method: http.MethodConnect, Host: host, Port: port,
			Status: http.StatusForbidden, Reason: "host/port not allowed",
		})
		http.Error(w, "egress denied by policy", http.StatusForbidden)
		return
	}

	if rule.Mode == ModeMITM {
		p.handleMITM(w, r, host, port, rule)
		return
	}

	// Pure CONNECT tunnel mode.
	upstream, err := p.dialer.DialContext(r.Context(), "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "egress blocked:") {
			status = http.StatusForbidden
		}
		p.record(r.Context(), AuditEvent{
			Action: "DENY", Mode: "tunnel", Client: r.RemoteAddr,
			Method: http.MethodConnect, Host: host, Port: port,
			Rule: rule.Name, Status: status, Reason: err.Error(),
			DurationMS: time.Since(started).Milliseconds(),
		})
		http.Error(w, err.Error(), status)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hj.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	defer clientConn.Close()
	defer upstream.Close()

	if _, err := io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	p.record(r.Context(), AuditEvent{
		Action: "ALLOW", Mode: "tunnel", Client: r.RemoteAddr,
		Method: http.MethodConnect, Host: host, Port: port,
		Rule: rule.Name, Status: http.StatusOK,
		DurationMS: time.Since(started).Milliseconds(),
	})

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, clientConn); done <- struct{}{} }()
	go func() { _, _ = io.Copy(clientConn, upstream); done <- struct{}{} }()
	<-done
}

func (p *Proxy) handleMITM(w http.ResponseWriter, r *http.Request, host string, port int, rule *Rule) {
	if p.ca == nil {
		http.Error(w, "MITM CA not configured", http.StatusInternalServerError)
		return
	}
	leaf, err := p.ca.LeafCert(host)
	if err != nil {
		http.Error(w, "failed signing leaf cert", http.StatusInternalServerError)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	rawConn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer rawConn.Close()

	if _, err := io.WriteString(rawConn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	tlsConn := tls.Server(rawConn, &tls.Config{
		Certificates: []tls.Certificate{*leaf},
		NextProtos:   []string{"http/1.1"},
	})
	if err := tlsConn.HandshakeContext(r.Context()); err != nil {
		return
	}
	defer tlsConn.Close()

	br := bufio.NewReader(tlsConn)
	hostPort := net.JoinHostPort(host, strconv.Itoa(port))

	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				p.record(r.Context(), AuditEvent{
					Action: "DENY", Mode: "mitm", Client: r.RemoteAddr,
					Host: host, Port: port, Rule: rule.Name, Reason: err.Error(),
				})
			}
			return
		}
		started := time.Now()
		if !rule.AllowsRequest(req.Method, req.URL.Path) {
			p.record(r.Context(), AuditEvent{
				Action: "DENY", Mode: "mitm", Client: r.RemoteAddr,
				Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
				Rule: rule.Name, Status: http.StatusForbidden, Reason: "method/path not allowed",
			})
			_ = writeSimpleResponse(tlsConn, req, http.StatusForbidden, "egress method or path denied by policy\n")
			_ = req.Body.Close()
			if req.Close {
				return
			}
			continue
		}

		if own, ok := rule.localReply(req); ok {
			p.record(r.Context(), AuditEvent{
				Action: own.action, Mode: "mitm", Client: r.RemoteAddr,
				Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
				Rule: rule.Name, Status: own.status, Reason: own.reason,
			})
			werr := writeResponse(tlsConn, req, own.status, own.contentType, own.body)
			cerr := req.Body.Close()
			if werr != nil || cerr != nil || req.Close {
				return
			}
			continue
		}

		fr := forwardReq{
			ctx: r.Context(), req: req, host: host, port: port,
			hostPort: hostPort, scheme: "https", mode: "mitm",
			clientAddr: r.RemoteAddr, rule: rule, started: started,
		}
		resp, bindings, err := p.roundTripWithFailover(fr)
		_ = req.Body.Close()
		if err != nil {
			status := http.StatusBadGateway
			if strings.Contains(err.Error(), "egress blocked:") {
				status = http.StatusForbidden
			}
			p.record(r.Context(), AuditEvent{
				Action: "DENY", Mode: "mitm", Client: r.RemoteAddr,
				Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
				Rule: rule.Name, Status: status, Reason: err.Error(),
				DurationMS: time.Since(started).Milliseconds(),
			})
			_ = writeSimpleResponse(tlsConn, req, status, err.Error()+"\n")
			return
		}

		tok, scrubbed, err := p.inspectResponse(resp, bindings)
		if err != nil {
			drainResponseBody(resp)
			if werr := writeSimpleResponse(tlsConn, req, http.StatusBadGateway, "failed reading upstream response\n"); werr != nil {
				return
			}
			return
		}

		p.recordTelemetry(r.Context(), AuditEvent{
			Action: "ALLOW", Mode: "mitm", Client: r.RemoteAddr,
			Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
			Rule: rule.Name, Status: resp.StatusCode,
			SecretsInjected: bindingSecrets(bindings), Account: primaryAccountLabel(bindings),
			Scrubbed:   scrubbed,
			DurationMS: time.Since(started).Milliseconds(),
		}, bindings, tok)

		err = resp.Write(tlsConn)
		_ = resp.Body.Close()
		if err != nil || req.Close || resp.Close {
			return
		}
	}
}

// localReply is a response the proxy gives itself, and how to audit it.
type localReply struct {
	action, reason, contentType, body string
	status                            int
}

// localReply is the proxy's reply to a request it does not forward. A WebSocket
// upgrade cannot pass, since the Upgrade header is stripped on the way out; 426
// tells Codex to use HTTP at once instead of retrying.
func (r *Rule) localReply(req *http.Request) (localReply, bool) {
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return localReply{
			action: "DENY", reason: "websocket not supported", status: http.StatusUpgradeRequired,
			contentType: "text/plain; charset=utf-8", body: "egress proxy carries no websockets; use HTTP\n",
		}, true
	}
	body, ok := r.answer(req.URL.Path)
	return localReply{
		action: "ALLOW", reason: "answered by the proxy", status: http.StatusOK,
		contentType: "application/json", body: body,
	}, ok
}

type forwardReq struct {
	ctx        context.Context
	req        *http.Request
	host       string
	port       int
	hostPort   string
	scheme     string
	mode       string
	clientAddr string
	rule       *Rule
	started    time.Time
}

func (p *Proxy) inspectResponse(resp *http.Response, bindings []injectedBinding) (tok TokenMetrics, scrubbed bool, err error) {
	for _, b := range bindings {
		p.quota.observeHeaders(b.secret, b.account, b.label, resp.Header)
	}
	wantScrub := p.cfg.ShouldScrubResponses()
	wantTokens := len(bindings) > 0 && resp.StatusCode < http.StatusBadRequest
	if !wantScrub && !wantTokens {
		return TokenMetrics{}, false, nil
	}
	body, scrubbed, err := p.secrets.Load().scrubAndCaptureResponse(resp, wantScrub)
	if err != nil {
		return TokenMetrics{}, scrubbed, err
	}
	if wantTokens && len(body) > 0 {
		tok = extractTokenMetrics(resp.Header.Get("Content-Type"), body)
	}
	return tok, scrubbed, nil
}

func (p *Proxy) buildOutboundRequest(fr forwardReq) *http.Request {
	outReq := fr.req.Clone(fr.ctx)
	outReq.RequestURI = ""
	outReq.URL.Scheme = fr.scheme
	outReq.URL.Host = fr.hostPort
	if outReq.Host == "" {
		outReq.Host = fr.host
	}
	stripHopHeaders(outReq.Header)
	if p.cfg.ShouldScrubResponses() {
		// Prevent compressed reflections from bypassing plaintext scrubbing.
		outReq.Header.Del("Accept-Encoding")
	}
	return outReq
}

func (p *Proxy) roundTripWithFailover(fr forwardReq) (*http.Response, []injectedBinding, error) {
	engine := p.secrets.Load()
	pooled, isPooled := engine.PooledSecretFor(fr.req, fr.rule.Secrets)
	if !isPooled {
		return p.roundTripSingle(fr, engine)
	}
	candidates := p.pool.candidates(pooled.name, pooled.accounts)
	bodyBytes, overflow, rest, err := readBoundedBody(fr.req.Body, p.maxRetryBodyBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read request body: %w", err)
	}
	if overflow {
		return p.roundTripOverflow(fr, engine, pooled.name, candidates[0], io.MultiReader(bytes.NewReader(bodyBytes), rest))
	}
	return p.roundTripPool(fr, engine, pooled.name, candidates, bodyBytes)
}

func (p *Proxy) roundTripSingle(fr forwardReq, engine *SecretEngine) (*http.Response, []injectedBinding, error) {
	outReq := p.buildOutboundRequest(fr)
	bindings := engine.injectRequestSelected(outReq, fr.rule.Secrets, nil)
	resp, err := p.tr.RoundTrip(outReq)
	if err != nil {
		return nil, bindings, fmt.Errorf("upstream round trip: %w", err)
	}
	return resp, bindings, nil
}

func (p *Proxy) roundTripOverflow(fr forwardReq, engine *SecretEngine, secretName string, cand resolvedAccount, body io.Reader) (*http.Response, []injectedBinding, error) {
	outReq := p.buildOutboundRequest(fr)
	outReq.Body = io.NopCloser(body)
	bindings := engine.injectRequestSelected(outReq, fr.rule.Secrets, map[string]resolvedAccount{secretName: cand})
	resp, err := p.tr.RoundTrip(outReq)
	if err != nil {
		return nil, bindings, fmt.Errorf("upstream round trip: %w", err)
	}
	p.updatePoolOutcome(secretName, cand, resp)
	return resp, bindings, nil
}

func (p *Proxy) roundTripPool(fr forwardReq, engine *SecretEngine, secretName string, candidates []resolvedAccount, bodyBytes []byte) (*http.Response, []injectedBinding, error) {
	for i, cand := range candidates {
		outReq := p.buildOutboundRequest(fr)
		setBufferedBody(outReq, bodyBytes)
		bindings := engine.injectRequestSelected(outReq, fr.rule.Secrets, map[string]resolvedAccount{secretName: cand})
		resp, err := p.tr.RoundTrip(outReq)
		if err != nil {
			return nil, bindings, fmt.Errorf("upstream round trip: %w", err)
		}
		p.updatePoolOutcome(secretName, cand, resp)
		if !shouldFailover(resp.StatusCode) || i+1 >= len(candidates) {
			return resp, bindings, nil
		}
		p.recordFailoverAttempt(fr, bindings, cand, resp)
		drainResponseBody(resp)
	}
	return nil, nil, errors.New("no accounts available in pool")
}

func (p *Proxy) updatePoolOutcome(secretName string, cand resolvedAccount, resp *http.Response) {
	if shouldFailover(resp.StatusCode) {
		p.pool.markFailure(secretName, cand, resp.StatusCode, resp.Header)
		return
	}
	if resp.StatusCode < http.StatusBadRequest {
		p.pool.markSuccess(secretName, cand)
	}
}

func (p *Proxy) recordFailoverAttempt(fr forwardReq, bindings []injectedBinding, cand resolvedAccount, resp *http.Response) {
	for _, b := range bindings {
		p.quota.observeHeaders(b.secret, b.account, b.label, resp.Header)
	}
	p.recordTelemetry(fr.ctx, AuditEvent{
		Action: "ALLOW", Mode: fr.mode, Client: fr.clientAddr,
		Method: fr.req.Method, Host: fr.host, Port: fr.port, Path: fr.req.URL.Path,
		Rule: fr.rule.Name, Status: resp.StatusCode,
		SecretsInjected: bindingSecrets(bindings), Account: cand.label,
		Failover:   true,
		DurationMS: time.Since(fr.started).Milliseconds(),
	}, bindings, TokenMetrics{})
}

func setBufferedBody(req *http.Request, bodyBytes []byte) {
	if len(bodyBytes) > 0 {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.ContentLength = int64(len(bodyBytes))
		return
	}
	req.Body = http.NoBody
	req.ContentLength = 0
}

func drainResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		if cerr := resp.Body.Close(); cerr != nil {
			return
		}
		return
	}
	if err := resp.Body.Close(); err != nil {
		return
	}
}

func readBoundedBody(body io.ReadCloser, limit int64) (buf []byte, overflow bool, rest io.Reader, err error) {
	if body == nil || body == http.NoBody {
		return nil, false, nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > limit {
		return data, true, body, nil
	}
	return data, false, nil, nil
}

func bindingSecrets(bindings []injectedBinding) []string {
	if len(bindings) == 0 {
		return nil
	}
	out := make([]string, len(bindings))
	for i, b := range bindings {
		out[i] = b.secret
	}
	return out
}

func primaryAccountLabel(bindings []injectedBinding) string {
	if len(bindings) == 1 {
		return bindings[0].label
	}
	return ""
}

func writeSimpleResponse(w io.Writer, req *http.Request, status int, msg string) error {
	return writeResponse(w, req, status, "text/plain; charset=utf-8", msg)
}

func writeResponse(w io.Writer, req *http.Request, status int, contentType, msg string) error {
	resp := &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(strings.NewReader(msg)),
		ContentLength: int64(len(msg)),
		Request:       req,
	}
	return resp.Write(w)
}

var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func stripHopHeaders(h http.Header) {
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
