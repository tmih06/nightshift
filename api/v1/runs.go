package v1

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Run states, the values of Run.State, in order. A run ends done or failed.
const (
	RunQueued   = "queued"
	RunCloning  = "cloning"
	RunStarting = "starting"
	RunWorking  = "working"
	RunDone     = "done"
	RunFailed   = "failed"
)

// Limits of a RunRequest.
const (
	MaxPrompt          = 64 << 10
	MaxExternalRef     = 200
	DefaultRunSeconds  = 3600
	MinRunSeconds      = 60
	MaxRunSeconds      = 86400
	maxIdempotencyKey  = 200
	maxModelNameLength = 200
)

// RunSource is the git repository a run works on. Any host that serves git
// over https will do.
type RunSource struct {
	// CloneURL is the repository's https clone address, without credentials.
	CloneURL string `json:"cloneUrl"`
	// Ref is where to start: a branch, tag, commit, or a full ref such as
	// refs/pull/7/head. Empty starts at the repository's default branch.
	Ref string `json:"ref,omitempty"`
	// Branch is the local branch the agent works on. Empty names one after the
	// run.
	Branch string `json:"branch,omitempty"`
}

// RunRequest hands a task to a coding agent: clone the source, start a sandbox
// on it, and run the pool's agent on the prompt until it exits. It names no
// issue tracker; an adapter ties a run to its ticket with ExternalRef.
type RunRequest struct {
	Source RunSource `json:"source"`
	// Prompt is the task, in the words the agent is given.
	Prompt string `json:"prompt"`
	// Pool is the warm pool whose template and agent the run uses.
	Pool string `json:"pool"`
	// Model is passed to the agent. Empty uses its default.
	Model string `json:"model,omitempty"`
	// ExternalRef is the caller's own name for the work, such as a ticket ID.
	// The server stores it and filters by it, and reads nothing into it.
	ExternalRef string `json:"externalRef,omitempty"`
	// IdempotencyKey makes asking twice safe: a second request with the same
	// key returns the first run.
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	// TimeoutSeconds stops an agent that has not exited. Zero means
	// DefaultRunSeconds.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// Run is one task given to a coding agent, from request to result.
type Run struct {
	ID string `json:"id"`
	// State is one of the Run constants. Done means the agent exited with
	// status zero, not that its work was checked.
	State          string    `json:"state"`
	Source         RunSource `json:"source"`
	Pool           string    `json:"pool"`
	Model          string    `json:"model,omitempty"`
	ExternalRef    string    `json:"externalRef,omitempty"`
	TimeoutSeconds int       `json:"timeoutSeconds"`
	// Sandbox is the pod, once there is one. It stays until the run is deleted.
	Sandbox string `json:"sandbox,omitempty"`
	// ExitCode is the agent's exit status, once it has exited.
	ExitCode *int `json:"exitCode,omitempty"`
	// Output is the end of what the agent printed. Lists leave it out.
	Output string `json:"output,omitempty"`
	// Error is why the run failed.
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// StartedAt is when the agent was started.
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

// Finished is whether the run has ended, either way.
func (r Run) Finished() bool { return r.State == RunDone || r.State == RunFailed }

// Runs is the body of GET /v1/runs, newest first.
type Runs struct {
	Runs []Run `json:"runs"`
}

var (
	modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:#@-]*$`)
	repoDir   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
)

// RepoName is the repository's directory name, as git clone would pick it, or
// "" when the address has none that is safe to use.
func (s RunSource) RepoName() string {
	path := strings.TrimSuffix(strings.TrimRight(s.CloneURL, "/"), ".git")
	name := path[strings.LastIndex(path, "/")+1:]
	if !repoDir.MatchString(name) {
		return ""
	}
	return name
}

func (s RunSource) problem() string {
	if why := s.addressProblem(); why != "" {
		return why
	}
	switch {
	case s.Ref != "" && BranchProblem(s.Ref) != "":
		return "source.ref: invalid git ref"
	case s.Branch != "" && BranchProblem(s.Branch) != "":
		return "source.branch: " + BranchProblem(s.Branch)
	}
	return ""
}

func (s RunSource) addressProblem() string {
	u, err := url.Parse(s.CloneURL)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "":
		return "source.cloneUrl must be an https address"
	case u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return "source.cloneUrl must carry no credentials, query, or fragment"
	case strings.ContainsFunc(s.CloneURL, func(r rune) bool { return r <= 0x20 || r == 0x7f }):
		return "source.cloneUrl must hold no spaces or control characters"
	case strings.Trim(u.Path, "/") == "" || s.RepoName() == "":
		return "source.cloneUrl must name a repository"
	}
	return ""
}

// Problem is why the request cannot be acted on, or "" when it can.
func (r RunRequest) Problem() string {
	if why := r.Source.problem(); why != "" {
		return why
	}
	switch {
	case r.Pool == "":
		return "pool is required"
	case strings.TrimSpace(r.Prompt) == "":
		return "prompt is required"
	case len(r.Prompt) > MaxPrompt || strings.ContainsRune(r.Prompt, 0):
		return "prompt must be at most 64 KiB of text"
	case r.Model != "" && (len(r.Model) > maxModelNameLength || !modelName.MatchString(r.Model)):
		return "model is not a model name"
	}
	return r.limitProblem()
}

func (r RunRequest) limitProblem() string {
	switch {
	case len(r.ExternalRef) > MaxExternalRef:
		return "externalRef is too long"
	case len(r.IdempotencyKey) > maxIdempotencyKey:
		return "idempotencyKey is too long"
	case r.TimeoutSeconds != 0 && (r.TimeoutSeconds < MinRunSeconds || r.TimeoutSeconds > MaxRunSeconds):
		return "timeoutSeconds must be between 60 and 86400"
	}
	return ""
}
