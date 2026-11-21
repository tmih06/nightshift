package cluster

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCPUMillicores(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"100m", 100},
		{"500m", 500},
		{"1", 1000},
		{"2500000n", 2},
		{"4857n", 0},
	}
	for _, tc := range cases {
		got, err := cpuMillicores(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("cpuMillicores(%q) = %d, %v, want %d", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "abc", "12x"} {
		if _, err := cpuMillicores(bad); err == nil {
			t.Errorf("cpuMillicores(%q) accepted a bad quantity", bad)
		}
	}
}

func TestMemMiB(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"128Mi", 128},
		{"256Mi", 256},
		{"7151616", 6},
		{"2048Ki", 2},
		{"1Gi", 1024},
	}
	for _, tc := range cases {
		got, err := memMiB(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("memMiB(%q) = %d, %v, want %d", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "zz", "5MB"} {
		if _, err := memMiB(bad); err == nil {
			t.Errorf("memMiB(%q) accepted a bad quantity", bad)
		}
	}
}

const sandboxListFixture = `{"items":[
 {"metadata":{"name":"sbx-warm-1","annotations":{"agents.x-k8s.io/sandbox-template-ref":"pi-template"},"ownerReferences":[{"kind":"SandboxWarmPool","name":"agent-pool"}]}},
 {"metadata":{"name":"sbx-orphan"}},
 {"metadata":{"name":"sbx-claim-1","annotations":{"agents.x-k8s.io/sandbox-template-ref":"opencode-template"},"ownerReferences":[{"kind":"SandboxClaim","name":"my-agent"}]}}
]}`

const podListFixture = `{"items":[
 {"metadata":{"name":"sbx-warm-1"},"spec":{"nodeName":"worker-a","containers":[{"resources":{"limits":{"cpu":"500m","memory":"256Mi"}}}]},"status":{"phase":"Running"}},
 {"metadata":{"name":"sbx-claim-1"},"spec":{"nodeName":"worker-b","containers":[{"resources":{"limits":{"cpu":"250m","memory":"128Mi"}}}]},"status":{"phase":"Running"}}
]}`

const metricsFixture = `{"items":[
 {"metadata":{"name":"sbx-warm-1"},"containers":[{"usage":{"cpu":"2500000n","memory":"7151616"}}]},
 {"metadata":{"name":"sbx-gone"},"containers":[{"usage":{"cpu":"1n","memory":"1024"}}]}
]}`

func mustParse(t *testing.T, in string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(in), out); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
}

// The observable behavior: the join maps each sandbox to its pod limits and
// its live usage by name, warm sandboxes sort first, and a sandbox without a
// pod still shows with an unknown phase.
func TestBuildSandboxes(t *testing.T) {
	var sb sandboxList
	var pl podList
	var ml podMetricsList
	mustParse(t, sandboxListFixture, &sb)
	mustParse(t, podListFixture, &pl)
	mustParse(t, metricsFixture, &ml)

	boxes, err := buildSandboxes(sb, pl, &ml)
	if err != nil {
		t.Fatalf("buildSandboxes: %v", err)
	}
	if len(boxes) != 3 {
		t.Fatalf("got %d boxes, want 3", len(boxes))
	}

	warm := boxes[0]
	if warm.Name != "sbx-warm-1" || warm.OwnerKind != "SandboxWarmPool" || warm.Owner != "agent-pool" || warm.Template != "pi-template" {
		t.Errorf("warm box = %+v", warm)
	}
	if warm.Phase != "Running" || warm.CPULimitMC != 500 || warm.MemLimitMiB != 256 || warm.NodeName != "worker-a" {
		t.Errorf("warm box = %+v", warm)
	}
	if warm.CPUUsageMC != 2 || warm.MemUsageMiB != 6 {
		t.Errorf("warm usage = %+v, want cpu 2m mem 6Mi", warm)
	}

	claim := boxes[1]
	if claim.Name != "sbx-claim-1" || claim.OwnerKind != "SandboxClaim" || claim.Owner != "my-agent" || claim.Template != "opencode-template" {
		t.Errorf("claimed box = %+v", claim)
	}
	if claim.CPULimitMC != 250 || claim.MemLimitMiB != 128 || claim.NodeName != "worker-b" {
		t.Errorf("claimed box = %+v, want 250m and 128Mi limits plus node worker-b", claim)
	}
	if claim.CPUUsageMC != 0 || claim.MemUsageMiB != 0 {
		t.Errorf("claimed box usage = %+v, want zero: no metrics entry matches", claim)
	}

	orphan := boxes[2]
	if orphan.Name != "sbx-orphan" || orphan.Phase != "…" || orphan.CPULimitMC != 0 || orphan.NodeName != "" {
		t.Errorf("podless box = %+v, want unknown phase and no limits", orphan)
	}
}

func TestBuildSandboxesParsesCreationTime(t *testing.T) {
	var sb sandboxList
	var pl podList
	mustParse(t, sandboxListFixture, &sb)
	mustParse(t, podListFixture, &pl)

	withTime := `{"items":[
	 {"metadata":{"name":"sbx-warm-1","creationTimestamp":"2026-11-19T08:00:00Z","ownerReferences":[{"kind":"SandboxWarmPool","name":"agent-pool"}]}}
	]}`
	mustParse(t, withTime, &sb)

	boxes, err := buildSandboxes(sb, pl, nil)
	if err != nil {
		t.Fatalf("buildSandboxes: %v", err)
	}
	if want := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC); !boxes[0].Created.Equal(want) {
		t.Errorf("Created = %v, want %v", boxes[0].Created, want)
	}
}

// The claim the dashboard applies must carry the same shape as
// k8s/claim.yaml: the pool ref and Delete on shutdown.
func TestClaimManifest(t *testing.T) {
	var c struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name      string `yaml:"name"`
			Namespace string `yaml:"namespace"`
		} `yaml:"metadata"`
		Spec struct {
			WarmPoolRef struct {
				Name string `yaml:"name"`
			} `yaml:"warmPoolRef"`
			Lifecycle struct {
				ShutdownPolicy string `yaml:"shutdownPolicy"`
			} `yaml:"lifecycle"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(claimManifest("dash-claim-42", "codex-pool")), &c); err != nil {
		t.Fatalf("claim manifest does not parse: %v", err)
	}
	if c.Kind != "SandboxClaim" || c.Metadata.Name != "dash-claim-42" || c.Metadata.Namespace != Namespace {
		t.Errorf("claim manifest header = %+v", c)
	}
	if c.Spec.WarmPoolRef.Name != "codex-pool" {
		t.Errorf("warmPoolRef = %q, want codex-pool", c.Spec.WarmPoolRef.Name)
	}
	if c.Spec.Lifecycle.ShutdownPolicy != "Delete" {
		t.Errorf("shutdownPolicy = %q, want Delete", c.Spec.Lifecycle.ShutdownPolicy)
	}
}

func TestCleanManifestYAML(t *testing.T) {
	raw := []byte(`{
		"apiVersion": "extensions.agents.x-k8s.io/v1beta1",
		"kind": "SandboxTemplate",
		"metadata": {
			"name": "agent-bash-template",
			"namespace": "nightshift",
			"resourceVersion": "12281",
			"uid": "02d23c69",
			"managedFields": [{"manager": "kubectl"}],
			"annotations": {
				"kubectl.kubernetes.io/last-applied-configuration": "{\"huge\":\"blob\"}",
				"custom/note": "keep-me"
			}
		},
		"spec": {
			"failedPodPolicy": "Retain"
		}
	}`)
	got := cleanManifestYAML(raw)
	for _, want := range []string{"kind: SandboxTemplate", "name: agent-bash-template", "custom/note: keep-me", "failedPodPolicy: Retain"} {
		if !strings.Contains(got, want) {
			t.Errorf("cleanManifestYAML missing %q:\n%s", want, got)
		}
	}
	for _, drop := range []string{"managedFields", "resourceVersion", "uid:", "last-applied-configuration"} {
		if strings.Contains(got, drop) {
			t.Errorf("cleanManifestYAML kept noisy field %q:\n%s", drop, got)
		}
	}
}

func TestParseClusterList(t *testing.T) {
	raw := []byte(`{"apiVersion":"v1","kind":"List","items":[
		{"kind":"Sandbox","metadata":{"name":"sbx-1","ownerReferences":[{"kind":"SandboxWarmPool","name":"agent-pool"}]}},
		{"kind":"Pod","metadata":{"name":"sbx-1"},"status":{"phase":"Running"}},
		{"kind":"SandboxWarmPool","metadata":{"name":"agent-pool"},"spec":{"replicas":2,"sandboxTemplateRef":{"name":"agent-bash-template"},"updateStrategy":{"type":"Recreate"}},"status":{"readyReplicas":2}},
		{"kind":"SandboxTemplate","metadata":{"name":"agent-bash-template"},"spec":{"failedPodPolicy":"Retain"}},
		{"kind":"SandboxWarmPool","metadata":{"name":"codex-pool"},"spec":{"replicas":1,"sandboxTemplateRef":{"name":"codex-template"}}},
		{"kind":"SandboxTemplate","metadata":{"name":"codex-template"},"spec":{"image":"codex"}}
	]}`)
	sb, pl, pools, err := parseClusterList(raw)
	if err != nil {
		t.Fatalf("parseClusterList: %v", err)
	}
	if len(sb.Items) != 1 || len(pl.Items) != 1 {
		t.Fatalf("items = %d sandboxes, %d pods, want 1 and 1", len(sb.Items), len(pl.Items))
	}
	pool := pools["agent-pool"]
	if len(pools) != 2 || pools["codex-pool"].Template != "codex-template" || !strings.Contains(pools["codex-pool"].TemplateYAML, "codex") {
		t.Errorf("other pool missing or mismatched: %+v", pools)
	}
	if pool.Name != "agent-pool" || pool.Template != "agent-bash-template" || pool.Ready != 2 || pool.Desired != 2 {
		t.Errorf("pool = %+v", pool)
	}
	if !strings.Contains(pool.ManifestYAML, "kind: SandboxWarmPool") || !strings.Contains(pool.TemplateYAML, "kind: SandboxTemplate") {
		t.Errorf("missing pool/template YAML: %+v", pool)
	}
}

func TestBuildSandboxesRejectsBadQuantities(t *testing.T) {
	var sb sandboxList
	var pl podList
	var ml podMetricsList
	mustParse(t, sandboxListFixture, &sb)
	mustParse(t, metricsFixture, &ml)

	badPods := `{"items":[
	 {"metadata":{"name":"sbx-warm-1"},"spec":{"containers":[{"resources":{"limits":{"cpu":"1x"}}}]},"status":{"phase":"Running"}}
	]}`
	mustParse(t, badPods, &pl)
	if _, err := buildSandboxes(sb, pl, &ml); err == nil {
		t.Error("buildSandboxes accepted an unparsable cpu limit")
	}

	badMetrics := `{"items":[
	 {"metadata":{"name":"sbx-warm-1"},"containers":[{"usage":{"cpu":"zz","memory":"10Mi"}}]}
	]}`
	mustParse(t, badMetrics, &ml)
	mustParse(t, podListFixture, &pl)
	if _, err := buildSandboxes(sb, pl, &ml); err == nil {
		t.Error("buildSandboxes accepted an unparsable usage quantity")
	}
}
