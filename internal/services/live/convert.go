package live

import (
	"sort"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

// notFoundError is a thing that does not exist; the server reports it as 404.
type notFoundError struct{ what string }

func (e *notFoundError) Error() string {
	return "Error from server (NotFound): " + e.what + " not found"
}

func sortPools(ps []v1.Pool) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
}

func splitLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

// role is the contract's name for a pod's role.
func role(r cluster.PodRole) string {
	switch r {
	case cluster.RoleWarm:
		return v1.RoleWarm
	case cluster.RoleClaimed:
		return v1.RoleClaimed
	case cluster.RoleEgress:
		return v1.RoleEgress
	case cluster.RoleSystem:
		return v1.RoleSystem
	case cluster.RoleJob:
		return v1.RoleJob
	case cluster.RoleOther:
	}
	return v1.RoleOther
}

// stage is the contract's name for a startup stage.
func stage(s cluster.StartupStage) string {
	switch s {
	case cluster.StagePulling:
		return v1.StagePulling
	case cluster.StageStarting:
		return v1.StageStarting
	case cluster.StageReadying:
		return v1.StageReadying
	case cluster.StageScheduling:
	}
	return v1.StageScheduling
}

// podOf is the contract's pod for one of the cluster's.
func podOf(b *cluster.Sandbox, configs bool) v1.Pod {
	phase := b.Phase
	if phase == "…" {
		phase = "" // the pod does not exist yet
	}
	p := v1.Pod{
		Name: b.Name, Namespace: b.EffectiveNamespace(), Role: role(b.PodRole()),
		Owner:    v1.OwnerRef{Kind: b.OwnerKind, Name: b.Owner},
		Volume:   b.Volume,
		Template: b.Template, Node: b.NodeName, Phase: phase, IP: b.PodIP,
		Ready: b.Ready, Restarts: b.Restarts, Waiting: b.Waiting,
		Ports: b.Ports, Container: b.Container, CreatedAt: b.Created.UTC(),
		Image: b.Image, Command: b.Command, RestartPolicy: b.RestartPolicy,
		Resources: v1.Resources{
			CPURequestMillis: b.CPUReqMC, CPULimitMillis: b.CPULimitMC,
			MemRequestMiB: b.MemReqMiB, MemLimitMiB: b.MemLimitMiB,
		},
	}
	if st := b.Startup; st != nil {
		p.Startup = &v1.Startup{
			Stage: stage(st.Stage), Since: st.Since.UTC(),
			Error: st.Err, Detail: st.Detail, PullFailures: st.PullFails,
		}
	}
	if configs {
		p.Configs = &v1.Configs{Sandbox: b.ManifestYAML, Pod: b.PodYAML, Extra: b.ExtraYAML}
	}
	return p
}
