// Command egress-proxy runs Nightshift's lightweight outbound HTTP/HTTPS
// egress proxy with host/path/method allowlists, SSRF-safe dialing, selective
// TLS interception, placeholder secret injection, and response scrubbing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tmih06/nightshift/internal/egress"
)

func main() {
	var (
		listenAddr = flag.String("listen", ":8080", "HTTP/HTTPS proxy listen address")
		adminAddr  = flag.String("admin", ":9090", "Admin listen address (/healthz, /ca.crt, /events); empty disables")
		configPath = flag.String("config", "/etc/egress/policy.yaml", "Path to YAML egress policy config")
		caCertPath = flag.String("ca-cert", "", "Override CA certificate PEM path")
		caKeyPath  = flag.String("ca-key", "", "Override CA private key PEM path")
		caOutPath  = flag.String("ca-out", "", "Optional path to write the public CA certificate PEM on startup")
	)
	flag.Parse()

	cfg, err := egress.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("egress-proxy: %v", err)
	}
	if *caCertPath != "" {
		cfg.CA.CertFile = *caCertPath
	}
	if *caKeyPath != "" {
		cfg.CA.KeyFile = *caKeyPath
	}

	proxy, err := egress.NewProxy(cfg, egress.Options{
		AuditWriter: os.Stdout,
	})
	if err != nil {
		log.Fatalf("egress-proxy: %v", err)
	}

	if *caOutPath != "" && proxy.CA() != nil {
		if err := os.WriteFile(*caOutPath, proxy.CA().CertPEM(), 0o644); err != nil {
			log.Fatalf("egress-proxy: write CA cert %s: %v", *caOutPath, err)
		}
	}

	proxySrv := &http.Server{
		Addr:              *listenAddr,
		Handler:           proxy,
		ReadHeaderTimeout: 15 * time.Second,
	}

	var adminSrv *http.Server
	if *adminAddr != "" {
		adminSrv = &http.Server{
			Addr:              *adminAddr,
			Handler:           proxy.AdminHandler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			log.Printf("egress-proxy: admin listening on %s", *adminAddr)
			if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("egress-proxy: admin server: %v", err)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = proxySrv.Shutdown(shutdownCtx)
		if adminSrv != nil {
			_ = adminSrv.Shutdown(shutdownCtx)
		}
	}()

	log.Printf("egress-proxy: proxy listening on %s (%d rules, block_private_ips=%v, scrub_responses=%v)",
		*listenAddr, len(cfg.Rules), cfg.ShouldBlockPrivateIPs(), cfg.ShouldScrubResponses())
	if err := proxySrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "egress-proxy: %v\n", err)
		os.Exit(1)
	}
}
