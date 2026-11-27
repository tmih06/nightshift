package manifest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// A NetworkPolicy with an empty podSelector isolates every pod in the
// namespace for the policy's types. sandbox-egress-proxy-access once selected
// everything, which silently cut the credential refresher off from DNS and
// the internet. Policies in nightshift must name the pods they cover.
func TestNamespacePoliciesNeverSelectEveryPod(t *testing.T) {
	root, err := os.OpenRoot("../../../k8s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("close k8s root: %v", err)
		}
	})
	for _, file := range []string{"egress.yaml", "auth.yaml", "warmpool.yaml", "nightshiftd.yaml"} {
		raw, err := root.ReadFile(file)
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
				Spec struct {
					PodSelector struct {
						MatchLabels      map[string]string `yaml:"matchLabels"`
						MatchExpressions []any             `yaml:"matchExpressions"`
					} `yaml:"podSelector"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			sel := doc.Spec.PodSelector
			if doc.Kind == "NetworkPolicy" && len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0 {
				t.Errorf("%s: NetworkPolicy %q selects every pod in its namespace", file, doc.Metadata.Name)
			}
		}
	}
}
