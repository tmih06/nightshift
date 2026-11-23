package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
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
	Namespace   = "nightshift"
	WarmPool    = "pi-pool"
)

type Sandbox struct {
	Name        string
	Namespace   string // Kubernetes namespace; empty defaults to Namespace ("nightshift")
	OwnerKind   string // SandboxWarmPool, SandboxClaim, ReplicaSet, Job, etc.; empty on a volume
	Owner       string // the pool, claim, workload, or volume
	Volume      string // the pull request volume it mounts, if Nightshift started it on one
	Template    string
	NodeName    string
	Phase       string // pod phase, "…" until the pod exists
	PodIP       string
	Ready       string // ready container fraction, e.g. "1/1"
	Restarts    int
	Waiting     string // container waiting reason when unhealthy
	Ports       []string
	Container   string
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
	ExtraYAML    string // extra markdown YAML sections (e.g. egress policy/service/netpol)
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
		Labels          map[string]string `json:"labels"`
		Annotations     map[string]string `json:"annotations"`
		Created         string            `json:"creationTimestamp"`
		OwnerReferences []ownerReference  `json:"ownerReferences"`
	} `json:"metadata"`
}

type sandboxList struct {
	Items []json.RawMessage `json:"items"`
}

type podPort struct {
	Name          string `json:"name"`
	ContainerPort int    `json:"containerPort"`
}

type podContainer struct {
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Command   []string  `json:"command"`
	Ports     []podPort `json:"ports"`
	Resources struct {
		Requests map[string]string `json:"requests"`
		Limits   map[string]string `json:"limits"`
	} `json:"resources"`
}

type podContainerStatus struct {
	Ready        bool `json:"ready"`
	RestartCount int  `json:"restartCount"`
	State        struct {
		Waiting *struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
	} `json:"state"`
}

type podItem struct {
	Metadata struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		Labels          map[string]string `json:"labels"`
		Created         string            `json:"creationTimestamp"`
		OwnerReferences []ownerReference  `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		NodeName      string         `json:"nodeName"`
		RestartPolicy string         `json:"restartPolicy"`
		Containers    []podContainer `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string               `json:"phase"`
		PodIP             string               `json:"podIP"`
		ContainerStatuses []podContainerStatus `json:"containerStatuses"`
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
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Containers []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

type parsedPod struct {
	item podItem
	raw  json.RawMessage
}

func parsePodIndex(pl podList) (map[string]*parsedPod, []*parsedPod, error) {
	byKey := make(map[string]*parsedPod, len(pl.Items))
	ordered := make([]*parsedPod, 0, len(pl.Items))
	for _, raw := range pl.Items {
		var p podItem
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, nil, fmt.Errorf("parse pod item: %w", err)
		}
		pp := &parsedPod{item: p, raw: raw}
		byKey[podKey(p.Metadata.Namespace, p.Metadata.Name)] = pp
		ordered = append(ordered, pp)
	}
	return byKey, ordered, nil
}

func parseMetricsIndex(ml *podMetricsList) (map[string][2]int64, error) {
	if ml == nil {
		return nil, nil
	}
	usedByKey := make(map[string][2]int64, len(ml.Items))
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
		usedByKey[podKey(item.Metadata.Namespace, item.Metadata.Name)] = [2]int64{cpu, mem}
	}
	return usedByKey, nil
}

func applyPodSpec(b *Sandbox, pp *parsedPod) error {
	p := &pp.item
	b.NodeName = p.Spec.NodeName
	b.Phase = p.Status.Phase
	b.PodIP = p.Status.PodIP
	b.Ready, b.Restarts, b.Waiting = summarizeContainers(p.Status.ContainerStatuses)
	b.Ports = collectPorts(p.Spec.Containers)
	b.RestartPolicy = p.Spec.RestartPolicy
	b.PodYAML = cleanManifestYAML(pp.raw)
	if ts := parseTime(p.Metadata.Created); !ts.IsZero() {
		b.Created = ts
	}
	for _, c := range p.Spec.Containers {
		if b.Container == "" {
			b.Container = c.Name
		}
		b.Image = c.Image
		b.Command = strings.Join(c.Command, " ")
		if err := addContainerResources(b, c); err != nil {
			return err
		}
	}
	return nil
}

func addContainerResources(b *Sandbox, c podContainer) error {
	if q, ok := c.Resources.Limits["cpu"]; ok {
		v, err := cpuMillicores(q)
		if err != nil {
			return err
		}
		b.CPULimitMC += v
	}
	if q, ok := c.Resources.Limits["memory"]; ok {
		v, err := memMiB(q)
		if err != nil {
			return err
		}
		b.MemLimitMiB += v
	}
	if q, ok := c.Resources.Requests["cpu"]; ok {
		v, err := cpuMillicores(q)
		if err != nil {
			return err
		}
		b.CPUReqMC += v
	}
	if q, ok := c.Resources.Requests["memory"]; ok {
		v, err := memMiB(q)
		if err != nil {
			return err
		}
		b.MemReqMiB += v
	}
	return nil
}

func buildSandboxItem(raw json.RawMessage, podByKey map[string]*parsedPod, usedByKey map[string][2]int64) (Sandbox, error) {
	var item sandboxItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return Sandbox{}, fmt.Errorf("parse sandbox item: %w", err)
	}
	b := Sandbox{
		Name:         item.Metadata.Name,
		Namespace:    Namespace,
		Template:     item.Metadata.Annotations["agents.x-k8s.io/sandbox-template-ref"],
		Phase:        "…",
		Created:      parseTime(item.Metadata.Created),
		ManifestYAML: cleanManifestYAML(raw),
	}
	if len(item.Metadata.OwnerReferences) > 0 {
		b.OwnerKind = item.Metadata.OwnerReferences[0].Kind
		b.Owner = item.Metadata.OwnerReferences[0].Name
	} else if v := item.Metadata.Labels[VolumeLabel]; v != "" {
		b.Volume, b.Owner = v, v
	}
	key := podKey(Namespace, item.Metadata.Name)
	if pp := podByKey[key]; pp != nil {
		if err := applyPodSpec(&b, pp); err != nil {
			return Sandbox{}, err
		}
	}
	if u, ok := usedByKey[key]; ok {
		b.CPUUsageMC, b.MemUsageMiB = u[0], u[1]
	}
	return b, nil
}

func buildInfraPod(pp *parsedPod, usedByKey map[string][2]int64) (Sandbox, error) {
	p := &pp.item
	b := Sandbox{
		Name:      p.Metadata.Name,
		Namespace: p.Metadata.Namespace,
		Phase:     "…",
		Created:   parseTime(p.Metadata.Created),
	}
	if len(p.Metadata.OwnerReferences) > 0 {
		b.OwnerKind = p.Metadata.OwnerReferences[0].Kind
		b.Owner = p.Metadata.OwnerReferences[0].Name
	} else if app := p.Metadata.Labels["app"]; app != "" {
		b.Owner = app
	}
	if err := applyPodSpec(&b, pp); err != nil {
		return Sandbox{}, err
	}
	if u, ok := usedByKey[podKey(b.Namespace, b.Name)]; ok {
		b.CPUUsageMC, b.MemUsageMiB = u[0], u[1]
	}
	return b, nil
}

// buildSandboxes joins sandboxes, essential cluster pods, and optional metrics.
func buildSandboxes(sb sandboxList, pl podList, ml *podMetricsList) ([]Sandbox, error) {
	podByKey, orderedPods, err := parsePodIndex(pl)
	if err != nil {
		return nil, err
	}
	usedByKey, err := parseMetricsIndex(ml)
	if err != nil {
		return nil, err
	}

	matched := make(map[string]bool, len(sb.Items))
	boxes := make([]Sandbox, 0, len(sb.Items)+len(orderedPods))
	for _, raw := range sb.Items {
		b, err := buildSandboxItem(raw, podByKey, usedByKey)
		if err != nil {
			return nil, err
		}
		matched[podKey(Namespace, b.Name)] = true
		boxes = append(boxes, b)
	}

	for _, pp := range orderedPods {
		ns := pp.item.Metadata.Namespace
		if ns == "" || matched[podKey(ns, pp.item.Metadata.Name)] || !isEssentialPod(ns, pp.item.Metadata.Name) {
			continue
		}
		b, err := buildInfraPod(pp, usedByKey)
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, b)
	}

	sort.SliceStable(boxes, func(i, j int) bool {
		if ri, rj := OwnerRank(boxes[i]), OwnerRank(boxes[j]); ri != rj {
			return ri < rj
		}
		return boxes[i].Name < boxes[j].Name
	})
	return boxes, nil
}

// Claimed is whether b is someone's workspace: adopted by a claim, or
// started by Nightshift on a pull request's volume.
func (b Sandbox) Claimed() bool { return b.OwnerKind == ownerKindClaim || b.Volume != "" }

// OwnerRank orders warm sandboxes, then claimed, then egress, system, jobs, and other.
func OwnerRank(b Sandbox) int {
	switch b.PodRole() {
	case RoleWarm:
		return 0
	case RoleClaimed:
		return 1
	case RoleEgress:
		return 2
	case RoleSystem:
		return 3
	case RoleJob:
		return 4
	default:
		return 5
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

// Fetch reads sandbox objects, essential cluster pods, pod metrics, and egress
// proxy configuration concurrently. Metrics and egress configs degrade
// gracefully when unavailable.
func Fetch() ([]Sandbox, map[string]Pool, bool, error) {
	var (
		resRaw, podRaw, metRaw, egressRaw []byte
		resErr, podErr, metErr, egressErr error
		wg                                sync.WaitGroup
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		resRaw, resErr = runKubectl(nil, "get", "sandboxes,sandboxwarmpools,sandboxtemplates", "-o", "json")
	}()
	go func() {
		defer wg.Done()
		podRaw, podErr = runKubectl(nil, "get", "pods", "-A", "-o", "json")
	}()
	go func() {
		defer wg.Done()
		metRaw, metErr = runKubectl(nil, "get", "--raw", "/apis/metrics.k8s.io/v1beta1/pods")
	}()
	go func() {
		defer wg.Done()
		egressRaw, egressErr = runKubectl(nil, "-n", EgressNamespace, "get", "configmap,service,networkpolicy", "-o", "json")
	}()
	wg.Wait()

	if resErr != nil {
		return nil, nil, false, resErr
	}
	if podErr != nil {
		return nil, nil, false, podErr
	}
	sb, _, pool, err := parseClusterList(resRaw)
	if err != nil {
		return nil, nil, false, err
	}
	var pl podList
	if err := json.Unmarshal(podRaw, &pl); err != nil {
		return nil, nil, false, fmt.Errorf("parse pods list: %w", err)
	}

	var ml *podMetricsList
	if metErr == nil {
		var list podMetricsList
		if json.Unmarshal(metRaw, &list) == nil {
			ml = &list
		}
	}
	boxes, err := buildSandboxes(sb, pl, ml)
	if err != nil {
		return nil, nil, false, err
	}
	if egressErr == nil {
		if extra := parseEgressConfigSections(egressRaw); extra != "" {
			for i := range boxes {
				if boxes[i].PodRole() == RoleEgress {
					boxes[i].ExtraYAML = extra
				}
			}
		}
	}
	return boxes, pool, ml != nil, nil
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
		return 0, errors.New("empty cpu quantity")
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
		return 0, errors.New("empty memory quantity")
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
