package manifest

import (
	"io"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type document struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		Replicas           int `yaml:"replicas"`
		SandboxTemplateRef struct {
			Name string `yaml:"name"`
		} `yaml:"sandboxTemplateRef"`
		PodTemplate struct {
			Spec struct {
				Containers []struct {
					Name            string `yaml:"name"`
					Image           string `yaml:"image"`
					ImagePullPolicy string `yaml:"imagePullPolicy"`
					Resources       struct {
						Requests quantities `yaml:"requests"`
						Limits   quantities `yaml:"limits"`
					} `yaml:"resources"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"podTemplate"`
	} `yaml:"spec"`
}

type quantities struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

func TestWarmPools(t *testing.T) {
	f, err := os.Open("../../demo/warmpool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pools, templates := map[string]document{}, map[string]document{}
	dec := yaml.NewDecoder(f)
	for {
		var doc document
		if err := dec.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		switch doc.Kind {
		case "SandboxWarmPool":
			if _, ok := pools[doc.Metadata.Name]; ok {
				t.Fatalf("duplicate pool %s", doc.Metadata.Name)
			}
			pools[doc.Metadata.Name] = doc
		case "SandboxTemplate":
			if _, ok := templates[doc.Metadata.Name]; ok {
				t.Fatalf("duplicate template %s", doc.Metadata.Name)
			}
			templates[doc.Metadata.Name] = doc
		}
	}
	agents := []struct{ pool, template, image string }{
		{"pi-pool", "agent-bash-template", "pi"},
		{"codex-pool", "codex-template", "codex"},
		{"claude-code-pool", "claude-code-template", "claude-code"},
		{"opencode-pool", "opencode-template", "opencode"},
	}
	if len(pools) != len(agents) || len(templates) != len(agents) {
		t.Fatalf("got %d pools and %d templates, want %d each", len(pools), len(templates), len(agents))
	}
	for _, a := range agents {
		t.Run(a.image, func(t *testing.T) {
			p, ok := pools[a.pool]
			if !ok || p.Metadata.Namespace != "sandbox-demo" || p.Spec.Replicas != 1 || p.Spec.SandboxTemplateRef.Name != a.template {
				t.Errorf("pool: %+v", p)
			}
			tpl, ok := templates[a.template]
			if !ok || tpl.Metadata.Namespace != "sandbox-demo" {
				t.Fatalf("missing template %s", a.template)
			}
			containers := tpl.Spec.PodTemplate.Spec.Containers
			if len(containers) != 1 {
				t.Fatalf("got %d containers", len(containers))
			}
			c := containers[0]
			if c.Name != "agent" || c.Image != "ghcr.io/tmih06/nightshift-"+a.image+":latest" || c.ImagePullPolicy != "Always" {
				t.Errorf("container: %+v", c)
			}
			if c.Resources.Requests != (quantities{"100m", "128Mi"}) || c.Resources.Limits != (quantities{"500m", "256Mi"}) {
				t.Errorf("resources: %+v", c.Resources)
			}
		})
	}
}
