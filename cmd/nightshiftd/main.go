// Command nightshiftd runs the Nightshift services: the API that frontends,
// such as the terminal UI in cmd/nightshift, use to drive the sandbox fleet.
// See docs/api.md for the contract it serves.
//
//	nightshiftd                              # on 127.0.0.1:7777, token from `/.config/nightshift/token
//	nightshiftd --listen :7777 --tls-cert c.pem --tls-key k.pem
//	NIGHTSHIFT_CONTEXT=my-cluster nightshiftd
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"

	"github.com/tmih06/nightshift/internal/services/daemon"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

// hostList is a repeatable --allow-host flag.
type hostList []string

func (h *hostList) String() string     { return strings.Join(*h, ",") }
func (h *hostList) Set(v string) error { *h = append(*h, v); return nil }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "nightshiftd:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		cfg       daemon.Config
		hosts     hostList
		tokenFile = flag.String("token-file", defaultTokenFile(), "file holding the bearer token; made if missing. NIGHTSHIFT_TOKEN overrides it")
		showVer   = flag.Bool("version", false, "print the version and exit")
		debug     = flag.Bool("debug", false, "log every step")
	)
	flag.StringVar(&cfg.Listen, "listen", "127.0.0.1:7777", "address to serve on")
	flag.StringVar(&cfg.TLSCert, "tls-cert", "", "TLS certificate, to serve HTTPS")
	flag.StringVar(&cfg.TLSKey, "tls-key", "", "TLS key, to serve HTTPS")
	flag.BoolVar(&cfg.PlainHTTP, "plain-http", false, "allow plain HTTP on a non-loopback address: something else protects the traffic (kubectl port-forward, or an ingress that terminates TLS)")
	flag.Var(&hosts, "allow-host", "a Host header name to accept besides loopback (repeatable)")
	flag.BoolVar(&cfg.AnyHost, "any-host", false, "accept every Host header; the token is still required")
	flag.StringVar(&cfg.AuthURL, "auth-url", "", "where nightshift-auth is reached; empty uses kubectl port-forward")
	flag.StringVar(&cfg.ProxyURL, "proxy-url", "", "where the egress proxy's admin port is reached; empty uses kubectl port-forward")
	flag.BoolVar(&cfg.NoAccounts, "no-accounts", false, "leave the accounts endpoints out")
	flag.Parse()
	if *showVer {
		if _, err := fmt.Fprintln(os.Stdout, version); err != nil {
			return fmt.Errorf("print the version: %w", err)
		}
		return nil
	}
	if *debug {
		log.SetLevel(log.DebugLevel)
	}
	cfg.Hosts, cfg.Version = hosts, version

	var err error
	if cfg.Token, err = loadToken(*tokenFile); err != nil {
		return err
	}
	d, err := start(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := d.Close(shutdown); err != nil {
			log.Error("shutdown", "err", err)
		}
	}()
	if err := d.Wait(); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// start serves the API and takes up the runs the cluster remembers.
func start(cfg daemon.Config) (*daemon.Daemon, error) {
	d, err := daemon.Start(cfg)
	if err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	d.ResumeRuns()
	return d, nil
}

func defaultTokenFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "nightshift", "token")
}

// loadToken is the token from NIGHTSHIFT_TOKEN, else the token file, which it
// makes (readable by its owner only) when it does not exist yet.
func loadToken(path string) (string, error) {
	if t := strings.TrimSpace(os.Getenv("NIGHTSHIFT_TOKEN")); t != "" {
		return t, nil
	}
	if path == "" {
		return "", errors.New("no token: set NIGHTSHIFT_TOKEN or pass --token-file")
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err == nil {
		if t := strings.TrimSpace(string(raw)); t != "" {
			return t, nil
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("load token: %w", err)
	}
	t, err := daemon.NewToken()
	if err != nil {
		return "", fmt.Errorf("load token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("load token: %w", err)
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("load token: %w", err)
	}
	log.Info("made a token", "file", path)
	return t, nil
}
