package live

import (
	"context"
	"sync"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/server"
	"github.com/tmih06/nightshift/internal/services/session"
)

// snapshotTTL is how long a read of the fleet answers the questions that only
// need to know which pods exist.
const snapshotTTL = 2 * time.Second

type snapshot struct {
	boxes     []cluster.Sandbox
	pools     map[string]cluster.Pool
	egress    bool
	metricsOK bool
	at        time.Time
}

// liveUsage is one pod's CPU as a rate between two cgroup samples.
type liveUsage struct {
	base  cluster.CgroupSample
	cpu   int64
	cpuOK bool
}

// Fleet reads the sandbox fleet and remembers what it needs between reads:
// the cgroup samples that make a CPU rate, and each pod's agent version.
type Fleet struct {
	mu       sync.Mutex
	snap     snapshot
	live     map[string]liveUsage
	versions map[string]string
}

// NewFleet returns a Fleet with nothing remembered.
func NewFleet() *Fleet {
	return &Fleet{live: map[string]liveUsage{}, versions: map[string]string{}}
}

// Fleet reads the fleet as it is now.
func (f *Fleet) Fleet(ctx context.Context, inc server.Include) (v1.Fleet, error) {
	snap, err := f.refresh(ctx)
	if err != nil {
		return v1.Fleet{}, err
	}
	samples := map[string]cluster.CgroupSample{}
	if inc.Live {
		samples = f.sample(ctx, snap.boxes)
	}
	if inc.Versions {
		f.probe(ctx, snap.boxes)
	}
	targets := f.targets(ctx, snap.boxes)
	var terms map[string][]session.Terminal
	if inc.Terminals {
		if got, err := call(ctx, func() (map[string][]session.Terminal, error) { return session.LiveTerminals(snap.boxes), nil }); err == nil {
			terms = got
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.rates(snap.boxes, samples)
	out := v1.Fleet{ObservedAt: snap.at.UTC(), Egress: snap.egress, Pods: make([]v1.Pod, 0, len(snap.boxes)), Pools: poolsOf(snap.pools, inc.Configs)}
	for i := range snap.boxes {
		b := &snap.boxes[i]
		p := podOf(b, inc.Configs)
		p.Target = targets[b.Volume]
		p.AgentVersion = f.versions[b.Name]
		p.Usage = f.usageOf(b, samples, snap.metricsOK)
		for _, x := range terms[b.Name] {
			p.Terminals = append(p.Terminals, terminalOf(x))
		}
		out.Pods = append(out.Pods, p)
	}
	return out, nil
}

// refresh reads the cluster and remembers what it saw.
func (f *Fleet) refresh(ctx context.Context) (snapshot, error) {
	type read struct {
		boxes     []cluster.Sandbox
		pools     map[string]cluster.Pool
		metricsOK bool
	}
	r, err := call(ctx, func() (read, error) {
		boxes, pools, metricsOK, err := cluster.Fetch()
		return read{boxes: boxes, pools: pools, metricsOK: metricsOK}, wrapErr("read the cluster", err)
	})
	if err != nil {
		return snapshot{}, err
	}
	snap := snapshot{boxes: r.boxes, pools: r.pools, metricsOK: r.metricsOK, egress: egressOf(r.boxes), at: time.Now()}
	f.mu.Lock()
	f.snap = snap
	f.mu.Unlock()
	return snap, nil
}

// egressOf is whether the egress proxy's pod is among the boxes.
func egressOf(boxes []cluster.Sandbox) bool {
	for i := range boxes {
		if boxes[i].PodRole() == cluster.RoleEgress {
			return true
		}
	}
	return false
}

// current is the fleet as of the last read, or a fresh read when that is old.
func (f *Fleet) current(ctx context.Context) (snapshot, error) {
	f.mu.Lock()
	snap := f.snap
	f.mu.Unlock()
	if time.Since(snap.at) < snapshotTTL {
		return snap, nil
	}
	return f.refresh(ctx)
}

// Claimed is whether pod is a running sandbox that someone holds: the only
// pods whose terminals the API will open.
func (f *Fleet) Claimed(ctx context.Context, pod string) (bool, error) {
	snap, err := f.current(ctx)
	if err != nil {
		return false, err
	}
	if has(snap, pod) {
		return true, nil
	}
	// The pod may be newer than the snapshot.
	if snap, err = f.refresh(ctx); err != nil {
		return false, err
	}
	return has(snap, pod), nil
}

func has(snap snapshot, pod string) bool {
	for i := range snap.boxes {
		if b := &snap.boxes[i]; b.Name == pod && b.Claimed() && b.Phase == cluster.PodPhaseRunning {
			return true
		}
	}
	return false
}

// PodLogs reads a pod's log, for the pods of the fleet and no others.
func (f *Fleet) PodLogs(ctx context.Context, namespace, pod string, tail int) ([]string, error) {
	snap, err := f.current(ctx)
	if err != nil {
		return nil, err
	}
	known := false
	for i := range snap.boxes {
		if b := &snap.boxes[i]; b.Name == pod && b.EffectiveNamespace() == namespace {
			known = true
		}
	}
	if !known {
		return nil, &notFoundError{what: "pod " + namespace + "/" + pod}
	}
	text, err := call(ctx, func() (string, error) { return cluster.PodLogs(namespace, pod, tail) })
	if err != nil {
		return nil, err
	}
	return splitLines(text), nil
}

// sample reads the cgroups of the claimed, running pods.
func (f *Fleet) sample(ctx context.Context, boxes []cluster.Sandbox) map[string]cluster.CgroupSample {
	var pods []string
	for i := range boxes {
		if boxes[i].Claimed() && boxes[i].Phase == cluster.PodPhaseRunning {
			pods = append(pods, boxes[i].Name)
		}
	}
	got, err := call(ctx, func() (map[string]cluster.CgroupSample, error) { return cluster.ReadCgroups(pods), nil })
	if err != nil {
		return nil
	}
	return got
}

// rates folds samples into each pod's CPU rate. Memory is live from the first
// sample, CPU from the second, as it is a rate between two. The caller holds mu.
func (f *Fleet) rates(boxes []cluster.Sandbox, samples map[string]cluster.CgroupSample) {
	next := make(map[string]liveUsage, len(samples))
	for i := range boxes {
		name := boxes[i].Name
		s, ok := samples[name]
		if !ok {
			continue
		}
		st, seen := f.live[name]
		switch {
		case !seen:
			st = liveUsage{base: s}
		case s.Uptime < st.base.Uptime || s.CPUUsec < st.base.CPUUsec:
			st = liveUsage{base: s} // restarted: start over
		default:
			if mc, ok := cluster.CPURate(st.base, s); ok {
				st.cpu, st.cpuOK, st.base = mc, true, s
			}
		}
		next[name] = st
	}
	f.live = next
}

// usageOf is what a pod uses: its own counters when it has them, else the
// cluster's metrics, else nothing. The caller holds mu.
func (f *Fleet) usageOf(b *cluster.Sandbox, samples map[string]cluster.CgroupSample, metricsOK bool) *v1.Usage {
	if s, ok := samples[b.Name]; ok {
		u := &v1.Usage{CPUMillis: b.CPUUsageMC, MemMiB: s.MemMiB(), Live: true}
		if st := f.live[b.Name]; st.cpuOK {
			u.CPUMillis = st.cpu
		} else if !metricsOK {
			return &v1.Usage{MemMiB: s.MemMiB(), Live: true}
		}
		return u
	}
	if !metricsOK {
		return nil
	}
	return &v1.Usage{CPUMillis: b.CPUUsageMC, MemMiB: b.MemUsageMiB}
}

// probe asks each running sandbox's coding CLI for its version, once per pod.
func (f *Fleet) probe(ctx context.Context, boxes []cluster.Sandbox) {
	f.mu.Lock()
	var unprobed []string
	alive := make(map[string]bool, len(boxes))
	for i := range boxes {
		b := &boxes[i]
		alive[b.Name] = true
		if _, ok := f.versions[b.Name]; !ok && b.IsSandbox() && b.Phase == cluster.PodPhaseRunning {
			unprobed = append(unprobed, b.Name)
		}
	}
	for pod := range f.versions {
		if !alive[pod] {
			delete(f.versions, pod)
		}
	}
	f.mu.Unlock()
	if len(unprobed) == 0 {
		return
	}
	got, err := call(ctx, func() (map[string]string, error) { return cluster.AgentVersions(unprobed), nil })
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for pod, v := range got {
		f.versions[pod] = v
	}
}

// targets reads what each volume keeps, for the pods that mount one.
func (f *Fleet) targets(ctx context.Context, boxes []cluster.Sandbox) map[string]*v1.Target {
	want := false
	for i := range boxes {
		want = want || boxes[i].Volume != ""
	}
	if !want {
		return nil
	}
	vols, err := call(ctx, cluster.ListVolumes)
	if err != nil {
		return nil
	}
	out := make(map[string]*v1.Target, len(vols))
	for _, v := range vols {
		t := targetOf(v.For)
		out[v.Name] = &t
	}
	return out
}

func targetOf(vf cluster.VolumeFor) v1.Target {
	t := v1.Target{Repo: vf.Repo, Number: vf.Number, Branch: vf.Branch}
	switch vf.Kind {
	case cluster.VolumeIssue:
		t.Kind = v1.TargetIssue
	case cluster.VolumeRepo:
		t.Kind = v1.TargetBranch
	default:
		t.Kind = v1.TargetPullRequest
	}
	return t
}

func poolsOf(pools map[string]cluster.Pool, configs bool) []v1.Pool {
	out := make([]v1.Pool, 0, len(pools))
	for _, p := range pools {
		vp := v1.Pool{
			Name: p.Name, Template: p.Template, Ready: p.Ready, Desired: p.Desired,
			UpdateStrategy: p.UpdateStrategy,
			Harness:        checkout.ForPool(p.Name, p.Template, p.TemplateYAML),
		}
		if configs {
			vp.Configs = &v1.PoolConfigs{Pool: p.ManifestYAML, Template: p.TemplateYAML}
		}
		out = append(out, vp)
	}
	sortPools(out)
	return out
}
