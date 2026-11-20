package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	KubeContext = "kind-agent-sandbox"
	Namespace   = "sandbox-demo"
	WarmPool    = "pi-pool"
)

type Sandbox struct {
	Name        string
	OwnerKind   string // SandboxWarmPool or SandboxClaim
	Owner       string
	Template    string
	NodeName    string
	Phase       string // pod phase, "…" until the pod exists
	Created     time.Time
	CPULimitMC  int64
	MemLimitMiB int64
	CPUUsageMC  int64
	MemUsageMiB int64

	Image         string
	Command       string
	RestartPolicy string
	CPUReqMC      int64
	MemReqMiB     int64

	ManifestYAML string
	PodYAML      string
}

type Pool struct {
	Name           string
	Template       string
	Ready, Desired int
	UpdateStrategy string
	ManifestYAML   string
	TemplateYAML   string
}

type ownerReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type sandboxItem struct {
	Metadata struct {
		Name            string            `json:"name"`
		Annotations     map[string]string `json:"annotations"`
		Created         string            `json:"creationTimestamp"`
		OwnerReferences []ownerReference  `json:"ownerReferences"`
	} `json:"metadata"`
}

type sandboxList struct {
	Items []json.RawMessage `json:"items"`
}

type podContainer struct {
	Image     string   `json:"image"`
	Command   []string `json:"command"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
}

type podItem struct {
	Metadata struct {
		Name    string `json:"name"`
		Created string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeName      string         `json:"nodeName"`
		RestartPolicy string         `json:"restartPolicy"`
		Containers    []podContainer `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type podList struct {
	Items []json.RawMessage `json:"items"`
}

type warmPool struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Replicas int `json:"replicas"`
		Template struct {
			Name string `json:"name"`
		} `json:"sandboxTemplateRef"`
		UpdateStrategy struct {
			Type string `json:"type"`
		} `json:"updateStrategy"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

type rawList struct {
	Items []json.RawMessage `json:"items"`
}

type kindMeta struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
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

// buildSandboxes joins sandboxes, pods, and optional metrics by pod name.
func buildSandboxes(sb sandboxList, pl podList, ml *podMetricsList) ([]Sandbox, error) {
	type parsedPod struct {
		item podItem
		raw  json.RawMessage
	}
	podByName := make(map[string]*parsedPod, len(pl.Items))
	for _, raw := range pl.Items {
		var p podItem
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("parse pod item: %w", err)
		}
		podByName[p.Metadata.Name] = &parsedPod{item: p, raw: raw}
	}

	var usedByName map[string][2]int64
	if ml != nil {
		usedByName = make(map[string][2]int64, len(ml.Items))
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

	boxes := make([]Sandbox, 0, len(sb.Items))
	for _, raw := range sb.Items {
		var item sandboxItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("parse sandbox item: %w", err)
		}
		b := Sandbox{
			Name:     item.Metadata.Name,
			Template: item.Metadata.Annotations["agents.x-k8s.io/sandbox-template-ref"],
			Phase:    "…",
			Created:  parseTime(item.Metadata.Created),
		}
		if len(item.Metadata.OwnerReferences) > 0 {
			b.OwnerKind = item.Metadata.OwnerReferences[0].Kind
			b.Owner = item.Metadata.OwnerReferences[0].Name
		}
		isWarm := b.OwnerKind != "SandboxClaim"
		if isWarm {
			b.ManifestYAML = cleanManifestYAML(raw)
		}
		if pp := podByName[item.Metadata.Name]; pp != nil {
			p := &pp.item
			b.NodeName = p.Spec.NodeName
			b.Phase = p.Status.Phase
			b.RestartPolicy = p.Spec.RestartPolicy
			if isWarm {
				b.PodYAML = cleanManifestYAML(pp.raw)
			}
			if ts := parseTime(p.Metadata.Created); !ts.IsZero() {
				b.Created = ts
			}
			for _, c := range p.Spec.Containers {
				b.Image = c.Image
				b.Command = strings.Join(c.Command, " ")
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
				if q, ok := c.Resources.Requests["cpu"]; ok {
					v, err := cpuMillicores(q)
					if err != nil {
						return nil, err
					}
					b.CPUReqMC += v
				}
				if q, ok := c.Resources.Requests["memory"]; ok {
					v, err := memMiB(q)
					if err != nil {
						return nil, err
					}
					b.MemReqMiB += v
				}
			}
		}
		if u, ok := usedByName[item.Metadata.Name]; ok {
			b.CPUUsageMC, b.MemUsageMiB = u[0], u[1]
		}
		boxes = append(boxes, b)
	}

	sort.SliceStable(boxes, func(i, j int) bool {
		if ri, rj := ownerRank(boxes[i].OwnerKind), ownerRank(boxes[j].OwnerKind); ri != rj {
			return ri < rj
		}
		return boxes[i].Name < boxes[j].Name
	})
	return boxes, nil
}

func ownerRank(kind string) int {
	switch kind {
	case "SandboxWarmPool":
		return 0
	case "SandboxClaim":
		return 1
	default:
		return 2
	}
}

// parseClusterList splits a combined kubectl List into sandboxes, pods,
// and the warm pools plus their referenced templates.
func parseClusterList(raw []byte) (sandboxList, podList, map[string]Pool, error) {
	var list rawList
	if err := json.Unmarshal(raw, &list); err != nil {
		return sandboxList{}, podList{}, nil, fmt.Errorf("parse cluster list: %w", err)
	}
	var (
		sb        sandboxList
		pl        podList
		pools     = map[string]Pool{}
		templates = map[string]json.RawMessage{}
	)
	for _, item := range list.Items {
		var meta kindMeta
		if err := json.Unmarshal(item, &meta); err != nil {
			continue
		}
		switch meta.Kind {
		case "Sandbox":
			sb.Items = append(sb.Items, item)
		case "Pod":
			pl.Items = append(pl.Items, item)
		case "SandboxWarmPool":
			var wp warmPool
			if json.Unmarshal(item, &wp) == nil {
				pools[wp.Metadata.Name] = Pool{
					Name:           wp.Metadata.Name,
					Template:       wp.Spec.Template.Name,
					Ready:          wp.Status.ReadyReplicas,
					Desired:        wp.Spec.Replicas,
					UpdateStrategy: wp.Spec.UpdateStrategy.Type,
					ManifestYAML:   cleanManifestYAML(item),
				}
			}
		case "SandboxTemplate":
			templates[meta.Metadata.Name] = item
		}
	}
	for name, pool := range pools {
		if rawTpl, ok := templates[pool.Template]; ok {
			pool.TemplateYAML = cleanManifestYAML(rawTpl)
			pools[name] = pool
		}
	}
	return sb, pl, pools, nil
}

// Fetch reads cluster objects and pod metrics concurrently in two
// kubectl calls. Metrics degrade gracefully when unavailable.
func Fetch() ([]Sandbox, map[string]Pool, bool, error) {
	var (
		resRaw, metRaw []byte
		resErr, metErr error
		wg             sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		resRaw, resErr = runKubectl(nil, "get", "sandboxes,pods,sandboxwarmpools,sandboxtemplates", "-o", "json")
	}()
	go func() {
		defer wg.Done()
		metRaw, metErr = runKubectl(nil, "get", "--raw",
			"/apis/metrics.k8s.io/v1beta1/namespaces/"+Namespace+"/pods")
	}()
	wg.Wait()

	if resErr != nil {
		return nil, nil, false, resErr
	}
	sb, pl, pool, err := parseClusterList(resRaw)
	if err != nil {
		return nil, nil, false, err
	}

	var ml *podMetricsList
	if metErr == nil {
		var list podMetricsList
		if json.Unmarshal(metRaw, &list) == nil {
			ml = &list
		}
	}
	boxes, err := buildSandboxes(sb, pl, ml)
	return boxes, pool, ml != nil, err
}

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

// cleanManifestYAML formats a Kubernetes JSON object as readable YAML
// with server-side boilerplate removed.
func cleanManifestYAML(raw []byte) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
		delete(meta, "resourceVersion")
		delete(meta, "uid")
		if ann, ok := meta["annotations"].(map[string]any); ok {
			delete(ann, "kubectl.kubernetes.io/last-applied-configuration")
			if len(ann) == 0 {
				delete(meta, "annotations")
			}
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(obj); err != nil {
		return ""
	}
	_ = enc.Close()
	return strings.TrimRight(buf.String(), "\n")
}

func Claim(pool string) (string, error) {
	name := fmt.Sprintf("dash-claim-%d", time.Now().UnixNano())
	if _, err := runKubectl([]byte(claimManifest(name, pool)), "apply", "-f", "-"); err != nil {
		return "", err
	}
	return name, nil
}

func WaitForClaim(name string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := runKubectl(nil, "get", "sandboxclaim", name,
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

func DeleteClaim(name string) error {
	_, err := runKubectl(nil, "delete", "sandboxclaim", name)
	return err
}

func claimManifest(name, pool string) string {
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
`, name, Namespace, pool)
}

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
