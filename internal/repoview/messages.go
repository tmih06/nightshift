package repoview

import (
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
)

// Host integration messages handled directly by internal/app.

// SwitchViewMsg asks the host to switch the GitHub sub-view (e.g. PRs, Issues,
// Notifications).
type SwitchViewMsg struct {
	View config.ViewType
}

// OpenPRMsg asks the host to open a pull request in zen-octo.
type OpenPRMsg struct {
	Repo   string
	Number int
	URL    string
}

// SandboxRepoMsg asks the host to start or open a sandbox on a repo/branch.
type SandboxRepoMsg struct {
	Repo   string
	Branch string
	URL    string
}

// SandboxPRMsg asks the host to start or open a sandbox for a pull request.
type SandboxPRMsg struct {
	Repo   string
	Number int
	URL    string
	Branch string
}

// SandboxIssueMsg asks the host to start or open a sandbox for an issue.
type SandboxIssueMsg struct {
	Repo   string
	Number int
	URL    string
	Title  string
}

// FocusSandboxMsg asks the host to switch to the Sandboxes tab and focus Pod.
type FocusSandboxMsg struct {
	Pod string
}

// Internal async/result messages owned exclusively by repoview.

type viewerLoadedMsg struct {
	login string
	err   error
}

type reposLoadedMsg struct {
	repos []Repo
	err   error
}

type searchReposLoadedMsg struct {
	query string
	repos []Repo
	err   error
}

type repoOverviewLoadedMsg struct {
	repo string
	data Repo
	err  error
}

type branchesLoadedMsg struct {
	repo     string
	branches []Branch
	err      error
}

type branchCommitsLoadedMsg struct {
	repo    string
	branch  string
	commits []Commit
	err     error
}

type branchCreatedMsg struct {
	repo   string
	branch string
	err    error
}

type branchDeletedMsg struct {
	repo   string
	branch string
	err    error
}

type prsLoadedMsg struct {
	repo string
	prs  []PullRequest
	err  error
}

type issuesLoadedMsg struct {
	repo   string
	issues []Issue
	err    error
}

type issueCommentsLoadedMsg struct {
	repo     string
	number   int
	comments []IssueComment
	err      error
}

type issueCreatedMsg struct {
	repo  string
	issue Issue
	err   error
}

type issueCommentedMsg struct {
	repo    string
	number  int
	comment IssueComment
	err     error
}

type issueStateSetMsg struct {
	repo   string
	number int
	state  string
	err    error
}

type runsLoadedMsg struct {
	repo string
	runs []WorkflowRun
	err  error
}

type jobsLoadedMsg struct {
	repo  string
	runID int64
	jobs  []WorkflowJob
	err   error
}

type workflowRerunMsg struct {
	repo  string
	runID int64
	err   error
}

type dirLoadedMsg struct {
	repo    string
	ref     string
	path    string
	entries []TreeEntry
	err     error
}

type fileLoadedMsg struct {
	repo    string
	ref     string
	path    string
	content FileContent
	err     error
}

type noticeExpiredMsg struct {
	at time.Time
}

type browserOpenedMsg struct {
	url string
	err error
}

type clipboardCopiedMsg struct {
	label string
	err   error
}

type fetchDebounceMsg struct {
	seq uint64
	at  time.Time
}

// Owns reports whether msg is an internal repoview async/result message.
// Host-handled messages (SwitchViewMsg, OpenPRMsg, Sandbox*Msg, FocusSandboxMsg)
// and standard Bubble Tea messages return false so the root app handles them.
func Owns(msg tea.Msg) bool {
	switch msg.(type) {
	case viewerLoadedMsg,
		reposLoadedMsg,
		searchReposLoadedMsg,
		repoOverviewLoadedMsg,
		branchesLoadedMsg,
		branchCommitsLoadedMsg,
		branchCreatedMsg,
		branchDeletedMsg,
		prsLoadedMsg,
		issuesLoadedMsg,
		issueCommentsLoadedMsg,
		issueCreatedMsg,
		issueCommentedMsg,
		issueStateSetMsg,
		runsLoadedMsg,
		jobsLoadedMsg,
		workflowRerunMsg,
		dirLoadedMsg,
		fileLoadedMsg,
		noticeExpiredMsg,
		browserOpenedMsg,
		clipboardCopiedMsg,
		fetchDebounceMsg:
		return true
	default:
		return false
	}
}

// Wants reports whether msg can matter to repoview: its own async messages,
// bubbles v2 messages (cursor/spinner), and window/focus/input messages.
func Wants(msg tea.Msg) bool {
	if Owns(msg) {
		return true
	}
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.FocusMsg, tea.BlurMsg,
		tea.KeyPressMsg, tea.KeyReleaseMsg,
		tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg,
		tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg, tea.MouseMotionMsg:
		return true
	}
	t := reflect.TypeOf(msg)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	return strings.HasPrefix(t.PkgPath(), "charm.land/bubbles/v2")
}
