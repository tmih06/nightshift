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

// registry is k8s/creds-providers.json: the agents whose logins Nightshift
// hands to sandboxes.
type registry struct {
	Providers []struct {
		ID     string   `json:"id"`
		Secret string   `json:"secret"`
		Hosts  []string `json:"hosts"`
	} `json:"providers"`
}

func loadRegistry(t *testing.T) registry {
	t.Helper()
	raw, err := os.ReadFile("../../../k8s/creds-providers.json")
	if err != nil {
		t.Fatal(err)
	}
	var reg registry
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	if len(reg.Providers) == 0 {
		t.Fatal("registry lists no providers")
	}
	return reg
}

// Every login in the registry must be swappable on each host it names, or a
// sandbox would send a worthless placeholder. Adding a provider to the
// registry without a matching proxy rule fails here.
func TestShippedPolicySwapsEveryRegisteredLogin(t *testing.T) {
	cfg := shippedPolicy(t)
	for _, p := range loadRegistry(t).Providers {
		b, ok := cfg.Secrets[p.Secret]
		if !ok {
			t.Errorf("%s: policy has no binding for %s", p.ID, p.Secret)
			continue
		}
		if want := DefaultPlaceholderPrefix + p.Secret; b.Placeholder != want {
			t.Errorf("%s: placeholder = %q, want %q (what creds-stage.sh writes)", p.ID, b.Placeholder, want)
		}
		if want := "/etc/egress-secrets/REAL_" + p.Secret; b.File != want {
			t.Errorf("%s: %s file = %q, want %q so a refresh needs no restart", p.ID, p.Secret, b.File, want)
		}
		if want := "/etc/egress-secrets/LABEL_" + p.Secret; b.LabelFile != want {
			t.Errorf("%s: %s label_file = %q, want %q so usage is counted per account", p.ID, p.Secret, b.LabelFile, want)
		}
		if want := "/etc/egress-secrets/POOL_" + p.Secret; b.PoolFile != want {
			t.Errorf("%s: %s pool_file = %q, want %q so fallback chains hot-reload", p.ID, p.Secret, b.PoolFile, want)
		}
		for _, host := range p.Hosts {
			r, ok := cfg.MatchHostPort(host, 443)
			if !ok {
				t.Errorf("%s: no rule matches %s", p.ID, host)
				continue
			}
			if !r.AllowsRequest("POST", "/oauth2/v3/tokeninfo") {
				t.Errorf("%s: rule %q denies %s", p.ID, r.Name, host)
			}
			if !slices.Contains(r.Secrets, p.Secret) {
				t.Errorf("%s: rule %q does not bind %s on %s", p.ID, r.Name, p.Secret, host)
			}
		}
	}
}

func TestShippedPolicyKeepsGoogleScopedToOAuth(t *testing.T) {
	cfg := shippedPolicy(t)
	r, ok := cfg.MatchHostPort("www.googleapis.com", 443)
	if !ok {
		t.Fatal("no rule for www.googleapis.com")
	}
	if r.AllowsRequest("GET", "/drive/v3/files") {
		t.Error("www.googleapis.com must only allow /oauth2/ paths")
	}
}
