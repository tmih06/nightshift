// Package repoview implements Nightshift's 3-pane repository workspace for the
// GitHub tab: a repository/scope navigation tree, a grouped scope list, and a
// detail preview pane styled in Nightshift's flat gh-dash aesthetic.
package repoview

import "time"

// Pane identifies one of the three workspace panes.
type Pane int

const (
	PaneNav Pane = iota
	PaneList
	PaneDetails
)

// Scope identifies which repository resource view is shown in Pane 2.
type Scope int

const (
	ScopeBranches Scope = iota
	ScopePRs
	ScopeIssues
	ScopeActions
	ScopeFiles
)

var allScopes = []Scope{
	ScopeBranches,
	ScopePRs,
	ScopeIssues,
	ScopeActions,
	ScopeFiles,
}

// String returns the full label for the scope.
func (s Scope) String() string {
	switch s {
	case ScopeBranches:
		return "Branches"
	case ScopePRs:
		return "Pull Requests"
	case ScopeIssues:
		return "Issues"
	case ScopeActions:
		return "Actions"
	case ScopeFiles:
		return "Files"
	default:
		return "Branches"
	}
}

// ShortLabel returns the compact tab strip label for the scope.
func (s Scope) ShortLabel() string {
	switch s {
	case ScopeBranches:
		return "Branches"
	case ScopePRs:
		return "PRs"
	case ScopeIssues:
		return "Issues"
	case ScopeActions:
		return "Actions"
	case ScopeFiles:
		return "Files"
	default:
		return "Branches"
	}
}

// Icon returns the Nerd Font glyph used for the scope.
func (s Scope) Icon() string {
	switch s {
	case ScopeBranches:
		return ""
	case ScopePRs:
		return ""
	case ScopeIssues:
		return ""
	case ScopeActions:
		return ""
	case ScopeFiles:
		return ""
	default:
		return ""
	}
}

// SearchMode identifies which pane currently holds an active inline search bar.
type SearchMode int

const (
	SearchNone SearchMode = iota
	SearchNav
	SearchList
)

// ModalKind identifies the currently open overlay modal, if any.
type ModalKind int

const (
	ModalNone ModalKind = iota
	ModalPalette
	ModalAddRepo
	ModalNewBranch
	ModalNewIssue
	ModalCommentIssue
	ModalConfirm
)

type confirmKind int

const (
	confirmDeleteBranch confirmKind = iota
	confirmSetIssueState
	confirmRerunWorkflow
)

type confirmAction struct {
	title      string
	detail     string
	kind       confirmKind
	repo       string
	branch     string
	issueNum   int
	nextState  string
	runID      int64
	failedOnly bool
	danger     bool
}

// Options configures a new repoview Model.
type Options struct {
	CurrentRepo string
	PinsPath    string
	Client      Client
}

// ActiveSandbox describes a running or pending Nightshift sandbox pod mounted
// on a repository volume.
type ActiveSandbox struct {
	Pod    string
	Volume string
	Phase  string
}

// Repo is a GitHub repository shown in the tree and overview pane.
type Repo struct {
	Owner         string
	Name          string
	FullName      string
	Description   string
	URL           string
	DefaultBranch string
	Language      string
	Topics        []string
	Visibility    string
	IsPrivate     bool
	IsFork        bool
	IsArchived    bool
	Stars         int
	Forks         int
	OpenIssues    int
	UpdatedAt     time.Time
	Readme        string
}

// NormalizedFullName returns "owner/name", deriving it from Owner and Name if
// FullName is empty.
func (r Repo) NormalizedFullName() string {
	if r.FullName != "" {
		return r.FullName
	}
	if r.Owner != "" && r.Name != "" {
		return r.Owner + "/" + r.Name
	}
	return r.Name
}

// Branch is a git branch in a repository.
type Branch struct {
	Name          string
	SHA           string
	Protected     bool
	IsDefault     bool
	CommitsAhead  int
	CommitsBehind int
	LinkedPR      *PullRequest
	UpdatedAt     time.Time
}

// Commit is a git commit on a branch.
type Commit struct {
	SHA      string
	ShortSHA string
	Message  string
	Author   string
	Date     time.Time
}

// PullRequest is a pull request in a repository.
type PullRequest struct {
	Number         int
	Title          string
	Body           string
	State          string // OPEN, CLOSED, MERGED
	IsDraft        bool
	Author         string
	HeadRef        string
	BaseRef        string
	URL            string
	Additions      int
	Deletions      int
	ChangedFiles   int
	CommentsCount  int
	ReviewDecision string
	ChecksState    string // SUCCESS, FAILURE, PENDING, etc.
	Labels         []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	MergedAt       *time.Time
}

// Issue is an issue in a repository.
type Issue struct {
	Number        int
	Title         string
	Body          string
	State         string // OPEN, CLOSED
	Author        string
	Assignees     []string
	Labels        []string
	CommentsCount int
	URL           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IssueComment is a comment on a GitHub issue.
type IssueComment struct {
	ID        string
	Author    string
	Body      string
	CreatedAt time.Time
}

// WorkflowRun is a GitHub Actions workflow run.
type WorkflowRun struct {
	ID           int64
	Name         string
	WorkflowName string
	DisplayTitle string
	Status       string // queued, in_progress, completed
	Conclusion   string // success, failure, cancelled, skipped, timed_out, action_required
	Branch       string
	Event        string
	HeadSHA      string
	RunNumber    int
	URL          string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// WorkflowJob is one job in a GitHub Actions workflow run.
type WorkflowJob struct {
	ID          int64
	Name        string
	Status      string
	Conclusion  string
	StartedAt   time.Time
	CompletedAt time.Time
	Steps       []WorkflowStep
}

// WorkflowStep is one step in a GitHub Actions job.
type WorkflowStep struct {
	Name       string
	Status     string
	Conclusion string
	Number     int
}

// TreeEntry is a directory or file entry returned by the repository contents API.
type TreeEntry struct {
	Name string
	Path string
	Type string // "dir", "file", "symlink", "submodule"
	Size int
	SHA  string
	URL  string
}

// FileContent holds a file's preview contents and metadata.
type FileContent struct {
	Path      string
	Ref       string
	Size      int
	Content   string
	Binary    bool
	Truncated bool
	Encoding  string
}
