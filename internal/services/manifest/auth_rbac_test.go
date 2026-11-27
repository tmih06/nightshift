package manifest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type rbacRule struct {
	APIGroups     []string `yaml:"apiGroups"`
	Resources     []string `yaml:"resources"`
	ResourceNames []string `yaml:"resourceNames"`
	Verbs         []string `yaml:"verbs"`
}

// nightshift-auth holds every login, so its service account may write only
// the credential secrets and the warm pool sizes, never read other secrets
// or touch other workloads.
func TestTheLoginServiceHasLeastPrivilege(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/auth.yaml")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"secrets":          true,
		"sandboxwarmpools": true,
	}
	roles := 0
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"metadata"`
			Rules []rbacRule `yaml:"rules"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "Role" {
			if doc.Kind == "ClusterRole" || doc.Kind == "ClusterRoleBinding" {
				t.Errorf("%s %s: nightshift-auth must stay namespaced", doc.Kind, doc.Metadata.Name)
			}
			continue
		}
		roles++
		for _, r := range doc.Rules {
			for _, res := range r.Resources {
				if !allowed[res] {
					t.Errorf("%s/%s grants %q", doc.Metadata.Namespace, doc.Metadata.Name, res)
				}
			}
			for _, verb := range r.Verbs {
				if verb == "*" || verb == "delete" || verb == "deletecollection" {
					t.Errorf("%s/%s grants verb %q", doc.Metadata.Namespace, doc.Metadata.Name, verb)
				}
			}
			// Reading or editing a secret must be pinned to a name; only
			// create, which Kubernetes cannot scope by name, is open.
			if slices.Contains(r.Resources, "secrets") && !slices.Equal(r.Verbs, []string{"create"}) && len(r.ResourceNames) == 0 {
				t.Errorf("%s/%s: secret verbs %v must name what they cover", doc.Metadata.Namespace, doc.Metadata.Name, r.Verbs)
			}
		}
	}
	if roles != 2 {
		t.Errorf("found %d roles, want one in nightshift and one in nightshift-egress", roles)
	}
}
