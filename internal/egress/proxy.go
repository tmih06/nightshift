package egress

import (
	"bufio"
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
	Scrubbed        bool      `json:"scrubbed,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
}

// Options tunes a Proxy instance (all fields optional).
type Options struct {
	CA               *CA
	Getenv           func(string) string
	UpstreamTLS      *tls.Config // optional custom upstream TLS config (used in tests)
	AuditWriter      io.Writer   // optional JSONL audit sink
	EventRingMax     int
	SkipAutoCA       bool
	PlainHTTPDefault bool
}

// Proxy is the HTTP/HTTPS egress proxy handler.
type Proxy struct {
	cfg     Config
	ca      *CA
	dialer  *SafeDialer
	secrets atomic.Pointer[SecretEngine]
	getenv  func(string) string
	tr      *http.Transport

	auditMu sync.Mutex
	auditW  io.Writer
	ring    []AuditEvent
	ringMax int
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

	ca := opts.CA
	if ca == nil && !opts.SkipAutoCA {
		if cfg.CA.CertFile != "" && cfg.CA.KeyFile != "" {
			ca, err = LoadCA(cfg.CA.CertFile, cfg.CA.KeyFile)
			if err != nil {
				return nil, err
			}
		} else if needsMITM(cfg) {
			ca, _, _, err = GenerateCA("Nightshift Egress Proxy CA")
			if err != nil {
				return nil, err
			}
		}
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

	p := &Proxy{
		cfg:     cfg,
		ca:      ca,
		dialer:  dialer,
		getenv:  opts.Getenv,
		tr:      tr,
		auditW:  opts.AuditWriter,
		ringMax: ringMax,
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

// Events returns a snapshot of the most recent audit events.
func (p *Proxy) Events() []AuditEvent {
	p.auditMu.Lock()
	defer p.auditMu.Unlock()
	out := make([]AuditEvent, len(p.ring))
	copy(out, p.ring)
	return out
}

func (p *Proxy) record(ev AuditEvent) {
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	p.auditMu.Lock()
	p.ring = append(p.ring, ev)
	if len(p.ring) > p.ringMax {
		p.ring = p.ring[len(p.ring)-p.ringMax:]
	}
	w := p.auditW
	p.auditMu.Unlock()
	if w != nil {
		if b, err := json.Marshal(ev); err == nil {
			_, _ = w.Write(append(b, '\n'))
		}
	}
}

// AdminHandler exposes health, public CA cert, and recent audit events on a
// separate admin port that sandbox pods cannot reach.
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
	mux.HandleFunc("/events", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.Events())
	})
	return mux
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
		p.record(AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Status: http.StatusForbidden, Reason: "host/port not allowed",
		})
		http.Error(w, "egress denied by policy", http.StatusForbidden)
		return
	}
	if !rule.AllowsRequest(r.Method, r.URL.Path) {
		p.record(AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Rule: rule.Name, Status: http.StatusForbidden, Reason: "method/path not allowed",
		})
		http.Error(w, "egress method or path denied by policy", http.StatusForbidden)
		return
	}

	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}
	outReq.URL.Host = net.JoinHostPort(host, strconv.Itoa(port))
	stripHopHeaders(outReq.Header)
	injected := p.secrets.Load().InjectRequest(outReq, rule.Secrets)
	if p.cfg.ShouldScrubResponses() {
		// Prevent compressed reflections from bypassing plaintext scrubbing.
		outReq.Header.Del("Accept-Encoding")
	}

	resp, err := p.tr.RoundTrip(outReq)
	if err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "egress blocked:") {
			status = http.StatusForbidden
		}
		p.record(AuditEvent{
			Action: "DENY", Mode: "http", Client: r.RemoteAddr,
			Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
			Rule: rule.Name, Status: status, Reason: err.Error(),
			DurationMS: time.Since(started).Milliseconds(),
		})
		http.Error(w, err.Error(), status)
		return
	}
	defer resp.Body.Close()

	var scrubbed bool
	if p.cfg.ShouldScrubResponses() {
		scrubbed, err = p.secrets.Load().ScrubResponse(resp)
		if err != nil {
			http.Error(w, "failed reading upstream response", http.StatusBadGateway)
			return
		}
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	p.record(AuditEvent{
		Action: "ALLOW", Mode: "http", Client: r.RemoteAddr,
		Method: r.Method, Host: host, Port: port, Path: r.URL.Path,
		Rule: rule.Name, Status: resp.StatusCode,
		SecretsInjected: injected, Scrubbed: scrubbed,
		DurationMS: time.Since(started).Milliseconds(),
	})
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
		p.record(AuditEvent{
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
		p.record(AuditEvent{
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

	p.record(AuditEvent{
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
				p.record(AuditEvent{
					Action: "DENY", Mode: "mitm", Client: r.RemoteAddr,
					Host: host, Port: port, Rule: rule.Name, Reason: err.Error(),
				})
			}
			return
		}
		started := time.Now()
		if !rule.AllowsRequest(req.Method, req.URL.Path) {
			p.record(AuditEvent{
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

		outReq := req.Clone(context.Background())
		outReq.RequestURI = ""
		outReq.URL.Scheme = "https"
		outReq.URL.Host = hostPort
		if outReq.Host == "" {
			outReq.Host = host
		}
		stripHopHeaders(outReq.Header)
		injected := p.secrets.Load().InjectRequest(outReq, rule.Secrets)
		if p.cfg.ShouldScrubResponses() {
			outReq.Header.Del("Accept-Encoding")
		}

		resp, err := p.tr.RoundTrip(outReq)
		_ = req.Body.Close()
		if err != nil {
			status := http.StatusBadGateway
			if strings.Contains(err.Error(), "egress blocked:") {
				status = http.StatusForbidden
			}
			p.record(AuditEvent{
				Action: "DENY", Mode: "mitm", Client: r.RemoteAddr,
				Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
				Rule: rule.Name, Status: status, Reason: err.Error(),
				DurationMS: time.Since(started).Milliseconds(),
			})
			_ = writeSimpleResponse(tlsConn, req, status, err.Error()+"\n")
			return
		}

		var scrubbed bool
		if p.cfg.ShouldScrubResponses() {
			scrubbed, err = p.secrets.Load().ScrubResponse(resp)
			if err != nil {
				_ = resp.Body.Close()
				_ = writeSimpleResponse(tlsConn, req, http.StatusBadGateway, "failed reading upstream response\n")
				return
			}
		}

		p.record(AuditEvent{
			Action: "ALLOW", Mode: "mitm", Client: r.RemoteAddr,
			Method: req.Method, Host: host, Port: port, Path: req.URL.Path,
			Rule: rule.Name, Status: resp.StatusCode,
			SecretsInjected: injected, Scrubbed: scrubbed,
			DurationMS: time.Since(started).Milliseconds(),
		})

		err = resp.Write(tlsConn)
		_ = resp.Body.Close()
		if err != nil || req.Close || resp.Close {
			return
		}
	}
}

func writeSimpleResponse(w io.Writer, req *http.Request, status int, msg string) error {
	resp := &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
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
