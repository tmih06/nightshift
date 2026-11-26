package cluster

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// ControllerNamespace hosts the agent-sandbox controller.
	ControllerNamespace = "agent-sandbox-system"

	// KubeSystemNamespace hosts essential cluster services like metrics-server.
	KubeSystemNamespace = "kube-system"

	// StorageNamespace hosts local-path-provisioner for checkout PVCs.
	StorageNamespace = "local-path-storage"

	// PodPhaseRunning is the Kubernetes Running pod phase.
	PodPhaseRunning = "Running"

	defaultLogTail  = 200
	ownerKindClaim  = "SandboxClaim"
	ownerKindPool   = "SandboxWarmPool"
	ownerKindJob    = "Job"
	rootCAConfigMap = "kube-root-ca.crt"
)

// PodRole classifies an essential pod displayed in Nightshift's Pods tab.
type PodRole int

const (
	RoleWarm PodRole = iota
	RoleClaimed
	RoleEgress
	RoleSystem
	RoleJob
	RoleOther
)

// EffectiveNamespace returns b's Kubernetes namespace, defaulting to Namespace
// for fixtures or records that omit it.
func (b Sandbox) EffectiveNamespace() string {
	if b.Namespace != "" {
		return b.Namespace
	}
	return Namespace
}

// Warm is whether b is a warm-pool standby sandbox.
func (b Sandbox) Warm() bool { return b.OwnerKind == ownerKindPool }

// IsSandbox is whether b is a warm or claimed coding sandbox.
func (b Sandbox) IsSandbox() bool { return b.Warm() || b.Claimed() }

// IsInfra is whether b is an essential non-sandbox pod (egress proxy,
// controller, cluster core service, or job).
func (b Sandbox) IsInfra() bool { return !b.IsSandbox() }

// PodRole returns b's operational role in Nightshift's cluster.
func (b Sandbox) PodRole() PodRole {
	switch {
	case b.Claimed():
		return RoleClaimed
	case b.Warm():
		return RoleWarm
	case b.Namespace == EgressNamespace:
		return RoleEgress
	case b.Namespace == ControllerNamespace || b.Namespace == KubeSystemNamespace || b.Namespace == StorageNamespace:
		return RoleSystem
	case b.OwnerKind == ownerKindJob:
		return RoleJob
	default:
		return RoleOther
	}
}

// isEssentialPod filters cluster-wide pods down to Nightshift's namespaces and
// the core services Nightshift directly relies on (metrics-server and
// local-path-provisioner).
func isEssentialPod(namespace, name string) bool {
	switch namespace {
	case Namespace, EgressNamespace, ControllerNamespace:
		return true
	case KubeSystemNamespace:
		return strings.HasPrefix(name, "metrics-server-")
	case StorageNamespace:
		return strings.HasPrefix(name, "local-path-provisioner-")
	default:
		return false
	}
}

// PodLogs returns the recent log lines for pod in namespace.
func PodLogs(namespace, pod string, tailLines int) (string, error) {
	if namespace == "" {
		namespace = Namespace
	}
	if tailLines <= 0 {
		tailLines = defaultLogTail
	}
	out, err := runKubectl(nil, "-n", namespace, "logs", pod, "--tail="+strconv.Itoa(tailLines))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// parseEgressConfigSections formats the nightshift-egress ConfigMap, Service,
// and NetworkPolicy resources into markdown YAML sections for the inspector.
func parseEgressConfigSections(raw []byte) string {
	var list rawList
	if err := json.Unmarshal(raw, &list); err != nil {
		return ""
	}
	type doc struct {
		kind, name, yaml string
	}
	docs := make([]doc, 0, len(list.Items))
	for _, item := range list.Items {
		var meta kindMeta
		if json.Unmarshal(item, &meta) != nil {
			continue
		}
		if meta.Kind == "ConfigMap" && meta.Metadata.Name == rootCAConfigMap {
			continue
		}
		body := cleanManifestYAML(item)
		if body == "" {
			continue
		}
		docs = append(docs, doc{kind: meta.Kind, name: meta.Metadata.Name, yaml: body})
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].kind != docs[j].kind {
			return docs[i].kind < docs[j].kind
		}
		return docs[i].name < docs[j].name
	})
	sections := make([]string, 0, len(docs))
	for _, d := range docs {
		sections = append(sections, "## "+d.kind+" · "+d.name+"\n```yaml\n"+d.yaml+"\n```")
	}
	return strings.Join(sections, "\n\n---\n\n")
}

func summarizeContainers(statuses []podContainerStatus) (ready string, restarts int, waiting string) {
	if len(statuses) == 0 {
		return "", 0, ""
	}
	readyCount := 0
	for _, cs := range statuses {
		if cs.Ready {
			readyCount++
		}
		restarts += cs.RestartCount
		if waiting == "" && cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			waiting = cs.State.Waiting.Reason
		}
	}
	return fmt.Sprintf("%d/%d", readyCount, len(statuses)), restarts, waiting
}

func collectPorts(containers []podContainer) []string {
	var ports []string
	for _, c := range containers {
		for _, p := range c.Ports {
			if p.ContainerPort <= 0 {
				continue
			}
			label := strconv.Itoa(p.ContainerPort)
			if p.Name != "" {
				label += "/" + p.Name
			}
			ports = append(ports, label)
		}
	}
	return ports
}

func podKey(namespace, name string) string {
	if namespace == "" {
		namespace = Namespace
	}
	return namespace + "/" + name
}
