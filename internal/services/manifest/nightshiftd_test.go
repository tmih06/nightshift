package manifest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// doc is the part of a manifest these tests look at.
type doc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Rules []rbacRule     `yaml:"rules"`
	Spec  map[string]any `yaml:"spec"`
}

func (o doc) String() string { return o.Kind + " " + o.Metadata.Namespace + "/" + o.Metadata.Name }

// docs reads every document of a file in k8s/.
func docs(t *testing.T, file string) []doc {
	t.Helper()
	raw, err := openRoot(t, "../../../k8s").ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var out []doc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var o doc
		if err := dec.Decode(&o); errors.Is(err, io.EOF) {
			return out
		} else if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		out = append(out, o)
	}
}

// The services can open a shell in a sandbox, so what their service account
// may do is the real boundary around them. It is read-only across the cluster
// and writes only in nightshift; it never reads a secret but the one key to
// nightshift-auth, and never anything in the proxy's namespace that holds the
// real credentials.
func TestTheServicesHaveLeastPrivilege(t *testing.T) {
	var sawExec bool
	for _, o := range docs(t, "nightshiftd.yaml") {
		if o.Kind != "Role" && o.Kind != "ClusterRole" {
			continue
		}
		for _, r := range o.Rules {
			for _, list := range [][]string{r.APIGroups, r.Resources, r.Verbs} {
				if slices.Contains(list, "*") {
					t.Errorf("%s uses a wildcard: %+v", o, r)
				}
			}
			for _, verb := range r.Verbs {
				if slices.Contains([]string{"escalate", "bind", "impersonate", "deletecollection"}, verb) {
					t.Errorf("%s grants %q", o, verb)
				}
				if o.Kind == "ClusterRole" && verb != "get" && verb != "list" {
					t.Errorf("%s may %q across the whole cluster; cluster-wide it only reads", o, verb)
				}
			}
			if slices.Contains(r.Resources, "secrets") && (len(r.ResourceNames) == 0 || !slices.Equal(r.Verbs, []string{"get"})) {
				t.Errorf("%s: a secret may be read by name only, and only read: %+v", o, r)
			}
			if o.Metadata.Namespace == "nightshift-egress" && slices.Contains(r.Resources, "secrets") {
				t.Errorf("%s reads secrets in the proxy's namespace, where the real credentials are", o)
			}
			if slices.Contains(r.Resources, "pods/exec") {
				sawExec = true
				if o.Kind != "Role" || o.Metadata.Namespace != "nightshift" {
					t.Errorf("%s may exec; that is for nightshift only", o)
				}
			}
		}
	}
	if !sawExec {
		t.Error("terminals need pods/exec, and nothing grants it")
	}
}

// Every resource the services drive with kubectl is covered by a rule, so a
// new kubectl call that nobody granted fails here, not in someone's cluster.
func TestTheServicesCanDoWhatTheyDo(t *testing.T) {
	granted := map[string]bool{}
	for _, o := range docs(t, "nightshiftd.yaml") {
		if o.Kind != "Role" && o.Kind != "ClusterRole" {
			continue
		}
		for _, r := range o.Rules {
			for _, res := range r.Resources {
				granted[res] = true
			}
		}
	}
	for _, res := range []string{
		"sandboxes", "sandboxclaims", "sandboxwarmpools", "sandboxtemplates", "persistentvolumeclaims",
		"jobs", "pods", "pods/exec", "configmaps", "secrets", "events", "services", "networkpolicies",
	} {
		if !granted[res] {
			t.Errorf("the services use %s with kubectl and no rule grants it", res)
		}
	}
}

// The services run as a Deployment on whatever the cluster is: kind, a managed
// service, k3s, bare metal. Nothing in it may assume one.
func TestTheServicesDeploymentAssumesNoProvider(t *testing.T) {
	raw, err := openRoot(t, "../../../k8s").ReadFile("nightshiftd.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, bad := range []string{"kind-", "hostPath", "hostNetwork", "clusterIP:", "nodePort", "LoadBalancer", "storageClassName", "nodeName"} {
		if strings.Contains(text, bad) {
			t.Errorf("nightshiftd.yaml has %q, which ties it to one provider or exposes it on its own", bad)
		}
	}
	for _, want := range []string{"NIGHTSHIFT_CONTEXT", "secretKeyRef", "runAsNonRoot: true", "readOnlyRootFilesystem: true", "--plain-http", "--auth-url=http://nightshift-auth.nightshift.svc", "--proxy-url=http://egress-admin.nightshift-egress.svc"} {
		if !strings.Contains(text, want) {
			t.Errorf("nightshiftd.yaml lacks %q", want)
		}
	}
	for _, o := range docs(t, "nightshiftd.yaml") {
		if o.Kind == "Service" && o.Metadata.Name == "nightshiftd" {
			if typ, ok := o.Spec["type"].(string); ok && typ != "ClusterIP" {
				t.Errorf("the API Service is %s; it is a ClusterIP, reached through port-forward or the provider's ingress", typ)
			}
		}
		if o.Kind == "Deployment" {
			if n, ok := o.Spec["replicas"].(int); !ok || n != 1 {
				t.Errorf("the services run one replica per cluster, got %v", o.Spec["replicas"])
			}
		}
	}
}

// The egress proxy's admin port tells how much every account has been used. Only
// the services' API may reach it, whichever file a policy that opens it is in.
func TestOnlyTheServicesReachTheProxyAdminPort(t *testing.T) {
	type ingress struct {
		From []struct {
			PodSelector struct {
				MatchLabels map[string]string `yaml:"matchLabels"`
			} `yaml:"podSelector"`
		} `yaml:"from"`
		Ports []struct {
			Port int `yaml:"port"`
		} `yaml:"ports"`
	}
	opened := 0
	for _, file := range []string{"egress.yaml", "nightshiftd.yaml"} {
		raw, err := openRoot(t, "../../../k8s").ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		for {
			var doc struct {
				Kind     string `yaml:"kind"`
				Metadata struct {
					Name      string `yaml:"name"`
					Namespace string `yaml:"namespace"`
				} `yaml:"metadata"`
				Spec struct {
					Ingress []ingress `yaml:"ingress"`
				} `yaml:"spec"`
			}
			if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			if doc.Kind != "NetworkPolicy" || doc.Metadata.Namespace != "nightshift-egress" {
				continue
			}
			for _, rule := range doc.Spec.Ingress {
				for _, p := range rule.Ports {
					if p.Port != 9090 {
						continue
					}
					opened++
					for _, from := range rule.From {
						if from.PodSelector.MatchLabels["app"] != "nightshiftd" {
							t.Errorf("%s opens the proxy's admin port to something other than nightshiftd: %+v", doc.Metadata.Name, from)
						}
					}
					if len(rule.From) == 0 {
						t.Errorf("%s opens the proxy's admin port to everyone", doc.Metadata.Name)
					}
				}
			}
		}
	}
	if opened == 0 {
		t.Error("nothing opens the admin port to the services, so accounts could show no usage")
	}
}

// runNightshiftdSetup runs k8s/nightshiftd-setup.sh against stub kubectl,
// docker, kind, and openssl, on a cluster that has every namespace but
// local-path-storage, as a managed one would.
func runNightshiftdSetup(t *testing.T, env ...string) (applied, calls string) {
	t.Helper()
	binDir, logDir := t.TempDir(), t.TempDir()
	bin, logs := openRoot(t, binDir), openRoot(t, logDir)
	stub := `#!/bin/sh
echo "$(basename "$0") $*" >> "$CALLS"
case "$*" in
  *"get namespace local-path-storage"*) exit 1 ;;
  *"get secret nightshiftd-token"*) exit 1 ;;
  *apply*) cat >> "$APPLIED" ;;
  *"openssl"*|"rand "*) printf 'abc123' ;;
esac
exit 0
`
	for _, name := range []string{"kubectl", "docker", "kind", "openssl"} {
		if err := bin.WriteFile(name, []byte(stub), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `chmod 700 "$BIN"/* && bash ../../../k8s/nightshiftd-setup.sh`)
	cmd.Env = append(os.Environ(),
		"BIN="+binDir,
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"APPLIED="+logDir+"/applied",
		"CALLS="+logDir+"/calls",
		"NIGHTSHIFT_CONTEXT=",
		"KUBE_CONTEXT=",
	)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nightshiftd-setup.sh: %v\n%s", err, out)
	}
	a, err := logs.ReadFile("applied")
	if err != nil {
		t.Fatal(err)
	}
	c, err := logs.ReadFile("calls")
	if err != nil {
		t.Fatal(err)
	}
	return string(a), string(c)
}

// Any Kubernetes provider: a cluster that is not kind pulls the image, builds
// and loads nothing, binds log access only where a namespace exists, and
// acts on the context it was given.
func TestNightshiftdSetupOnAManagedCluster(t *testing.T) {
	applied, calls := runNightshiftdSetup(t, "NIGHTSHIFT_CONTEXT=gke_proj_zone_prod")
	if !strings.Contains(applied, "image: ghcr.io/tmih06/nightshiftd:latest") {
		t.Error("a non-kind cluster must pull the published image")
	}
	for _, bad := range []string{"kind load", "docker build", "docker push", "rollout restart"} {
		if strings.Contains(calls, bad) {
			t.Errorf("a non-kind cluster ran %q:\n%s", bad, calls)
		}
	}
	if strings.Contains(calls, "kind-agent-sandbox") {
		t.Errorf("the setup used the kind default instead of the context it was given:\n%s", calls)
	}
	if !strings.Contains(calls, "--context gke_proj_zone_prod") {
		t.Errorf("kubectl was not pointed at the given context:\n%s", calls)
	}
	for _, ns := range []string{"nightshift", "nightshift-egress", "kube-system", "agent-sandbox-system"} {
		if !strings.Contains(calls, "-n "+ns+" create rolebinding nightshiftd-logs") {
			t.Errorf("log access was not bound in %s", ns)
		}
	}
	if strings.Contains(calls, "-n local-path-storage create rolebinding") {
		t.Error("log access was bound in a namespace this cluster does not have")
	}
	if !strings.Contains(calls, "create secret generic nightshiftd-token") {
		t.Errorf("the API token was not made:\n%s", calls)
	}
}

func TestNightshiftdSetupBuildsAndLoadsOnKind(t *testing.T) {
	applied, calls := runNightshiftdSetup(t, "NIGHTSHIFT_CONTEXT=kind-dev")
	if !strings.Contains(applied, "image: nightshiftd:local") {
		t.Error("kind must run the image it built")
	}
	if !strings.Contains(calls, "kind load docker-image nightshiftd:local --name dev") || !strings.Contains(calls, "rollout restart deploy/nightshiftd") {
		t.Errorf("kind builds, loads, and restarts:\n%s", calls)
	}
	applied, calls = runNightshiftdSetup(t, "NIGHTSHIFT_CONTEXT=eks-prod", "NIGHTSHIFTD_IMAGE=registry.example.com/ns/nightshiftd:1", "NIGHTSHIFTD_BUILD=1")
	if !strings.Contains(applied, "image: registry.example.com/ns/nightshiftd:1") || !strings.Contains(calls, "docker push registry.example.com/ns/nightshiftd:1") {
		t.Errorf("an own image is built, pushed, and used:\n%s", calls)
	}
}
