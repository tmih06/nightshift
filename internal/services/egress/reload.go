package egress

import (
	"context"
	"fmt"
	"time"
)

// ReloadSecrets re-reads every secret binding and swaps the new engine in
// when any value changed. File-backed bindings let a refresher replace a
// short-lived OAuth access token without restarting the proxy. It reports
// whether the loaded secrets changed.
func (p *Proxy) ReloadSecrets() (bool, error) {
	next, err := NewSecretEngine(p.cfg.Secrets, p.getenv)
	if err != nil {
		return false, fmt.Errorf("reload secrets: %w", err)
	}
	if next.signature() == p.secrets.Load().signature() {
		return false, nil
	}
	p.secrets.Store(next)
	p.pool.onReload(next)
	return true, nil
}

// WatchSecrets reloads secrets every interval until ctx ends. It calls
// onChange (when non-nil) after each reload that changed anything, and
// onError for a failed reload; the previous secrets stay active on error.
func (p *Proxy) WatchSecrets(ctx context.Context, interval time.Duration, onChange func(), onError func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := p.ReloadSecrets()
			switch {
			case err != nil && onError != nil:
				onError(err)
			case changed && onChange != nil:
				onChange()
			}
		}
	}
}
