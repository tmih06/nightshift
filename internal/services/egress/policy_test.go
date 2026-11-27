package egress

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// shippedPolicy loads the policy.yaml embedded in k8s/egress.yaml.
func shippedPolicy(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../k8s/egress.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Data map[string]string `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if doc.Kind == "ConfigMap" && doc.Metadata.Name == "egress-policy" {
			cfg, err := ParseConfig([]byte(doc.Data["policy.yaml"]))
			if err != nil {
				t.Fatalf("shipped policy does not parse: %v", err)
			}
			return cfg
		}
	}
	t.Fatal("k8s/egress.yaml has no egress-policy ConfigMap")
	return Config{}
}

func TestShippedPolicyBindsEverySecretItNames(t *testing.T) {
	cfg := shippedPolicy(t)
	for _, r := range cfg.Rules {
		for _, name := range r.Secrets {
			if _, ok := cfg.Secrets[name]; !ok {
				t.Errorf("rule %q names secret %q with no binding", r.Name, name)
			}
		}
	}
}

// Codex looks its login's account_id up in this list and refuses to run when it
// is missing. A sandbox's account_id is a placeholder, which the real list never
// holds, and the real list names the account.
func TestShippedPolicyAnswersCodexWorkspaceDiscovery(t *testing.T) {
	const id = "nightshift:secret:CODEX_ACCOUNT_ID"
	const path = "/backend-api/wham/accounts/check"
	cfg := shippedPolicy(t)
	r, ok := cfg.MatchHostPort("chatgpt.com", 443)
	if !ok {
		t.Fatal("no rule for chatgpt.com")
	}
	body, answered := r.answer(path)
	if !answered || !r.AllowsRequest("GET", path) {
		t.Fatalf("%s is not answered by the proxy", path)
	}
	var got struct {
		Accounts []struct {
			ID       string `json:"id"`
			Origin   string `json:"workspace_backend_origin"`
			Override string `json:"account_routing_override"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(got.Accounts) != 1 || got.Accounts[0].ID != id {
		t.Fatalf("accounts = %+v, want only %s", got.Accounts, id)
	}
	// Codex also refuses an entry without both routing fields.
	if a := got.Accounts[0]; a.Origin != "NO_CONSTRAINT" || a.Override != "NO_CONSTRAINT" {
		t.Errorf("routing = %q %q, want NO_CONSTRAINT", a.Origin, a.Override)
	}
	// TestEveryAgentIsSwappedByTheProxy checks the id is the one a sandbox's
	// auth.json carries.
}

// Google names the account behind a token to whoever asks with it, so the
// proxy answers those questions itself and sends none upstream.
func TestShippedPolicyNeverAsksGoogleWhoTheAccountIs(t *testing.T) {
	cfg := shippedPolicy(t)
	r, ok := cfg.MatchHostPort("www.googleapis.com", 443)
	if !ok {
		t.Fatal("no rule for www.googleapis.com")
	}
	if len(r.Secrets) > 0 {
		t.Errorf("rule %q injects %v, which would let a sandbox ask Google who it is", r.Name, r.Secrets)
	}
	for _, path := range []string{"/oauth2/v2/userinfo", "/oauth2/v3/userinfo"} {
		body, answered := r.answer(path)
		if !answered || !r.AllowsRequest("GET", path) {
			t.Errorf("%s is not answered by the proxy", path)
			continue
		}
		var who map[string]any
		if err := json.Unmarshal([]byte(body), &who); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		// agy's /usage loads forever without an email.
		if who["email"] != "nightshift@proxy.local" {
			t.Errorf("%s email = %v, want the fixed sandbox identity", path, who["email"])
		}
	}
	for _, path := range []string{"/oauth2/v3/tokeninfo", "/drive/v3/files"} {
		if r.AllowsRequest("GET", path) {
			if _, answered := r.answer(path); !answered {
				t.Errorf("%s would be forwarded", path)
			}
		}
	}
}

// Devin's key rides inside protobuf requests to its API server, and the
// replies name the account by email. The shipped rule must swap the key
// there and answer with the sandbox identity, and Devin's login hosts must
// stay closed so nobody signs in from a sandbox.
func TestShippedPolicyProxiesDevinsKeyAndHidesItsAccount(t *testing.T) {
	cfg := shippedPolicy(t)
	r, ok := cfg.MatchHostPort("server.codeium.com", 443)
	if !ok {
		t.Fatal("no rule for server.codeium.com")
	}
	if r.Mode != ModeMITM || !r.ProtoBodies || !slices.Contains(r.Secrets, "DEVIN_ACCESS_TOKEN") {
		t.Errorf("rule %q: mode %s, proto_bodies %v, secrets %v", r.Name, r.Mode, r.ProtoBodies, r.Secrets)
	}
	if r.ProtoEmail != "nightshift@proxy.local" {
		t.Errorf("rule %q answers the account's email with %q", r.Name, r.ProtoEmail)
	}
	if b, ok := cfg.Secrets["DEVIN_ACCESS_TOKEN"]; !ok || b.Placeholder != DefaultPlaceholderPrefix+"DEVIN_ACCESS_TOKEN" {
		t.Errorf("DEVIN_ACCESS_TOKEN binding = %+v", b)
	}
	for _, host := range []string{"app.devin.ai", "api.devin.ai", "static.devin.ai"} {
		if r, ok := cfg.MatchHostPort(host, 443); ok {
			t.Errorf("rule %q opens %s", r.Name, host)
		}
	}
}
