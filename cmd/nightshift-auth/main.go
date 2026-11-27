// Command nightshift-auth signs coding-agent accounts in, keeps their tokens
// fresh, and publishes them to the cluster: real tokens to the egress proxy,
// placeholder logins to sandboxes. nightshiftd's Accounts endpoints drive it;
// it serves nothing else. See internal/services/auth.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/log/v2"

	"github.com/tmih06/nightshift/internal/services/auth"
	"github.com/tmih06/nightshift/internal/services/auth/providers"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("nightshift-auth", "err", err)
	}
}

func run() error {
	listen := flag.String("listen", ":8318", "address to serve the API on")
	data := flag.String("data", "/data", "directory of the vault, private to this process")
	sandboxes := flag.String("namespace", "nightshift", "namespace of agent-creds and the warm pools")
	egressNS := flag.String("egress-namespace", "nightshift-egress", "namespace of egress-secrets")
	every := flag.Duration("tick", time.Minute, "how often to look for tokens to renew")
	flag.Parse()

	key := strings.TrimSpace(os.Getenv("NIGHTSHIFT_AUTH_KEY"))
	if key == "" {
		return errors.New("NIGHTSHIFT_AUTH_KEY is unset: the API would be open")
	}
	store, err := auth.NewStore(*data)
	if err != nil {
		return fmt.Errorf("open the vault: %w", err)
	}
	cluster, err := auth.NewInCluster()
	if err != nil {
		return fmt.Errorf("reach the cluster: %w", err)
	}
	svc, err := auth.New(store, providers.All(nil), cluster, auth.Config{
		Namespaces: auth.Namespaces{Sandboxes: *sandboxes, Egress: *egressNS},
	})
	if err != nil {
		return fmt.Errorf("load the vault: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var wg sync.WaitGroup
	wg.Go(func() { svc.Run(ctx, *every) })
	defer wg.Wait()

	srv := &http.Server{Addr: *listen, Handler: svc.Handler(key), ReadHeaderTimeout: 10 * time.Second}
	wg.Go(func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			log.Warn("nightshift-auth: shut down", "err", err)
		}
	})
	log.Info("nightshift-auth: serving", "listen", *listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		stop()
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
