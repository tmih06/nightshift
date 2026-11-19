package main

import (
	"io"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The data shape of demo-warmpool.yaml, the subset the tests pin. The file
// holds three documents; the SandboxTemplate is the only one that defines
// sandbox pods, through spec.podTemplate.spec.containers. Each container
// carries a name and a resource budget of cpu and memory quantity strings,
// requested and limited.
type manifest struct {
	Kind string `yaml:"kind"`
}

type sandboxTemplate struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		PodTemplate struct {
			Spec struct {
				Containers []container `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"podTemplate"`
	} `yaml:"spec"`
}

type container struct {
	Name      string    `yaml:"name"`
	Resources resources `yaml:"resources"`
}

type resources struct {
	Requests quantities `yaml:"requests"`
	Limits   quantities `yaml:"limits"`
}

type quantities struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

func loadSandboxTemplate(t *testing.T) sandboxTemplate {
	t.Helper()
	f, err := os.Open("demo-warmpool.yaml")
	if err != nil {
		t.Fatalf("open demo-warmpool.yaml: %v", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode document: %v", err)
		}
		var m manifest
		if err := doc.Decode(&m); err != nil {
			t.Fatalf("decode document kind: %v", err)
		}
		if m.Kind != "SandboxTemplate" {
			continue
		}
		var tpl sandboxTemplate
		if err := doc.Decode(&tpl); err != nil {
			t.Fatalf("decode SandboxTemplate: %v", err)
		}
		return tpl
	}
	t.Fatal("no SandboxTemplate document in demo-warmpool.yaml")
	return sandboxTemplate{}
}

// Every container the template declares must be capped, so no path can add
// an uncapped sandbox pod.
func TestEverySandboxContainerIsCapped(t *testing.T) {
	tpl := loadSandboxTemplate(t)
	if len(tpl.Spec.PodTemplate.Spec.Containers) == 0 {
		t.Fatal("SandboxTemplate declares no containers")
	}
	for _, c := range tpl.Spec.PodTemplate.Spec.Containers {
		t.Run(c.Name, func(t *testing.T) {
			if c.Resources.Requests.CPU == "" || c.Resources.Requests.Memory == "" {
				t.Errorf("requests must set cpu and memory, got %+v", c.Resources.Requests)
			}
			if c.Resources.Limits.CPU == "" || c.Resources.Limits.Memory == "" {
				t.Errorf("limits must set cpu and memory, got %+v", c.Resources.Limits)
			}
		})
	}
}

// The agent sandbox budget: a bash container needs little at idle, can burst
// to half a core and 256Mi, and cannot starve the node it shares with the
// kind control plane.
func TestAgentSandboxBudget(t *testing.T) {
	tpl := loadSandboxTemplate(t)
	var agent *container
	for i, c := range tpl.Spec.PodTemplate.Spec.Containers {
		if c.Name == "agent" {
			agent = &tpl.Spec.PodTemplate.Spec.Containers[i]
			break
		}
	}
	if agent == nil {
		t.Fatal("no container named agent in the SandboxTemplate")
	}
	cases := []struct {
		what string
		got  string
		want string
	}{
		{"cpu request", agent.Resources.Requests.CPU, "100m"},
		{"memory request", agent.Resources.Requests.Memory, "128Mi"},
		{"cpu limit", agent.Resources.Limits.CPU, "500m"},
		{"memory limit", agent.Resources.Limits.Memory, "256Mi"},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("agent container %s = %q, want %q", tc.what, tc.got, tc.want)
			}
		})
	}
}
