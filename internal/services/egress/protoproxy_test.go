package egress

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// longDevinKey() is long enough that its length takes two varint bytes, so
// every length around it must grow.
func longDevinKey() string { return strings.Repeat("k", 200) }

// readBody is what the upstream received.
func readBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
	}
	return b
}

// upstreamRoots trusts upstream's test certificate.
func upstreamRoots(upstream *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(upstream.Certificate())
	return pool
}

// protoClient is a client of a proxy with one proto rule in front of upstream.
func protoClient(t *testing.T, upstream *httptest.Server, rule Rule) *http.Client {
	t.Helper()
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
	rule.Hosts, rule.Ports, rule.Mode = []string{host}, []int{port}, ModeMITM
	ca, _, _, err := GenerateCA("Test Proxy CA")
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewProxy(Config{
		BlockPrivateIPs: boolPtr(false),
		ScrubResponses:  boolPtr(true),
		Rules:           []Rule{rule},
		Secrets:         map[string]SecretBinding{"DEVIN_API_KEY": {Placeholder: phDevin, Value: longDevinKey()}},
	}, Options{CA: ca, UpstreamTLS: &tls.Config{RootCAs: upstreamRoots(upstream)}, AuditWriter: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(proxy)
	t.Cleanup(srv.Close)
	proxyURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca.CertPEM())
	return &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: pool},
	}}
}

// Devin's CLI sends its key in the Authorization header and inside the
// protobuf request; the reply names the account's email. Through the proxy
// the server sees the real key in both, and the sandbox sees neither the key
// nor the email.
func TestProxySwapsAKeyInsideProtobufAndHidesTheAccount(t *testing.T) {
	var gotAuth string
	var gotBody []byte
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotBody = readBody(t, r)
		w.Header().Set("Content-Type", "application/proto")
		reply := field(1, append(field(2, []byte("someone@example.com")), field(7, []byte("key "+longDevinKey()))...))
		if _, err := w.Write(reply); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()
	client := protoClient(t, upstream, Rule{
		Name: "devin", Methods: []string{"POST"}, Secrets: []string{"DEVIN_API_KEY"},
		ProtoBodies: true, ProtoEmail: "nightshift@proxy.local",
	})

	body := field(1, append(field(1, []byte("chisel")), field(3, []byte(phDevin))...))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		upstream.URL+"/exa.seat_management_pb.SeatManagementService/GetUserStatus", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Authorization", "Basic "+phDevin+"-"+phDevin)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}

	if want := "Basic " + longDevinKey() + "-" + longDevinKey(); gotAuth != want {
		t.Errorf("Authorization = %q, want the real key twice", gotAuth)
	}
	if got := lookup(t, gotBody, 1, 3); string(got) != longDevinKey() {
		t.Errorf("metadata.api_key = %q, want the real key", got)
	}
	if got := lookup(t, gotBody, 1, 1); string(got) != "chisel" {
		t.Errorf("metadata.ide_name = %q: the fields after the key moved", got)
	}
	if got := lookup(t, reply, 1, 2); string(got) != "nightshift@proxy.local" {
		t.Errorf("reply email = %q, want the sandbox identity", got)
	}
	if got := lookup(t, reply, 1, 7); string(got) != "key "+redactedMarker {
		t.Errorf("reflected key = %q, want it scrubbed", got)
	}
}

// Without proto_bodies a rule leaves a protobuf body as the sandbox sent it.
func TestProxyLeavesProtobufBodiesAloneUnlessTheRuleCarriesThem(t *testing.T) {
	var gotBody []byte
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = readBody(t, r)
	}))
	defer upstream.Close()
	client := protoClient(t, upstream, Rule{Name: "plain", Methods: []string{"POST"}, Secrets: []string{"DEVIN_API_KEY"}})
	body := field(1, field(3, []byte(phDevin)))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL+"/x", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Authorization", "Basic "+phDevin)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Errorf("body = %q, want it untouched", gotBody)
	}
}
