package v1

// ClaimRequest asks for a sandbox from a warm pool.
type ClaimRequest struct {
	// Pool is the warm pool's name, such as claude-code-pool.
	Pool string `json:"pool"`
}

// Claim is a request for a workspace. Claiming adopts any standby of the pool;
// the pool restocks itself.
type Claim struct {
	Name string `json:"name"`
	Pool string `json:"pool"`
	// Sandbox is the pod the claim adopted, empty until it has one.
	Sandbox string `json:"sandbox,omitempty"`
}

// Bound is whether the claim has a sandbox.
func (c Claim) Bound() bool { return c.Sandbox != "" }
