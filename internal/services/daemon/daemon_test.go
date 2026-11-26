package daemon

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The API can open a shell in a sandbox. Serving it in the clear on an address
// anyone can reach must be something a person says, not something that
// happens.
func TestPlainHTTPOnAReachableAddressNeedsSayingSo(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"loopback", Config{Listen: "127.0.0.1:0"}, true},
		{"loopback name", Config{Listen: "localhost:0"}, true},
		{"v6 loopback", Config{Listen: "[::1]:0"}, true},
		{"every interface", Config{Listen: ":7777"}, false},
		{"a public address", Config{Listen: "10.1.2.3:7777"}, false},
		{"every interface, said so", Config{Listen: ":7777", PlainHTTP: true}, true},
		{"every interface, with TLS", Config{Listen: ":7777", TLSCert: "c.pem", TLSKey: "k.pem"}, true},
		{"a certificate without its key", Config{Listen: "127.0.0.1:0", TLSCert: "c.pem"}, false},
		{"no port", Config{Listen: "127.0.0.1"}, false},
	} {
		err := tc.cfg.check()
		if (err == nil) != tc.ok {
			t.Errorf("%s: check() = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestATokenIsMadeWhenNoneIsGiven(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 32 || a == b || strings.ContainsAny(a, " \n") {
		t.Errorf("tokens %q and %q are not distinct, long, plain strings", a, b)
	}
}

// The daemon answers only to its token, and says nothing of the cluster on
// the liveness check.
func TestADaemonServesOnlyWithItsToken(t *testing.T) {
	d, err := Start(Config{Listen: "127.0.0.1:0", NoAccounts: true, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.Close(ctx); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if d.Token() == "" || !strings.HasPrefix(d.URL(), "http://127.0.0.1:") {
		t.Fatalf("token %q url %q", d.Token(), d.URL())
	}
	get := func(path, token string) int {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, d.URL()+path, http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			t.Fatal(err)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	if got := get("/v1/healthz", ""); got != http.StatusOK {
		t.Errorf("healthz = %d", got)
	}
	if got := get("/v1", ""); got != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", got)
	}
	if got := get("/v1", "wrong"); got != http.StatusUnauthorized {
		t.Errorf("a wrong token = %d, want 401", got)
	}
	if got := get("/v1", d.Token()); got != http.StatusOK {
		t.Errorf("the token = %d, want 200", got)
	}
	if got := get("/v1/accounts", d.Token()); got != http.StatusNotImplemented {
		t.Errorf("a daemon without accounts answers %d, want 501", got)
	}
}
