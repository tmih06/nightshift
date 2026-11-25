// Package fleet is how the terminal UI sees the sandbox fleet: plain values
// read from the services' API, with the questions the screens ask of them. It
// holds nothing of the cluster; it is what a frontend makes of the contract's
// types.
package fleet

import (
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// Namespace is where sandboxes live. A record that omits its namespace is in
// this one.
const Namespace = "nightshift"

// PodPhaseRunning is the Kubernetes Running pod phase.
const PodPhaseRunning = "Running"

const (
	ownerClaim = "SandboxClaim"
	ownerPool  = "SandboxWarmPool"
	ownerJob   = "Job"
)

// PodRole classifies a pod shown in the Pods tab.
type PodRole int

// The roles, in the order the tab groups them.
const (
	RoleWarm PodRole = iota
	RoleClaimed
	RoleEgress
	RoleSystem
	RoleJob
	RoleOther
)

// StartupStage is how far a pod is on its way to readiness.
type StartupStage int

// The stages a starting pod passes through.
const (
	StageScheduling StartupStage = iota
	StagePulling
	StageStarting
	StageReadying
)

// StartupStageCount is the number of startup stages, for segmented displays.
const StartupStageCount = 4

// Startup is a pod's progress toward readiness.
type Startup struct {
	Stage     StartupStage
	Since     time.Time // when the current stage began, best known
	Err       string    // waiting reason when the stage is failing
	Detail    string    // the latest failing pull event's message
	PullFails int       // failed pull attempts so far
}

// Failing is whether the startup is retrying after failures.
func (s *Startup) Failing() bool { return s != nil && s.Err != "" }

// Sandbox is one pod of the fleet.
type Sandbox struct {
	Name      string
	Namespace string // empty means Namespace
	OwnerKind string // SandboxWarmPool, SandboxClaim, Job, and so on
	Owner     string // the pool, claim, workload, or volume
	Volume    string // the volume it mounts, if a checkout started it
	// Target is what the volume keeps, when there is one.
	Target      *v1.Target
	Template    string
	NodeName    string
	Phase       string // pod phase, "…" until the pod exists
	PodIP       string
	Ready       string // ready container fraction, such as "1/1"
	Restarts    int
	Waiting     string // container waiting reason when unhealthy
	Ports       []string
	Container   string
	Created     time.Time
	CPULimitMC  int64
	MemLimitMiB int64
	CPUUsageMC  int64
	MemUsageMiB int64
	// UsageKnown is whether the two usage numbers were measured.
	UsageKnown bool

	Image         string
	Command       string
	RestartPolicy string
	CPUReqMC      int64
	MemReqMiB     int64

	// AgentVersion is the coding agent installed in the pod, when known.
	AgentVersion string
	// Terminals are the live terminals of a claimed sandbox.
	Terminals []v1.Terminal

	ManifestYAML string
	PodYAML      string
	ExtraYAML    string

	// Role is what the server says the pod is, for the roles that are not
	// told by the owner: egress, system, job.
	Role string

	// Startup is the pod's progress while it is still starting up; nil once
	// it is ready, terminal, or being deleted.
	Startup *Startup
}

// Pool is a warm pool.
type Pool struct {
	Name           string
	Template       string
	Ready, Desired int
	UpdateStrategy string
	ManifestYAML   string
	TemplateYAML   string
	Harness        v1.Harness
}

// EffectiveNamespace is b's namespace, Namespace when it omits one.
func (b Sandbox) EffectiveNamespace() string {
	if b.Namespace != "" {
		return b.Namespace
	}
	return Namespace
}

// Claimed is whether b is someone's workspace: adopted by a claim, or started
// on a volume.
func (b Sandbox) Claimed() bool { return b.OwnerKind == ownerClaim || b.Volume != "" }

// Warm is whether b is a warm-pool standby.
func (b Sandbox) Warm() bool { return b.OwnerKind == ownerPool }

// IsSandbox is whether b is a warm or claimed coding sandbox.
func (b Sandbox) IsSandbox() bool { return b.Warm() || b.Claimed() }

// IsInfra is whether b is an essential pod that is not a sandbox.
func (b Sandbox) IsInfra() bool { return !b.IsSandbox() }

// PodRole is b's role.
func (b Sandbox) PodRole() PodRole {
	switch {
	case b.Claimed():
		return RoleClaimed
	case b.Warm():
		return RoleWarm
	}
	switch b.Role {
	case v1.RoleEgress:
		return RoleEgress
	case v1.RoleSystem:
		return RoleSystem
	case v1.RoleJob:
		return RoleJob
	}
	if b.OwnerKind == ownerJob {
		return RoleJob
	}
	return RoleOther
}

// OwnerRank orders warm sandboxes, then claimed, then egress, system, jobs,
// and other.
func OwnerRank(b Sandbox) int { return int(b.PodRole()) }
