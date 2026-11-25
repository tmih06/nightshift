package v1

import "time"

// Pod roles, the values of Pod.Role.
const (
	RoleWarm    = "warm"    // a standby in a warm pool, waiting to be claimed
	RoleClaimed = "claimed" // someone's workspace
	RoleEgress  = "egress"  // the egress proxy
	RoleSystem  = "system"  // cluster services Nightshift relies on
	RoleJob     = "job"     // a clone job
	RoleOther   = "other"
)

// Startup stages, the values of Startup.Stage, in the order a pod passes
// through them.
const (
	StageScheduling = "scheduling"
	StagePulling    = "pulling"
	StageStarting   = "starting"
	StageReadying   = "readying"
)

// Include values for GET /v1/fleet's include parameter.
const (
	IncludeConfigs  = "configs"  // manifests on pods and pools
	IncludeLive     = "live"     // CPU and memory read from each pod's cgroup
	IncludeVersions = "versions" // the coding agent's version in each pod
)

// Fleet is the state of the sandbox fleet at one moment: GET /v1/fleet.
type Fleet struct {
	ObservedAt time.Time `json:"observedAt"`
	// Pods are the sandboxes and the essential cluster pods around them.
	Pods []Pod `json:"pods"`
	// Pools are the warm pools, one per agent.
	Pools []Pool `json:"pools"`
	// Egress is whether the egress proxy is installed.
	Egress bool `json:"egress"`
}

// Pod is one pod of the fleet. It describes; to act, use claims, checkouts,
// and terminals.
type Pod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	// Role is one of the Role constants.
	Role string `json:"role"`
	// Owner is what keeps the pod: a pool, a claim, a job, or a volume.
	Owner OwnerRef `json:"owner"`
	// Volume names the volume the pod mounts, when a checkout started it.
	Volume string `json:"volume,omitempty"`
	// Target is what the volume keeps, when the pod mounts one.
	Target *Target `json:"target,omitempty"`
	// Template is the SandboxTemplate the pod was made from.
	Template string `json:"template,omitempty"`
	Node     string `json:"node,omitempty"`
	// Phase is the Kubernetes pod phase, empty while the pod does not exist yet.
	Phase string `json:"phase"`
	IP    string `json:"ip,omitempty"`
	// Ready is the ready fraction of containers, such as "1/1".
	Ready    string `json:"ready,omitempty"`
	Restarts int    `json:"restarts"`
	// Waiting is why a container is not running, such as ImagePullBackOff.
	Waiting   string    `json:"waiting,omitempty"`
	Ports     []string  `json:"ports,omitempty"`
	Container string    `json:"container,omitempty"`
	CreatedAt time.Time `json:"createdAt"`

	Image         string `json:"image,omitempty"`
	Command       string `json:"command,omitempty"`
	RestartPolicy string `json:"restartPolicy,omitempty"`
	// AgentVersion is the coding agent installed in the pod, when known. It is
	// read once per pod and only with include=versions.
	AgentVersion string `json:"agentVersion,omitempty"`

	Resources Resources `json:"resources"`
	// Usage is what the pod uses now; absent when nothing could measure it.
	Usage *Usage `json:"usage,omitempty"`
	// Startup is the pod's progress while it is still starting, absent once it
	// is ready, finished, or being deleted.
	Startup *Startup `json:"startup,omitempty"`
	// Configs are the pod's manifests, only with include=configs.
	Configs *Configs `json:"configs,omitempty"`
}

// OwnerRef names what owns a pod.
type OwnerRef struct {
	// Kind is SandboxWarmPool, SandboxClaim, Job, ReplicaSet, and so on; empty
	// when a volume's sandbox has no owner.
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

// Resources are a pod's requests and limits.
type Resources struct {
	CPURequestMillis int64 `json:"cpuRequestMillis"`
	CPULimitMillis   int64 `json:"cpuLimitMillis"`
	MemRequestMiB    int64 `json:"memRequestMiB"`
	MemLimitMiB      int64 `json:"memLimitMiB"`
}

// Usage is a pod's CPU and memory now.
type Usage struct {
	CPUMillis int64 `json:"cpuMillis"`
	MemMiB    int64 `json:"memMiB"`
	// Live is true when the numbers come from the pod's own cgroup
	// (include=live), false when they are the cluster's metrics, which lag by
	// tens of seconds.
	Live bool `json:"live"`
}

// Startup is a pod's progress toward readiness. Kubernetes exposes no
// byte-level image pull progress, so stages are as fine as this gets.
type Startup struct {
	// Stage is one of the Stage constants.
	Stage string `json:"stage"`
	// Since is when the stage began, as best known.
	Since time.Time `json:"since"`
	// Error is the waiting reason when the stage keeps failing, such as
	// ImagePullBackOff.
	Error string `json:"error,omitempty"`
	// Detail is the latest failing pull event's message.
	Detail       string `json:"detail,omitempty"`
	PullFailures int    `json:"pullFailures,omitempty"`
}

// Configs are manifests as YAML, for showing, never for applying.
type Configs struct {
	// Sandbox is the Sandbox or workload manifest.
	Sandbox string `json:"sandbox,omitempty"`
	// Pod is the pod manifest.
	Pod string `json:"pod,omitempty"`
	// Extra is more YAML worth showing with the pod, such as the egress
	// proxy's policy.
	Extra string `json:"extra,omitempty"`
}

// Pool is a warm pool: standbys of one agent, kept ready to be claimed.
type Pool struct {
	Name     string `json:"name"`
	Template string `json:"template"`
	Ready    int    `json:"ready"`
	Desired  int    `json:"desired"`
	// UpdateStrategy is how the pool replaces standbys when its template
	// changes.
	UpdateStrategy string `json:"updateStrategy,omitempty"`
	// Configs are the pool and template manifests, only with include=configs.
	Configs *PoolConfigs `json:"configs,omitempty"`
}

// PoolConfigs are a pool's manifests as YAML.
type PoolConfigs struct {
	Pool     string `json:"pool,omitempty"`
	Template string `json:"template,omitempty"`
}

// Logs is a pod's recent log lines: GET /v1/pods/{namespace}/{name}/logs.
type Logs struct {
	Pod   string   `json:"pod"`
	Lines []string `json:"lines"`
}
