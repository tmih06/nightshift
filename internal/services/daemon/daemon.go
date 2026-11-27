// Package daemon runs the Nightshift services: the API server over the
// cluster-backed backend, on a listener of its own. cmd/nightshiftd is a thin
// shell around it.
package daemon

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"charm.land/log/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/accounts"
	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/live"
	"github.com/tmih06/nightshift/internal/services/server"
)

// Config is how a daemon is run.
type Config struct {
	// Listen is the address to serve on. Port 0 picks a free one.
	Listen string
	// Token is the bearer token. Empty makes a random one.
	Token string
	// TLSCert and TLSKey serve HTTPS.
	TLSCert, TLSKey string
	// PlainHTTP allows plain HTTP on an address that is not loopback: the
	// caller says the traffic is protected some other way, such as a
	// ClusterIP Service reached through kubectl port-forward, or an ingress
	// that terminates TLS.
	PlainHTTP bool
	// Hosts are Host header names, besides loopback, to accept.
	Hosts []string
	// AnyHost accepts every Host header.
	AnyHost bool
	// AuthURL and ProxyURL are where nightshift-auth and the egress proxy's
	// admin port are reached. Empty means a kubectl port-forward to each,
	// which is how a daemon on a laptop reaches them.
	AuthURL, ProxyURL string
	// NoAccounts leaves the accounts endpoints out.
	NoAccounts bool
	// Version is the build, for bug reports.
	Version string
}

// Daemon is a running server. It owns the listener, the goroutine serving it,
// and everything the backend runs.
type Daemon struct {
	token   string
	url     string
	srv     *http.Server
	backend *live.Backend
	done    chan error
}

// Start serves the API and returns once it is listening.
func Start(cfg Config) (*Daemon, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	token := cfg.Token
	if token == "" {
		var err error
		if token, err = NewToken(); err != nil {
			return nil, err
		}
	}
	backend := live.New(live.Options{Accounts: cfg.accounts()})
	s, err := server.New(server.Config{
		Token: token, Hosts: cfg.Hosts, AnyHost: cfg.AnyHost, Version: cfg.Version,
		Cluster: v1.ClusterInfo{Context: cluster.ContextLabel(), Namespace: cluster.Namespace},
		Fleet:   backend.Fleet, Claims: backend.Claims, Checkouts: backend.Checkouts, Volumes: backend.Volumes,
		Terminals: backend.Terminals, Accounts: accountsOf(backend), Runs: backend.Runs,
	})
	if err != nil {
		return nil, fmt.Errorf("start the server: %w", err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	scheme := "http"
	d := &Daemon{token: token, backend: backend, done: make(chan error, 1)}
	d.srv = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if cfg.TLSCert != "" {
		scheme = "https"
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("load TLS key pair: %w", err), ln.Close())
		}
		d.srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		ln = tls.NewListener(ln, d.srv.TLSConfig)
	}
	d.url = scheme + "://" + ln.Addr().String()
	go func() {
		err := d.srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		d.done <- err
	}()
	log.Info("nightshiftd is serving", "url", d.url, "cluster", cluster.ContextLabel())
	return d, nil
}

// accountsOf is the backend's accounts, or nil when it has none, so that the
// server leaves the endpoints out instead of holding a typed nil.
func accountsOf(b *live.Backend) server.Accounts {
	if b.Accounts == nil {
		return nil
	}
	return b.Accounts
}

func (c Config) accounts() accounts.Service {
	if c.NoAccounts {
		return nil
	}
	l := accounts.NewLive()
	if c.AuthURL != "" {
		l.Auth = accounts.Static(c.AuthURL)
	}
	if c.ProxyURL != "" {
		l.Proxy = accounts.Static(c.ProxyURL)
	}
	return l
}

// check refuses a configuration that would expose the API unprotected.
func (c Config) check() error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", c.Listen, err)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("TLS needs both a certificate and a key")
	}
	if !loopback(host) && c.TLSCert == "" && !c.PlainHTTP {
		return fmt.Errorf("refusing to serve plain HTTP on %s: the API can open a shell in a sandbox. Give a TLS certificate and key, or say the traffic is protected another way with --plain-http", c.Listen)
	}
	return nil
}

// loopback is whether host only accepts connections from this machine.
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ResumeRuns takes up the runs the cluster remembers, so their results are
// read without a request having to ask first. The program calls it, not Start,
// as it changes the records in whatever cluster kubectl points at.
func (d *Daemon) ResumeRuns() { d.backend.Runs.Resume() }

// URL is where the daemon can be reached.
func (d *Daemon) URL() string { return d.url }

// Token is the bearer token requests must carry.
func (d *Daemon) Token() string { return d.token }

// Wait blocks until the daemon stops, and returns why.
func (d *Daemon) Wait() error { return <-d.done }

// Close stops serving and what the backend runs, waiting until ctx ends.
func (d *Daemon) Close(ctx context.Context) error {
	err := d.srv.Shutdown(ctx)
	if err != nil {
		err = errors.Join(fmt.Errorf("shut down: %w", err), d.srv.Close())
	}
	d.backend.Close(ctx)
	return err
}

// NewToken makes a random bearer token.
func NewToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make a token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
