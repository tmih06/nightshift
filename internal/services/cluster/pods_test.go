package cluster

import (
	"strings"
	"testing"
)

func TestIsEssentialPod(t *testing.T) {
	cases := []struct {
		ns, name string
		want     bool
	}{
		{"nightshift", "pi-pool-abc", true},
		{"nightshift", "clone-pr-repo-1-xyz", true},
		{"nightshift-egress", "egress-proxy-abc", true},
		{"agent-sandbox-system", "agent-sandbox-controller-abc", true},
		{"kube-system", "metrics-server-767bf95dcc-qblb6", true},
		{"kube-system", "coredns-66bc5c9577-2fkxl", false},
		{"kube-system", "etcd-control-plane", false},
		{"local-path-storage", "local-path-provisioner-abc", true},
		{"sandbox-demo", "pi-pool-nnjdh", false},
		{"default", "random-pod", false},
	}
	for _, tc := range cases {
		if got := isEssentialPod(tc.ns, tc.name); got != tc.want {
			t.Errorf("isEssentialPod(%q, %q) = %v, want %v", tc.ns, tc.name, got, tc.want)
		}
	}
}

func TestBuildSandboxesIncludesEssentialInfraPods(t *testing.T) {
	const sbRaw = `{"items":[
		{"metadata":{"name":"pi-pool-a","annotations":{"agents.x-k8s.io/sandbox-template-ref":"pi-template"},"ownerReferences":[{"kind":"SandboxWarmPool","name":"pi-pool"}]}}
	]}`
	const podsRaw = `{"items":[
		{"metadata":{"name":"pi-pool-a","namespace":"nightshift"},"spec":{"nodeName":"n1","containers":[{"name":"agent","image":"pi:latest","resources":{"limits":{"cpu":"1000m","memory":"2Gi"}}}]},"status":{"phase":"Running","podIP":"10.0.0.1","containerStatuses":[{"ready":true,"restartCount":0}]}},
		{"metadata":{"name":"egress-proxy-1","namespace":"nightshift-egress","ownerReferences":[{"kind":"ReplicaSet","name":"egress-proxy-rs"}]},"spec":{"nodeName":"n1","containers":[{"name":"proxy","image":"nightshift-egress-proxy:local","ports":[{"name":"proxy","containerPort":8080},{"name":"admin","containerPort":9090}],"resources":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"cpu":"200m","memory":"128Mi"}}}]},"status":{"phase":"Running","podIP":"10.0.0.2","containerStatuses":[{"ready":true,"restartCount":0}]}},
		{"metadata":{"name":"agent-sandbox-controller-1","namespace":"agent-sandbox-system","ownerReferences":[{"kind":"ReplicaSet","name":"ctrl-rs"}]},"spec":{"nodeName":"n1","containers":[{"name":"ctrl","image":"ctrl:v1"}]},"status":{"phase":"Running","podIP":"10.0.0.3","containerStatuses":[{"ready":true,"restartCount":7}]}},
		{"metadata":{"name":"metrics-server-1","namespace":"kube-system"},"spec":{"nodeName":"n1","containers":[{"name":"ms","image":"ms:v1"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":0}]}},
		{"metadata":{"name":"coredns-1","namespace":"kube-system"},"spec":{"nodeName":"n1","containers":[{"name":"dns","image":"dns:v1"}]},"status":{"phase":"Running"}},
		{"metadata":{"name":"pi-pool-demo","namespace":"sandbox-demo"},"spec":{"nodeName":"n1","containers":[{"name":"agent","image":"pi:latest"}]},"status":{"phase":"Running"}},
		{"metadata":{"name":"clone-pr-1-xyz","namespace":"nightshift","ownerReferences":[{"kind":"Job","name":"clone-pr-1"}]},"spec":{"nodeName":"n1","containers":[{"name":"clone","image":"pi:latest"}]},"status":{"phase":"Running","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"ContainerCreating"}}}]}}
	]}`
	const metricsRaw = `{"items":[
		{"metadata":{"name":"egress-proxy-1","namespace":"nightshift-egress"},"containers":[{"usage":{"cpu":"5000000n","memory":"8Mi"}}]},
		{"metadata":{"name":"agent-sandbox-controller-1","namespace":"agent-sandbox-system"},"containers":[{"usage":{"cpu":"12m","memory":"42Mi"}}]}
	]}`

	var sb sandboxList
	var pl podList
	var ml podMetricsList
	mustParse(t, sbRaw, &sb)
	mustParse(t, podsRaw, &pl)
	mustParse(t, metricsRaw, &ml)

	boxes, err := buildSandboxes(sb, pl, &ml)
	if err != nil {
		t.Fatalf("buildSandboxes: %v", err)
	}
	// Expect: pi-pool-a (warm), egress-proxy-1 (egress), agent-sandbox-controller-1 (system), metrics-server-1 (system), clone-pr-1-xyz (job).
	// coredns-1 and sandbox-demo/pi-pool-demo must be excluded.
	if len(boxes) != 5 {
		t.Fatalf("got %d essential pods, want 5: %+v", len(boxes), boxes)
	}

	byName := map[string]Sandbox{}
	for _, b := range boxes {
		byName[b.Name] = b
	}
	if _, excluded := byName["coredns-1"]; excluded {
		t.Error("coredns should not be included")
	}
	if _, excluded := byName["pi-pool-demo"]; excluded {
		t.Error("sandbox-demo pod should not be included")
	}

	egress := byName["egress-proxy-1"]
	if egress.PodRole() != RoleEgress || egress.Namespace != EgressNamespace || !egress.IsInfra() {
		t.Errorf("egress role/ns = %+v", egress)
	}
	if egress.Ready != "1/1" || egress.PodIP != "10.0.0.2" || egress.CPUUsageMC != 5 || egress.MemUsageMiB != 8 {
		t.Errorf("egress status/metrics = %+v", egress)
	}
	if len(egress.Ports) != 2 || egress.Ports[0] != "8080/proxy" || egress.Ports[1] != "9090/admin" {
		t.Errorf("egress ports = %v", egress.Ports)
	}

	ctrl := byName["agent-sandbox-controller-1"]
	if ctrl.PodRole() != RoleSystem || ctrl.Restarts != 7 || ctrl.CPUUsageMC != 12 || ctrl.MemUsageMiB != 42 {
		t.Errorf("controller = %+v", ctrl)
	}

	job := byName["clone-pr-1-xyz"]
	if job.PodRole() != RoleJob || job.Ready != "0/1" || job.Waiting != "ContainerCreating" {
		t.Errorf("job = %+v", job)
	}
}

func TestParseEgressConfigSections(t *testing.T) {
	raw := []byte(`{"apiVersion":"v1","kind":"List","items":[
		{"kind":"ConfigMap","metadata":{"name":"kube-root-ca.crt","namespace":"nightshift-egress"},"data":{"ca.crt":"cert"}},
		{"kind":"ConfigMap","metadata":{"name":"egress-policy","namespace":"nightshift-egress"},"data":{"policy.yaml":"rules: []"}},
		{"kind":"Service","metadata":{"name":"egress-proxy","namespace":"nightshift-egress"},"spec":{"ports":[{"port":8080}]}},
		{"kind":"NetworkPolicy","metadata":{"name":"egress-proxy-ingress","namespace":"nightshift-egress"}}
	]}`)
	got := parseEgressConfigSections(raw)
	for _, want := range []string{"## ConfigMap · egress-policy", "## Service · egress-proxy", "## NetworkPolicy · egress-proxy-ingress"} {
		if !strings.Contains(got, want) {
			t.Errorf("parseEgressConfigSections missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "kube-root-ca.crt") {
		t.Errorf("parseEgressConfigSections should omit kube-root-ca.crt:\n%s", got)
	}
}
