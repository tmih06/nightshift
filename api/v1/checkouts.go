package v1

import "time"

// Target kinds, the values of Target.Kind.
const (
	TargetPullRequest = "pull_request"
	TargetIssue       = "issue"
	TargetBranch      = "branch"
)

// Target is what a volume keeps: a pull request, an issue, or a branch.
type Target struct {
	// Kind is one of the Target constants.
	Kind string `json:"kind"`
	// Repo is owner/name.
	Repo string `json:"repo"`
	// Number is the pull request or issue number; zero for a branch.
	Number int `json:"number,omitempty"`
	// Branch is the pull request's head branch, the branch to work on for an
	// issue, or the branch itself. Empty picks a default: pr-N, issue-N, or
	// main.
	Branch string `json:"branch,omitempty"`
}

// CheckoutRequest puts a target on its own volume and starts a sandbox on it.
// The volume outlives the sandbox, so asking again for the same target
// returns to the work.
type CheckoutRequest struct {
	Target Target `json:"target"`
	// Pool is the warm pool whose template the sandbox is made from. The
	// sandbox itself starts cold: a running pod cannot take a new volume.
	Pool string `json:"pool"`
	// Harness is the coding agent command to start in the clone, such as
	// claude or codex. Empty leaves bash open.
	Harness string `json:"harness,omitempty"`
	// Model is passed to the harness as --model. Empty uses its default.
	Model string `json:"model,omitempty"`
}

// Checkout steps, the values of CheckoutEvent.Step, in order.
const (
	StepVolume  = "volume"
	StepClone   = "clone"
	StepSandbox = "sandbox"
	StepShell   = "shell"
)

// Checkout states, the values of Checkout.State.
const (
	CheckoutRunning = "running"
	CheckoutDone    = "done"
	CheckoutFailed  = "failed"
)

// Checkout is one run of the checkout pipeline.
type Checkout struct {
	ID     string `json:"id"`
	Target Target `json:"target"`
	Pool   string `json:"pool"`
	Volume string `json:"volume"`
	// State is one of the Checkout constants.
	State string `json:"state"`
	// Sandbox is the pod, once the pipeline has started it.
	Sandbox   string    `json:"sandbox,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	// Last is the most recent step event, so a client that joins late can draw
	// where things stand.
	Last *CheckoutEvent `json:"last,omitempty"`
}

// CheckoutEvent is one step of progress, streamed from
// GET /v1/checkouts/{id}/events as the server-sent event "checkout". The
// stream ends after an event with Done or Error set.
type CheckoutEvent struct {
	// Step is one of the Step constants.
	Step string `json:"step"`
	// Detail is what the step is doing, in a few words. During the clone it is
	// git's own progress line.
	Detail string `json:"detail,omitempty"`
	// Finished marks Step as complete.
	Finished bool `json:"finished,omitempty"`
	// Sandbox is the pod, once it exists.
	Sandbox string `json:"sandbox,omitempty"`
	// Done marks the whole pipeline as complete.
	Done bool `json:"done,omitempty"`
	// Error is why the pipeline stopped at Step.
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

// Volume is a checkout's storage: the clone and the work on it.
type Volume struct {
	Name   string `json:"name"`
	Target Target `json:"target"`
	// Cloned is whether the clone finished. A volume that is not cloned is
	// cloned again from scratch.
	Cloned bool `json:"cloned"`
	// Sandbox is the pod that mounts the volume now, empty when none does.
	Sandbox   string    `json:"sandbox,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Volumes is the body of GET /v1/volumes.
type Volumes struct {
	Volumes []Volume `json:"volumes"`
}
