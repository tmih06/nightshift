package fleet

import (
	v1 "github.com/tmih06/nightshift/api/v1"
)

// stageOf is the startup stage the contract's name stands for.
func stageOf(name string) StartupStage {
	switch name {
	case v1.StagePulling:
		return StagePulling
	case v1.StageStarting:
		return StageStarting
	case v1.StageReadying:
		return StageReadying
	}
	return StageScheduling
}

// Load is the fleet's pods and pools as the screens use them, from the API's
// answer. The pods keep the order the server sent them in.
func Load(f v1.Fleet) (boxes []Sandbox, pools map[string]Pool) {
	boxes = make([]Sandbox, 0, len(f.Pods))
	for i := range f.Pods {
		boxes = append(boxes, fromPod(&f.Pods[i]))
	}
	pools = make(map[string]Pool, len(f.Pools))
	for i := range f.Pools {
		p := &f.Pools[i]
		fp := Pool{
			Name: p.Name, Template: p.Template, Ready: p.Ready, Desired: p.Desired,
			UpdateStrategy: p.UpdateStrategy, Harness: p.Harness,
		}
		if p.Configs != nil {
			fp.ManifestYAML, fp.TemplateYAML = p.Configs.Pool, p.Configs.Template
		}
		pools[p.Name] = fp
	}
	return boxes, pools
}

func fromPod(p *v1.Pod) Sandbox {
	b := Sandbox{
		Name: p.Name, Namespace: p.Namespace, OwnerKind: p.Owner.Kind, Owner: p.Owner.Name,
		Volume: p.Volume, Target: p.Target, Template: p.Template, NodeName: p.Node,
		Phase: p.Phase, PodIP: p.IP, Ready: p.Ready, Restarts: p.Restarts, Waiting: p.Waiting,
		Ports: p.Ports, Container: p.Container, Created: p.CreatedAt,
		CPULimitMC: p.Resources.CPULimitMillis, MemLimitMiB: p.Resources.MemLimitMiB,
		CPUReqMC: p.Resources.CPURequestMillis, MemReqMiB: p.Resources.MemRequestMiB,
		Image: p.Image, Command: p.Command, RestartPolicy: p.RestartPolicy,
		AgentVersion: p.AgentVersion, Terminals: p.Terminals, Role: p.Role,
	}
	if b.Phase == "" {
		b.Phase = "…" // the pod does not exist yet
	}
	if u := p.Usage; u != nil {
		b.CPUUsageMC, b.MemUsageMiB, b.UsageKnown = u.CPUMillis, u.MemMiB, true
	}
	if s := p.Startup; s != nil {
		b.Startup = &Startup{Stage: stageOf(s.Stage), Since: s.Since, Err: s.Error, Detail: s.Detail, PullFails: s.PullFailures}
	}
	if c := p.Configs; c != nil {
		b.ManifestYAML, b.PodYAML, b.ExtraYAML = c.Sandbox, c.Pod, c.Extra
	}
	return b
}
