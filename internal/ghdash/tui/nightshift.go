package tui

// OpenPullRequestMsg asks the host to open a pull request in its own view.
// nightshift sends it from enter on a pull request row and shows the pull
// request in zen-octo's view; gh-dash itself does nothing with it.
type OpenPullRequestMsg struct {
	Repo   string // owner/name
	Number int
	URL    string
}
