package providers

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// shippedPolicy is the policy in k8s/egress.yaml: its rules and secrets.
type shippedPolicy struct {
	Rules []struct {
		Name    string            `yaml:"name"`
		Hosts   []string          `yaml:"hosts"`
		Mode    string            `yaml:"mode"`
		Secrets []string          `yaml:"secrets"`
		Answers map[string]string `yaml:"answers"`
	} `yaml:"rules"`
	Secrets map[string]struct {
		Placeholder string `yaml:"placeholder"`
		File        string `yaml:"file"`
		LabelFile   string `yaml:"label_file"`
		PoolFile    string `yaml:"pool_file"`
	} `yaml:"secrets"`
}

func loadPolicy(t *testing.T) shippedPolicy {
	t.Helper()
	raw, err := os.ReadFile("../../../../k8s/egress.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind     string                `yaml:"kind"`
			Metadata struct{ Name string } `yaml:"metadata"`
			Data     map[string]string     `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			t.Fatal("k8s/egress.yaml has no egress-policy ConfigMap")
		} else if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "ConfigMap" || doc.Metadata.Name != "egress-policy" {
			continue
		}
		var p shippedPolicy
		if err := yaml.Unmarshal([]byte(doc.Data["policy.yaml"]), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
}

// Every agent's token reaches the proxy as POOL_, REAL_ and LABEL_ files that
// the proxy must read, and is swapped only on the agent's own hosts. An agent
// added without a matching proxy rule would hand sandboxes a placeholder
// nobody swaps; it fails here.
func TestEveryAgentIsSwappedByTheProxy(t *testing.T) {
	p := loadPolicy(t)
	for _, prov := range All(nil) {
		info := prov.Info()
		b, ok := p.Secrets[info.Secret]
		if !ok {
			t.Errorf("%s: the proxy binds no %s", info.ID, info.Secret)
			continue
		}
		dir := "/etc/egress-secrets/"
		if b.Placeholder != "nightshift:secret:"+info.Secret || b.File != dir+"REAL_"+info.Secret ||
			b.LabelFile != dir+"LABEL_"+info.Secret || b.PoolFile != dir+"POOL_"+info.Secret {
			t.Errorf("%s: binding %+v is not what nightshift-auth publishes", info.ID, b)
		}
		if len(info.Hosts) == 0 {
			t.Errorf("%s names no API host", info.ID)
		}
		for _, host := range info.Hosts {
			swapped := false
			for _, r := range p.Rules {
				if slices.Contains(r.Hosts, host) && r.Mode == "mitm" && slices.Contains(r.Secrets, info.Secret) {
					swapped = true
				}
			}
			if !swapped {
				t.Errorf("%s: no mitm rule swaps %s on %s", info.ID, info.Secret, host)
			}
		}
	}
}

// Codex refuses to run unless the proxy's workspace answer holds the
// account_id its login names, which in a sandbox is a placeholder.
func TestCodexSandboxAccountIDIsTheOneTheProxyAnswers(t *testing.T) {
	login, err := newCodex(nil).SandboxLogin(Credential{AccountID: "real"}, "ph")
	if err != nil {
		t.Fatal(err)
	}
	id := "nightshift:secret:" + codexAccountIDSecret
	if !strings.Contains(string(login), id) {
		t.Fatalf("the sandbox auth.json lacks %s: %s", id, login)
	}
	for _, r := range loadPolicy(t).Rules {
		for _, body := range r.Answers {
			if strings.Contains(body, id) {
				return
			}
		}
	}
	t.Errorf("no proxy answer names %s", id)
}

// A sandbox login's expiry must lie far ahead, or the CLI tries to renew the
// placeholder itself.
func TestSandboxLoginsNeverLookExpired(t *testing.T) {
	for _, p := range All(nil) {
		login, err := p.SandboxLogin(Credential{Expires: time.Now()}, "ph")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(login), time.Now().Format("2006-01-02")) {
			t.Errorf("%s: the sandbox login carries today's expiry: %s", p.Info().ID, login)
		}
	}
}
