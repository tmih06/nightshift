package tui

// OpenPullRequestMsg asks the host to open a pull request in its own view.
// nightshift sends it from enter on a pull request row and shows the pull
// request in zen-octo's view; gh-dash itself does nothing with it.
type OpenPullRequestMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
}

// SandboxPullRequestMsg asks the host to check a pull request out in a new
// sandbox. nightshift sends it from S on a pull request row, asks which warm
// pool to claim from, and checks the pull request out in the claim's shell.
type SandboxPullRequestMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
	Branch string // the head branch; empty when gh-dash does not know it
}
