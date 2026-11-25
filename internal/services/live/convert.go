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

var roles = map[cluster.PodRole]string{
	cluster.RoleWarm:    v1.RoleWarm,
	cluster.RoleClaimed: v1.RoleClaimed,
	cluster.RoleEgress:  v1.RoleEgress,
	cluster.RoleSystem:  v1.RoleSystem,
	cluster.RoleJob:     v1.RoleJob,
	cluster.RoleOther:   v1.RoleOther,
}

var stages = map[cluster.StartupStage]string{
	cluster.StageScheduling: v1.StageScheduling,
	cluster.StagePulling:    v1.StagePulling,
	cluster.StageStarting:   v1.StageStarting,
	cluster.StageReadying:   v1.StageReadying,
}

// podOf is the contract's pod for one of the cluster's.
func podOf(b *cluster.Sandbox, configs bool) v1.Pod {
	phase := b.Phase
	if phase == "…" {
		phase = "" // the pod does not exist yet
	}
	p := v1.Pod{
		Name: b.Name, Namespace: b.EffectiveNamespace(), Role: roles[b.PodRole()],
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
			Stage: stages[st.Stage], Since: st.Since.UTC(),
			Error: st.Err, Detail: st.Detail, PullFailures: st.PullFails,
		}
	}
	if configs {
		p.Configs = &v1.Configs{Sandbox: b.ManifestYAML, Pod: b.PodYAML, Extra: b.ExtraYAML}
	}
	return p
}
