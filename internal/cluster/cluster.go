// Data shape. The dashboard's unit is a sandbox, one entry in the left
// pane and one inspect view in the right. A sandbox is a Sandbox object
// in the demo namespace; the controller runs a pod with the same name, so
// sandbox, pod, and metrics lines all join on that name. An entry carries
// who owns the sandbox (warm pool or claim), the pod phase, the container
// limits from the pod spec, and live usage from the metrics API. Zero
// usage means unknown, not idle.
package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// KubeContext is the kind cluster the demo runs on.
	KubeContext = "kind-agent-sandbox"
	// Namespace is the demo namespace every read and write targets.
	Namespace = "sandbox-demo"
	// WarmPool is the pool claims draw from.
	WarmPool = "agent-pool"
)

type Sandbox struct {
	Name        string
	OwnerKind   string // SandboxWarmPool or SandboxClaim
	Owner       string
	Phase       string // pod phase, "…" until the pod exists
	Created     time.Time
	CPULimitMC  int64 // container cpu limit, millicores
	MemLimitMiB int64 // container memory limit, MiB
	CPUUsageMC  int64 // live cpu usage, millicores
	MemUsageMiB int64 // live memory usage, MiB
}

type ownerReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type sandboxItem struct {
	Metadata struct {
		Name            string           `json:"name"`
		Created         string           `json:"creationTimestamp"`
		OwnerReferences []ownerReference `json:"ownerReferences"`
	} `json:"metadata"`
}

type sandboxList struct {
	Items []sandboxItem `json:"items"`
}

type podContainer struct {
	Resources struct {
		Limits map[string]string `json:"limits"`
	} `json:"resources"`
}

type podItem struct {
	Metadata struct {
		Name    string `json:"name"`
		Created string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Containers []podContainer `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type podList struct {
	Items []podItem `json:"items"`
}

type podMetricsList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Containers []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// buildSandboxes joins the three cluster views into the boxes the grid
// shows. It is pure: the kubectl calls stay outside.
func buildSandboxes(sb sandboxList, pl podList, ml *podMetricsList) ([]Sandbox, error) {
	podByName := map[string]*podItem{}
	for i := range pl.Items {
		podByName[pl.Items[i].Metadata.Name] = &pl.Items[i]
	}
	usedByName := map[string][2]int64{}
	if ml != nil {
		for _, item := range ml.Items {
			var cpu, mem int64
			for _, c := range item.Containers {
				mc, err := cpuMillicores(c.Usage.CPU)
				if err != nil {
					return nil, err
				}
				mm, err := memMiB(c.Usage.Memory)
				if err != nil {
					return nil, err
				}
				cpu += mc
				mem += mm
			}
			usedByName[item.Metadata.Name] = [2]int64{cpu, mem}
		}
	}

	var boxes []Sandbox
	for _, item := range sb.Items {
		b := Sandbox{Name: item.Metadata.Name, Phase: "…", Created: parseTime(item.Metadata.Created)}
		if len(item.Metadata.OwnerReferences) > 0 {
			b.OwnerKind = item.Metadata.OwnerReferences[0].Kind
			b.Owner = item.Metadata.OwnerReferences[0].Name
		}
		if p := podByName[item.Metadata.Name]; p != nil {
			b.Phase = p.Status.Phase
			if ts := parseTime(p.Metadata.Created); !ts.IsZero() {
				b.Created = ts
			}
			for _, c := range p.Spec.Containers {
				if q, ok := c.Resources.Limits["cpu"]; ok {
					v, err := cpuMillicores(q)
					if err != nil {
						return nil, err
					}
					b.CPULimitMC += v
				}
				if q, ok := c.Resources.Limits["memory"]; ok {
					v, err := memMiB(q)
					if err != nil {
						return nil, err
					}
					b.MemLimitMiB += v
				}
			}
		}
		if u, ok := usedByName[item.Metadata.Name]; ok {
			b.CPUUsageMC, b.MemUsageMiB = u[0], u[1]
		}
		boxes = append(boxes, b)
	}
	// Warm sandboxes first: they are the ones available to claim.
	rank := func(kind string) int {
		switch kind {
		case "SandboxWarmPool":
			return 0
		case "SandboxClaim":
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(boxes, func(i, j int) bool {
		if ri, rj := rank(boxes[i].OwnerKind), rank(boxes[j].OwnerKind); ri != rj {
			return ri < rj
		}
		return boxes[i].Name < boxes[j].Name
	})
	return boxes, nil
}

// Fetch reads the cluster once. A metrics failure degrades to budgets
// without usage rather than failing the dashboard.
func Fetch() (boxes []Sandbox, metricsOK bool, err error) {
	raw, err := kubectlOutput("get", "sandboxes", "-o", "json")
	if err != nil {
		return nil, false, err
	}
	var sb sandboxList
	if err := json.Unmarshal(raw, &sb); err != nil {
		return nil, false, fmt.Errorf("parse sandboxes: %w", err)
	}

	raw, err = kubectlOutput("get", "pods", "-o", "json")
	if err != nil {
		return nil, false, err
	}
	var pl podList
	if err := json.Unmarshal(raw, &pl); err != nil {
		return nil, false, fmt.Errorf("parse pods: %w", err)
	}

	var ml *podMetricsList
	raw, merr := kubectlOutput("get", "--raw",
		"/apis/metrics.k8s.io/v1beta1/namespaces/"+Namespace+"/pods")
	if merr == nil {
		var list podMetricsList
		if jerr := json.Unmarshal(raw, &list); jerr == nil {
			ml = &list
		}
	}

	boxes, err = buildSandboxes(sb, pl, ml)
	return boxes, ml != nil, err
}

func kubectlOutput(args ...string) ([]byte, error) {
	return runKubectl(nil, args...)
}

// runKubectl shells out to kubectl, optionally piping stdin.
func runKubectl(stdin []byte, args ...string) ([]byte, error) {
	full := append([]string{"--context", KubeContext, "-n", Namespace}, args...)
	cmd := exec.Command("kubectl", full...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("kubectl %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.Bytes(), nil
}

func parseTime(s string) time.Time {
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return ts
}

// Claim claims a warm sandbox from the pool and returns the claim name.
// Deleting the claim destroys the sandbox and the pool restocks.
func Claim() (string, error) {
	name := claimName()
	if _, err := runKubectl([]byte(claimManifest(name)), "apply", "-f", "-"); err != nil {
		return "", err
	}
	return name, nil
}

// WaitForClaim polls until the claim adopts a warm sandbox and returns
// its pod name, so the caller can start bash before the user asks for
// it. Warm adoption is near-instant; the timeout covers a cold refill.
func WaitForClaim(name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := kubectlOutput("get", "sandboxclaim", name,
			"-o", "jsonpath={.status.sandbox.name}")
		if err == nil {
			if pod := strings.TrimSpace(string(out)); pod != "" {
				return pod, nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("claim %s adopted no sandbox within %s", name, timeout)
}

// DeleteClaim deletes a SandboxClaim, tearing its sandbox down.
func DeleteClaim(name string) error {
	_, err := kubectlOutput("delete", "sandboxclaim", name)
	return err
}

func claimName() string {
	return fmt.Sprintf("dash-claim-%d", time.Now().UnixNano())
}

// claimManifest renders the SandboxClaim object Claim applies. Same
// shape as demo/claim.yaml, with a unique name.
func claimManifest(name string) string {
	return fmt.Sprintf(`apiVersion: extensions.agents.x-k8s.io/v1beta1
kind: SandboxClaim
metadata:
  name: %s
  namespace: %s
spec:
  warmPoolRef:
    name: %s
  lifecycle:
    shutdownPolicy: Delete
`, name, Namespace, WarmPool)
}

// cpuMillicores parses the cpu quantity units this demo produces: plain
// cores, "m" millicores from the manifests, and "n" nanocores from the
// metrics API.
func cpuMillicores(q string) (int64, error) {
	s := strings.TrimSpace(q)
	if s == "" {
		return 0, fmt.Errorf("empty cpu quantity")
	}
	mult, div := int64(1000), int64(1)
	switch {
	case strings.HasSuffix(s, "m"):
		s, mult = strings.TrimSuffix(s, "m"), 1
	case strings.HasSuffix(s, "n"):
		s, mult, div = strings.TrimSuffix(s, "n"), 1, 1_000_000
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cpu quantity %q: %w", q, err)
	}
	return v * mult / div, nil
}

// memMiB parses the memory quantity units this demo produces: plain bytes,
// "Ki", "Mi", and "Gi".
func memMiB(q string) (int64, error) {
	s := strings.TrimSpace(q)
	if s == "" {
		return 0, fmt.Errorf("empty memory quantity")
	}
	mult, div := int64(1), int64(1024*1024)
	switch {
	case strings.HasSuffix(s, "Ki"):
		s, div = strings.TrimSuffix(s, "Ki"), 1024
	case strings.HasSuffix(s, "Mi"):
		s, div = strings.TrimSuffix(s, "Mi"), 1
	case strings.HasSuffix(s, "Gi"):
		s, mult, div = strings.TrimSuffix(s, "Gi"), 1024, 1
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("memory quantity %q: %w", q, err)
	}
	return v * mult / div, nil
}
