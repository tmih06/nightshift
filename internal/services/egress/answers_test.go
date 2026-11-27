package egress

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestProxyAnswersAListedPathWithoutForwardingIt(t *testing.T) {
	var reached atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		if _, err := io.WriteString(w, `{"email":"real@example.com"}`); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	u, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	ca, _, _, err := GenerateCA("Test Proxy CA")
	if err != nil {
		t.Fatal(err)
	}
	open := false
	trusted := x509.NewCertPool()
	trusted.AddCert(upstream.Certificate())
	proxy, err := NewProxy(Config{
		BlockPrivateIPs: &open,
		Rules: []Rule{{
			Name: "userinfo", Hosts: []string{host}, Ports: []int{port}, Mode: ModeMITM,
			PathPrefixes: []string{"/userinfo"}, Answers: map[string]string{"/userinfo": "{}"},
		}},
	}, Options{CA: ca, UpstreamTLS: &tls.Config{RootCAs: trusted, MinVersion: tls.VersionTLS12}})
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(proxy)
	defer front.Close()
	proxyURL, err := url.Parse(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca.CertPEM()) {
		t.Fatal("no proxy CA")
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
	}}
	defer client.CloseIdleConnections()

	// Twice on one connection: the tunnel must survive an answered request.
	for range 2 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, upstream.URL+"/userinfo", http.NoBody)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			t.Error(cerr)
		}
		if err != nil || string(body) != "{}" || resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("answer = %q %q (%v), want an empty JSON object", resp.Header.Get("Content-Type"), body, err)
		}
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("upstream saw %d requests, want none", n)
	}
}
