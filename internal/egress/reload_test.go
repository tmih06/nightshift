package egress

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func injected(t *testing.T, p *Proxy, placeholder string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example.test/", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+placeholder)
	p.secrets.Load().InjectRequest(req, []string{"TOKEN"})
	return req.Header.Get("Authorization")
}

func TestReloadSecretsSwapsRotatedFileValue(t *testing.T) {
	const placeholder = "nightshift:secret:TOKEN"
	file := filepath.Join(t.TempDir(), "REAL_TOKEN")
	if err := os.WriteFile(file, []byte("first-real-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Rules: []Rule{{Name: "api", Hosts: []string{"api.example.test"}, Ports: []int{443}, Mode: ModeTunnel}},
		Secrets: map[string]SecretBinding{
			"TOKEN": {Placeholder: placeholder, File: file},
		},
	}
	p, err := NewProxy(cfg, Options{SkipAutoCA: true})
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}

	if got, want := injected(t, p, placeholder), "Bearer first-real-token"; got != want {
		t.Fatalf("before rotation: Authorization = %q, want %q", got, want)
	}
	if changed, err := p.ReloadSecrets(); err != nil || changed {
		t.Fatalf("reload with no change = (%v, %v), want (false, nil)", changed, err)
	}

	if err := os.WriteFile(file, []byte("second-real-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := p.ReloadSecrets()
	if err != nil || !changed {
		t.Fatalf("reload after rotation = (%v, %v), want (true, nil)", changed, err)
	}
	if got, want := injected(t, p, placeholder), "Bearer second-real-token"; got != want {
		t.Errorf("after rotation: Authorization = %q, want %q", got, want)
	}
}
