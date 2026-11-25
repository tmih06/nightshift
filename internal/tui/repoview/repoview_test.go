package repoview

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/ghdash/config"
	ghctx "github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

type fakeClient struct {
	mu sync.Mutex

	viewer        string
	repos         []Repo
	searchResults map[string][]Repo
	overviews     map[string]Repo
	branches      map[string][]Branch
	commits       map[string][]Commit // key: repo + ":" + branch
	prs           map[string][]PullRequest
	issues        map[string][]Issue
	comments      map[string][]IssueComment // key: repo + "#" + num
	runs          map[string][]WorkflowRun
	jobs          map[int64][]WorkflowJob
	dirs          map[string][]TreeEntry // key: repo + ":" + path
	files         map[string]FileContent // key: repo + ":" + path

	createdBranches []string
	deletedBranches []string
	createdIssues   []Issue
	addedComments   []string
	stateChanges    []string
	reruns          []int64

	overviewCalls []string
	branchCalls   []string
}

func newFakeClient() *fakeClient {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	return &fakeClient{
		viewer: "alice",
		repos: []Repo{
			{
				Owner:         "alice",
				Name:          "nightshift",
				FullName:      "alice/nightshift",
				Description:   "Autonomous software factory TUI",
				URL:           "https://github.com/alice/nightshift",
				DefaultBranch: "main",
				Language:      "Go",
				Topics:        []string{"tui", "kubernetes"},
				Visibility:    "public",
				Stars:         128,
				Forks:         14,
				OpenIssues:    3,
				UpdatedAt:     now.Add(-2 * time.Hour),
			},
			{
				Owner:         "acme",
				Name:          "rocket",
				FullName:      "acme/rocket",
				Description:   "Orbital launch control",
				URL:           "https://github.com/acme/rocket",
				DefaultBranch: "main",
				Language:      "Rust",
				Visibility:    "private",
				IsPrivate:     true,
				Stars:         42,
				Forks:         5,
				OpenIssues:    2,
				UpdatedAt:     now.Add(-6 * time.Hour),
			},
			{
				Owner:         "acme",
				Name:          "widgets",
				FullName:      "acme/widgets",
				Description:   "Reusable widget library",
				URL:           "https://github.com/acme/widgets",
				DefaultBranch: "main",
				Language:      "TypeScript",
				Visibility:    "public",
				Stars:         9,
				UpdatedAt:     now.Add(-24 * time.Hour),
			},
		},
		searchResults: map[string][]Repo{
			"orbiter": {
				{
					Owner:         "space",
					Name:          "orbiter",
					FullName:      "space/orbiter",
					Description:   "Deep space probe controller",
					URL:           "https://github.com/space/orbiter",
					DefaultBranch: "main",
				},
			},
		},
		overviews: map[string]Repo{
			"alice/nightshift": {
				Owner:         "alice",
				Name:          "nightshift",
				FullName:      "alice/nightshift",
				Description:   "Autonomous software factory TUI",
				URL:           "https://github.com/alice/nightshift",
				DefaultBranch: "main",
				Language:      "Go",
				Topics:        []string{"tui", "kubernetes", "agents"},
				Visibility:    "public",
				Stars:         128,
				Forks:         14,
				OpenIssues:    3,
				Readme:        "# Nightshift\n\nWelcome to the **software factory**.\n",
			},
			"acme/rocket": {
				Owner:         "acme",
				Name:          "rocket",
				FullName:      "acme/rocket",
				Description:   "Orbital launch control",
				URL:           "https://github.com/acme/rocket",
				DefaultBranch: "main",
				Language:      "Rust",
				Visibility:    "private",
				IsPrivate:     true,
				Readme:        "# Rocket\n\nLaunch checklists.\n",
			},
		},
		branches: map[string][]Branch{
			"alice/nightshift": {
				{Name: "main", SHA: "1234567890abcdef", IsDefault: true, Protected: true, UpdatedAt: now.Add(-1 * time.Hour)},
				{Name: "feat/repoview", SHA: "abcdef1234567890", LinkedPR: &PullRequest{Number: 42, Title: "Add 3-pane repo workspace"}, UpdatedAt: now.Add(-30 * time.Minute)},
				{Name: "chore/cleanup", SHA: "fedcba0987654321", UpdatedAt: now.Add(-3 * time.Hour)},
			},
		},
		commits: map[string][]Commit{
			"alice/nightshift:main": {
				{SHA: "1234567890abcdef", ShortSHA: "1234567", Message: "feat: initial workspace", Author: "alice", Date: now.Add(-1 * time.Hour)},
				{SHA: "0000111122223333", ShortSHA: "0000111", Message: "docs: update readme", Author: "bob", Date: now.Add(-5 * time.Hour)},
			},
			"alice/nightshift:feat/repoview": {
				{SHA: "abcdef1234567890", ShortSHA: "abcdef1", Message: "feat(repoview): add 3-pane layout", Author: "alice", Date: now.Add(-30 * time.Minute)},
			},
		},
		prs: map[string][]PullRequest{
			"alice/nightshift": {
				{
					Number:        42,
					Title:         "Add 3-pane repo workspace",
					Body:          "## Summary\nAdds `[1] Repos | [2] Scope | [3] Details`.",
					State:         "OPEN",
					Author:        "alice",
					HeadRef:       "feat/repoview",
					BaseRef:       "main",
					URL:           "https://github.com/alice/nightshift/pull/42",
					Additions:     450,
					Deletions:     12,
					ChangedFiles:  8,
					CommentsCount: 2,
					ChecksState:   "SUCCESS",
					Labels:        []string{"enhancement", "tui"},
					UpdatedAt:     now.Add(-20 * time.Minute),
				},
				{
					Number:    43,
					Title:     "WIP: sandbox warmpool tuning",
					State:     "OPEN",
					IsDraft:   true,
					Author:    "bob",
					HeadRef:   "wip/warmpool",
					BaseRef:   "main",
					URL:       "https://github.com/alice/nightshift/pull/43",
					UpdatedAt: now.Add(-2 * time.Hour),
				},
				{
					Number:    40,
					Title:     "Fix footer truncation",
					State:     "MERGED",
					Author:    "alice",
					HeadRef:   "fix/footer",
					BaseRef:   "main",
					URL:       "https://github.com/alice/nightshift/pull/40",
					UpdatedAt: now.Add(-24 * time.Hour),
				},
			},
		},
		issues: map[string][]Issue{
			"alice/nightshift": {
				{
					Number:        10,
					Title:         "Support file preview syntax highlighting",
					Body:          "Files should be highlighted using Chroma.",
					State:         "OPEN",
					Author:        "bob",
					Assignees:     []string{"alice"},
					Labels:        []string{"enhancement"},
					CommentsCount: 1,
					URL:           "https://github.com/alice/nightshift/issues/10",
					UpdatedAt:     now.Add(-45 * time.Minute),
				},
				{
					Number:    9,
					Title:     "Old closed bug",
					Body:      "Already resolved.",
					State:     "CLOSED",
					Author:    "carol",
					URL:       "https://github.com/alice/nightshift/issues/9",
					UpdatedAt: now.Add(-48 * time.Hour),
				},
			},
		},
		comments: map[string][]IssueComment{
			"alice/nightshift#10": {
				{ID: "c1", Author: "alice", Body: "Using `zenocto/tui/syntax` for this!", CreatedAt: now.Add(-15 * time.Minute)},
			},
		},
		runs: map[string][]WorkflowRun{
			"alice/nightshift": {
				{
					ID:           1001,
					Name:         "CI",
					DisplayTitle: "Running unit tests",
					Status:       "in_progress",
					Branch:       "feat/repoview",
					Event:        "pull_request",
					HeadSHA:      "abcdef1234567890",
					RunNumber:    88,
					URL:          "https://github.com/alice/nightshift/actions/runs/1001",
					CreatedAt:    now.Add(-5 * time.Minute),
					UpdatedAt:    now.Add(-1 * time.Minute),
				},
				{
					ID:           1002,
					Name:         "Lint",
					DisplayTitle: "Lint check failed",
					Status:       "completed",
					Conclusion:   "failure",
					Branch:       "chore/cleanup",
					Event:        "push",
					HeadSHA:      "fedcba0987654321",
					RunNumber:    87,
					URL:          "https://github.com/alice/nightshift/actions/runs/1002",
					CreatedAt:    now.Add(-30 * time.Minute),
					UpdatedAt:    now.Add(-28 * time.Minute),
				},
				{
					ID:           1003,
					Name:         "CI",
					DisplayTitle: "Main green build",
					Status:       "completed",
					Conclusion:   "success",
					Branch:       "main",
					Event:        "push",
					HeadSHA:      "1234567890abcdef",
					RunNumber:    86,
					URL:          "https://github.com/alice/nightshift/actions/runs/1003",
					CreatedAt:    now.Add(-2 * time.Hour),
					UpdatedAt:    now.Add(-118 * time.Minute),
				},
			},
		},
		jobs: map[int64][]WorkflowJob{
			1001: {
				{
					ID:     501,
					Name:   "test",
					Status: "in_progress",
					Steps: []WorkflowStep{
						{Name: "Checkout", Status: "completed", Conclusion: "success", Number: 1},
						{Name: "go test ./...", Status: "in_progress", Number: 2},
					},
				},
			},
			1002: {
				{
					ID:         502,
					Name:       "golangci-lint",
					Status:     "completed",
					Conclusion: "failure",
					Steps: []WorkflowStep{
						{Name: "Run linter", Status: "completed", Conclusion: "failure", Number: 1},
					},
				},
			},
		},
		dirs: map[string][]TreeEntry{
			"alice/nightshift:": {
				{Name: "cmd", Path: "cmd", Type: "dir"},
				{Name: "README.md", Path: "README.md", Type: "file", Size: 120},
				{Name: "main.go", Path: "main.go", Type: "file", Size: 64},
				{Name: "logo.bin", Path: "logo.bin", Type: "file", Size: 2048},
			},
			"alice/nightshift:cmd": {
				{Name: "app.go", Path: "cmd/app.go", Type: "file", Size: 88},
			},
		},
		files: map[string]FileContent{
			"alice/nightshift:main.go": {
				Path:    "main.go",
				Ref:     "main",
				Size:    64,
				Content: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
			},
			"alice/nightshift:README.md": {
				Path:    "README.md",
				Ref:     "main",
				Size:    120,
				Content: "# Nightshift\n\nRendered via glamour.\n",
			},
			"alice/nightshift:logo.bin": {
				Path:   "logo.bin",
				Ref:    "main",
				Size:   2048,
				Binary: true,
			},
			"alice/nightshift:cmd/app.go": {
				Path:    "cmd/app.go",
				Ref:     "main",
				Size:    88,
				Content: "package cmd\n\nfunc Run() {}\n",
			},
		},
	}
}

func (f *fakeClient) Viewer(context.Context) (string, error) { return f.viewer, nil }

func (f *fakeClient) ListRepos(context.Context, int) ([]Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Repo, len(f.repos))
	copy(out, f.repos)
	return out, nil
}

func (f *fakeClient) SearchRepos(_ context.Context, query string, _ int) ([]Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.searchResults[strings.ToLower(strings.TrimSpace(query))], nil
}

func (f *fakeClient) GetRepoOverview(_ context.Context, repo string) (Repo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overviewCalls = append(f.overviewCalls, repo)
	if r, ok := f.overviews[repo]; ok {
		return r, nil
	}
	owner, name, _ := strings.Cut(repo, "/")
	return Repo{Owner: owner, Name: name, FullName: repo, DefaultBranch: "main"}, nil
}

func (f *fakeClient) ListBranches(_ context.Context, repo string) ([]Branch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branchCalls = append(f.branchCalls, repo)
	return append([]Branch(nil), f.branches[repo]...), nil
}

func (f *fakeClient) ListBranchCommits(_ context.Context, repo, branch string, _ int) ([]Commit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Commit(nil), f.commits[repo+":"+branch]...), nil
}

func (f *fakeClient) CreateBranch(_ context.Context, repo, name, baseRef string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdBranches = append(f.createdBranches, repo+":"+name+"<-"+baseRef)
	f.branches[repo] = append(f.branches[repo], Branch{Name: name, SHA: "9999999"})
	return nil
}

func (f *fakeClient) DeleteBranch(_ context.Context, repo, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedBranches = append(f.deletedBranches, repo+":"+name)
	var kept []Branch
	for _, b := range f.branches[repo] {
		if b.Name != name {
			kept = append(kept, b)
		}
	}
	f.branches[repo] = kept
	return nil
}

func (f *fakeClient) ListPRs(_ context.Context, repo string, _ int) ([]PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PullRequest(nil), f.prs[repo]...), nil
}

func (f *fakeClient) ListIssues(_ context.Context, repo string, _ int) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Issue(nil), f.issues[repo]...), nil
}

func (f *fakeClient) ListIssueComments(_ context.Context, repo string, number int) ([]IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]IssueComment(nil), f.comments[repo+"#"+intStr(number)]...), nil
}

func (f *fakeClient) CreateIssue(_ context.Context, repo, title, body string) (Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	iss := Issue{
		Number: 100 + len(f.createdIssues),
		Title:  title,
		Body:   body,
		State:  "OPEN",
		Author: f.viewer,
		URL:    "https://github.com/" + repo + "/issues/100",
	}
	f.createdIssues = append(f.createdIssues, iss)
	f.issues[repo] = append([]Issue{iss}, f.issues[repo]...)
	return iss, nil
}

func (f *fakeClient) CommentIssue(_ context.Context, repo string, number int, body string) (IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addedComments = append(f.addedComments, repo+"#"+intStr(number)+":"+body)
	cm := IssueComment{ID: "new", Author: f.viewer, Body: body}
	key := repo + "#" + intStr(number)
	f.comments[key] = append(f.comments[key], cm)
	return cm, nil
}

func (f *fakeClient) SetIssueState(_ context.Context, repo string, number int, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateChanges = append(f.stateChanges, repo+"#"+intStr(number)+"="+state)
	return nil
}

func (f *fakeClient) ListWorkflowRuns(_ context.Context, repo string, _ int) ([]WorkflowRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WorkflowRun(nil), f.runs[repo]...), nil
}

func (f *fakeClient) ListWorkflowJobs(_ context.Context, _ string, runID int64) ([]WorkflowJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]WorkflowJob(nil), f.jobs[runID]...), nil
}

func (f *fakeClient) RerunWorkflow(_ context.Context, _ string, runID int64, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reruns = append(f.reruns, runID)
	return nil
}

func (f *fakeClient) ListDir(_ context.Context, repo, _, path string) ([]TreeEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]TreeEntry(nil), f.dirs[repo+":"+path]...), nil
}

func (f *fakeClient) GetFile(_ context.Context, repo, _, path string) (FileContent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[repo+":"+path], nil
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func newTestModel(t *testing.T, fc *fakeClient) *Model {
	t.Helper()
	th := *ghtheme.DefaultTheme
	styles := ghctx.InitStyles(th)
	pinsPath := filepath.Join(t.TempDir(), "repos.json")
	m := New(th, styles, Options{
		CurrentRepo: "alice/nightshift",
		PinsPath:    pinsPath,
		Client:      fc,
	})
	fixedNow := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixedNow }
	m.noticeTTL = time.Millisecond
	m.fetchDebounce = time.Millisecond
	m.openBrowser = func(string) error { return nil }
	m.copyClipboard = func(string) error { return nil }
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	runAllCmds(t, m, m.EnsureLoaded())
	return m
}

// runAllCmds executes cmd (recursively handling tea.BatchMsg) and feeds internal
// repoview messages back into m, returning any host-level messages emitted.
func runAllCmds(t *testing.T, m *Model, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var hostMsgs []tea.Msg
	var drain func(c tea.Cmd, depth int)
	drain = func(c tea.Cmd, depth int) {
		if c == nil || depth > 8 {
			return
		}
		msg := c()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, sub := range batch {
				drain(sub, depth+1)
			}
			return
		}
		if Owns(msg) {
			_, next := m.Update(msg)
			drain(next, depth+1)
			return
		}
		hostMsgs = append(hostMsgs, msg)
	}
	drain(cmd, 0)
	return hostMsgs
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	case "ctrl+n":
		return tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	default:
		r := []rune(k)
		if len(r) == 1 {
			return tea.KeyPressMsg{Code: r[0], Text: k}
		}
		return tea.KeyPressMsg{Text: k}
	}
}

func pressKey(t *testing.T, m *Model, k string) []tea.Msg {
	t.Helper()
	_, cmd := m.Update(keyMsg(k))
	return runAllCmds(t, m, cmd)
}

func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		runAllCmds(t, m, cmd)
	}
}

func assertExactFrame(t *testing.T, m *Model, wantW, wantH int) string {
	t.Helper()
	v := m.View().Content
	lines := strings.Split(v, "\n")
	if len(lines) != wantH {
		t.Fatalf("frame height = %d, want %d", len(lines), wantH)
	}
	for i, l := range lines {
		w := ansi.StringWidth(l)
		if w != wantW {
			t.Fatalf("line %d width = %d, want %d (plain=%q)", i, w, wantW, ansi.Strip(l))
		}
	}
	return ansi.Strip(v)
}

func TestOwnsAndWants(t *testing.T) {
	// Internal messages must be owned & wanted.
	internal := []tea.Msg{
		viewerLoadedMsg{},
		reposLoadedMsg{},
		branchesLoadedMsg{},
		prsLoadedMsg{},
		issuesLoadedMsg{},
		runsLoadedMsg{},
		dirLoadedMsg{},
		fileLoadedMsg{},
		noticeExpiredMsg{},
		fetchDebounceMsg{},
	}
	for _, msg := range internal {
		if !Owns(msg) {
			t.Errorf("Owns(%T) = false, want true", msg)
		}
		if !Wants(msg) {
			t.Errorf("Wants(%T) = false, want true", msg)
		}
	}

	// Host messages must NOT be owned so internal/app handles them directly.
	host := []tea.Msg{
		SwitchViewMsg{View: config.PRsView},
		OpenPRMsg{Repo: "a/b", Number: 1},
		SandboxRepoMsg{Repo: "a/b"},
		SandboxPRMsg{Repo: "a/b", Number: 1},
		SandboxIssueMsg{Repo: "a/b", Number: 2},
		FocusSandboxMsg{Pod: "pod-1"},
		tea.WindowSizeMsg{Width: 100, Height: 40},
	}
	for _, msg := range host {
		if Owns(msg) {
			t.Errorf("Owns(%T) = true, want false", msg)
		}
	}

	// Standard input/window/bubbles messages must be wanted.
	wanted := []tea.Msg{
		tea.WindowSizeMsg{Width: 100, Height: 40},
		tea.KeyPressMsg{Code: 'j', Text: "j"},
		tea.MouseClickMsg{},
		spinner.TickMsg{},
	}
	for _, msg := range wanted {
		if !Wants(msg) {
			t.Errorf("Wants(%T) = false, want true", msg)
		}
	}
}

func TestTreeNavAndPinPersistence(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	// Initial state: alice/nightshift selected & expanded; viewer compacting hides "alice/" prefix.
	plain := assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "[1] Repos") || !strings.Contains(plain, "[3] Details") {
		t.Fatalf("expected 3-pane headers in wide view:\n%s", plain)
	}
	if !strings.Contains(plain, "REPOSITORIES (3)") {
		t.Fatalf("expected REPOSITORIES heading:\n%s", plain)
	}
	if !strings.Contains(plain, "Autonomous software factory TUI") {
		t.Fatalf("expected repo overview in Pane 3 when on repo row")
	}

	// Collapse alice/nightshift with 'h', then move down skipping headings.
	pressKey(t, m, "h")
	if m.expandedRepos["alice/nightshift"] {
		t.Fatal("expected alice/nightshift collapsed after h")
	}
	pressKey(t, m, "j")
	if m.SelectedRepo() != "acme/rocket" {
		t.Fatalf("SelectedRepo = %q, want acme/rocket", m.SelectedRepo())
	}

	// Pin acme/rocket and alice/nightshift, verify persistence & reordering.
	pressKey(t, m, "f")
	if !m.isPinned("acme/rocket") {
		t.Fatal("expected acme/rocket to be pinned")
	}
	pressKey(t, m, "j")
	if m.SelectedRepo() != "alice/nightshift" {
		t.Fatalf("SelectedRepo = %q, want alice/nightshift", m.SelectedRepo())
	}
	pressKey(t, m, "f")
	if len(m.pins) != 2 || m.pins[0] != "acme/rocket" || m.pins[1] != "alice/nightshift" {
		t.Fatalf("unexpected pins order: %v", m.pins)
	}

	// Reorder alice/nightshift up with K.
	pressKey(t, m, "K")
	if m.pins[0] != "alice/nightshift" || m.pins[1] != "acme/rocket" {
		t.Fatalf("pins after K = %v, want [alice/nightshift acme/rocket]", m.pins)
	}

	// Reorder down with J.
	pressKey(t, m, "J")
	if m.pins[0] != "acme/rocket" || m.pins[1] != "alice/nightshift" {
		t.Fatalf("pins after J = %v, want [acme/rocket alice/nightshift]", m.pins)
	}

	// Verify persistence by loading a fresh Model from the same PinsPath.
	m2 := New(*ghtheme.DefaultTheme, ghctx.InitStyles(*ghtheme.DefaultTheme), Options{
		PinsPath: m.pinsPath,
		Client:   fc,
	})
	if len(m2.pins) != 2 || m2.pins[0] != "acme/rocket" || m2.pins[1] != "alice/nightshift" {
		t.Fatalf("reloaded pins = %v, want [acme/rocket alice/nightshift]", m2.pins)
	}

	// Filter repos in Pane 1 using '/'.
	pressKey(t, m, "/")
	if !m.Capturing() || m.searchMode != SearchNav {
		t.Fatal("expected Capturing=true in SearchNav")
	}
	typeText(t, m, "widgets")
	pressKey(t, m, "enter")
	if m.Capturing() {
		t.Fatal("expected Capturing=false after enter in search")
	}
	if m.SelectedRepo() != "acme/widgets" {
		t.Fatalf("filtered SelectedRepo = %q, want acme/widgets", m.SelectedRepo())
	}
	// Clear filter with / + esc.
	pressKey(t, m, "/")
	pressKey(t, m, "esc")
	if m.navFilter != "" {
		t.Fatalf("expected cleared navFilter, got %q", m.navFilter)
	}

	// Tab key must remain free when not capturing.
	prevPane := m.activePane
	msgs := pressKey(t, m, "tab")
	if len(msgs) != 0 || m.activePane != prevPane {
		t.Fatalf("tab should not be consumed when not capturing")
	}
}

func TestAllFiveScopesAndDetailsAndMutations(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	// Focus Pane 2 (Branches scope).
	pressKey(t, m, "2")
	plain := assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "DEFAULT (1)") || !strings.Contains(plain, "WITH PR (1)") || !strings.Contains(plain, "OTHER (1)") {
		t.Fatalf("expected grouped branches in Pane 2:\n%s", plain)
	}
	if !strings.Contains(plain, "RECENT COMMITS") || !strings.Contains(plain, "feat: initial workspace") {
		t.Fatalf("expected branch commits in Pane 3:\n%s", plain)
	}

	// Create new branch via 'n' modal.
	pressKey(t, m, "n")
	if m.modal != ModalNewBranch || !m.Capturing() {
		t.Fatal("expected ModalNewBranch open")
	}
	typeText(t, m, "feat/new-branch")
	pressKey(t, m, "enter")
	if len(fc.createdBranches) != 1 || !strings.Contains(fc.createdBranches[0], "feat/new-branch") {
		t.Fatalf("expected created branch, got %v", fc.createdBranches)
	}

	// Delete non-default branch via 'd' + confirm 'y'.
	pressKey(t, m, "j") // move from main to feat/repoview
	pressKey(t, m, "d")
	if m.modal != ModalConfirm {
		t.Fatalf("expected ModalConfirm for branch delete, got %v", m.modal)
	}
	pressKey(t, m, "y")
	if len(fc.deletedBranches) != 1 || fc.deletedBranches[0] != "alice/nightshift:feat/repoview" {
		t.Fatalf("expected deleted branch feat/repoview, got %v", fc.deletedBranches)
	}

	// Scope 2: Pull Requests via ']'.
	pressKey(t, m, "]")
	if m.activeScope != ScopePRs {
		t.Fatalf("activeScope = %v, want ScopePRs", m.activeScope)
	}
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "OPEN (1)") || !strings.Contains(plain, "DRAFT (1)") || !strings.Contains(plain, "CLOSED / MERGED (1)") {
		t.Fatalf("expected grouped PRs:\n%s", plain)
	}
	if !strings.Contains(plain, "main ← feat/repoview") || !strings.Contains(plain, "+450") {
		t.Fatalf("expected PR details in Pane 3:\n%s", plain)
	}
	// Enter emits OpenPRMsg.
	hostMsgs := pressKey(t, m, "enter")
	if len(hostMsgs) != 1 {
		t.Fatalf("expected 1 host msg on PR enter, got %v", hostMsgs)
	}
	openPR, ok := hostMsgs[0].(OpenPRMsg)
	if !ok || openPR.Repo != "alice/nightshift" || openPR.Number != 42 {
		t.Fatalf("unexpected OpenPRMsg: %#v", hostMsgs[0])
	}
	// S emits SandboxPRMsg.
	hostMsgs = pressKey(t, m, "S")
	sbxPR, ok := hostMsgs[0].(SandboxPRMsg)
	if !ok || sbxPR.Number != 42 || sbxPR.Branch != "feat/repoview" {
		t.Fatalf("unexpected SandboxPRMsg: %#v", hostMsgs)
	}

	// Scope 3: Issues via ']'.
	pressKey(t, m, "]")
	if m.activeScope != ScopeIssues {
		t.Fatalf("activeScope = %v, want ScopeIssues", m.activeScope)
	}
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "OPEN (1)") || !strings.Contains(plain, "CLOSED (1)") {
		t.Fatalf("expected grouped issues:\n%s", plain)
	}
	if !strings.Contains(plain, "COMMENTS (1)") {
		t.Fatalf("expected issue comments in Pane 3:\n%s", plain)
	}
	// S emits SandboxIssueMsg.
	hostMsgs = pressKey(t, m, "S")
	sbxIss, ok := hostMsgs[0].(SandboxIssueMsg)
	if !ok || sbxIss.Number != 10 {
		t.Fatalf("unexpected SandboxIssueMsg: %#v", hostMsgs)
	}
	// Comment on issue via 'c'.
	pressKey(t, m, "c")
	if m.modal != ModalCommentIssue {
		t.Fatal("expected ModalCommentIssue")
	}
	typeText(t, m, "Looks great!")
	pressKey(t, m, "enter")
	if len(fc.addedComments) != 1 || !strings.Contains(fc.addedComments[0], "Looks great!") {
		t.Fatalf("expected comment added, got %v", fc.addedComments)
	}
	// Close issue via 'x' + confirm.
	pressKey(t, m, "x")
	if m.modal != ModalConfirm {
		t.Fatal("expected ModalConfirm for issue close")
	}
	pressKey(t, m, "y")
	if len(fc.stateChanges) != 1 || fc.stateChanges[0] != "alice/nightshift#10=closed" {
		t.Fatalf("expected issue close state change, got %v", fc.stateChanges)
	}
	// New issue via 'n' modal with tab switching to body.
	pressKey(t, m, "n")
	if m.modal != ModalNewIssue {
		t.Fatal("expected ModalNewIssue")
	}
	typeText(t, m, "Add dark mode")
	pressKey(t, m, "tab")
	typeText(t, m, "Full theme support")
	pressKey(t, m, "ctrl+s")
	if len(fc.createdIssues) != 1 || fc.createdIssues[0].Title != "Add dark mode" || fc.createdIssues[0].Body != "Full theme support" {
		t.Fatalf("unexpected created issues: %#v", fc.createdIssues)
	}

	// Scope 4: Actions via ']'.
	pressKey(t, m, "]")
	if m.activeScope != ScopeActions {
		t.Fatalf("activeScope = %v, want ScopeActions", m.activeScope)
	}
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "IN PROGRESS (1)") || !strings.Contains(plain, "FAILED (1)") || !strings.Contains(plain, "PASSED (1)") {
		t.Fatalf("expected grouped actions:\n%s", plain)
	}
	if !strings.Contains(plain, "JOBS") || !strings.Contains(plain, "Checkout") {
		t.Fatalf("expected workflow jobs and steps in Pane 3:\n%s", plain)
	}
	// Rerun workflow via 'R' + confirm.
	pressKey(t, m, "R")
	if m.modal != ModalConfirm {
		t.Fatal("expected ModalConfirm for workflow rerun")
	}
	pressKey(t, m, "y")
	if len(fc.reruns) != 1 || fc.reruns[0] != 1001 {
		t.Fatalf("expected rerun of run 1001, got %v", fc.reruns)
	}
	// S on Action run emits SandboxRepoMsg with run's branch.
	hostMsgs = pressKey(t, m, "S")
	sbxRepo, ok := hostMsgs[0].(SandboxRepoMsg)
	if !ok || sbxRepo.Branch != "feat/repoview" {
		t.Fatalf("unexpected SandboxRepoMsg from action run: %#v", hostMsgs)
	}

	// Scope 5: Files via ']'.
	pressKey(t, m, "]")
	if m.activeScope != ScopeFiles {
		t.Fatalf("activeScope = %v, want ScopeFiles", m.activeScope)
	}
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "FOLDERS (1)") || !strings.Contains(plain, "FILES (3)") {
		t.Fatalf("expected grouped files:\n%s", plain)
	}
	// Enter folder 'cmd'.
	pressKey(t, m, "enter")
	if m.filePath != "cmd" {
		t.Fatalf("filePath = %q, want cmd", m.filePath)
	}
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "..") || !strings.Contains(plain, "app.go") {
		t.Fatalf("expected parent dir and app.go:\n%s", plain)
	}
	// Go back up via 'h'.
	pressKey(t, m, "h")
	if m.filePath != "" {
		t.Fatalf("expected root filePath, got %q", m.filePath)
	}
	// Move down to README.md, main.go, logo.bin and check previews.
	pressKey(t, m, "j") // README.md
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "Rendered via glamour") {
		t.Fatalf("expected markdown preview for README.md:\n%s", plain)
	}
	pressKey(t, m, "j") // logo.bin or main.go
	pressKey(t, m, "j")
	plain = assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "Binary file preview") && !strings.Contains(plain, "package main") {
		t.Fatalf("expected code or binary preview:\n%s", plain)
	}
}

func TestCommandPaletteAndAddRepoModal(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)
	pressKey(t, m, "2") // Branches scope

	// Open command palette and verify scope-specific commands and full input placeholder render.
	pressKey(t, m, ":")
	if m.modal != ModalPalette || !m.Capturing() {
		t.Fatal("expected ModalPalette open")
	}
	paletteFrame := assertExactFrame(t, m, 140, 40)
	if !strings.Contains(paletteFrame, "Type a command…") {
		t.Fatalf("expected full palette placeholder, got:\n%s", paletteFrame)
	}

	// Filter to "Switch to PRs" and execute.
	typeText(t, m, "Pull Requests")
	pressKey(t, m, "enter")
	if m.modal != ModalNone {
		t.Fatal("expected palette closed after execution")
	}
	if m.activeScope != ScopePRs {
		t.Fatalf("expected ScopePRs after palette command, got %v", m.activeScope)
	}

	// Open palette again in PRs scope and check PR-specific command appears.
	pressKey(t, m, ":")
	cmds := m.filteredPaletteCommands()
	foundPRCmd := false
	for _, c := range cmds {
		if strings.Contains(c.Label, "Open PR #42") {
			foundPRCmd = true
			break
		}
	}
	if !foundPRCmd {
		t.Fatal("expected context-scoped 'Open PR #42' command in palette")
	}
	pressKey(t, m, "esc")

	// Add repo modal: search query then pick result.
	pressKey(t, m, "a")
	if m.modal != ModalAddRepo {
		t.Fatal("expected ModalAddRepo")
	}
	typeText(t, m, "orbiter")
	pressKey(t, m, "enter") // triggers search
	if len(m.addRepoResults) != 1 || m.addRepoResults[0].FullName != "space/orbiter" {
		t.Fatalf("unexpected search results: %#v", m.addRepoResults)
	}
	assertExactFrame(t, m, 140, 40)
	pressKey(t, m, "enter") // selects space/orbiter
	if m.SelectedRepo() != "space/orbiter" {
		t.Fatalf("SelectedRepo = %q, want space/orbiter", m.SelectedRepo())
	}

	// Add repo modal: direct owner/repo entry.
	pressKey(t, m, "a")
	typeText(t, m, "org/direct")
	pressKey(t, m, "enter")
	if m.SelectedRepo() != "org/direct" {
		t.Fatalf("SelectedRepo = %q, want org/direct", m.SelectedRepo())
	}
}

func TestActiveSandboxBadgesAndFocusMsg(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	prVol := v1.PRVolumeName("alice/nightshift", 42)
	issVol := v1.IssueVolumeName("alice/nightshift", 10)
	m.SetSandboxes([]ActiveSandbox{
		{Pod: "pi-pr-42-xyz", Volume: prVol, Phase: "Running"},
		{Pod: "claude-iss-10-abc", Volume: issVol, Phase: "Running"},
	})

	// Repo overview should show ACTIVE SANDBOXES (2) and green dot in nav.
	plain := assertExactFrame(t, m, 140, 40)
	if !strings.Contains(plain, "ACTIVE SANDBOXES (2)") || !strings.Contains(plain, "pi-pr-42-xyz") {
		t.Fatalf("expected active sandboxes in Repo Overview:\n%s", plain)
	}

	// Switch to PRs scope; PR #42 should match pi-pr-42-xyz on 'b'.
	pressKey(t, m, "2")
	pressKey(t, m, "]")
	hostMsgs := pressKey(t, m, "b")
	if len(hostMsgs) != 1 {
		t.Fatalf("expected FocusSandboxMsg, got %v", hostMsgs)
	}
	focus, ok := hostMsgs[0].(FocusSandboxMsg)
	if !ok || focus.Pod != "pi-pr-42-xyz" {
		t.Fatalf("unexpected FocusSandboxMsg: %#v", hostMsgs[0])
	}

	// Switch to Issues scope; Issue #10 should match claude-iss-10-abc on 'b'.
	pressKey(t, m, "]")
	hostMsgs = pressKey(t, m, "b")
	focus, ok = hostMsgs[0].(FocusSandboxMsg)
	if !ok || focus.Pod != "claude-iss-10-abc" {
		t.Fatalf("unexpected FocusSandboxMsg for issue: %#v", hostMsgs)
	}
}

func TestResponsiveLayoutsAndExactFrameDimensions(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	// Wide 140x40 -> 3 panes.
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	pl := m.computeLayout()
	if !pl.showNav || !pl.showList || !pl.showDetails {
		t.Fatalf("expected 3 panes at 140x40, got %+v", pl)
	}
	assertExactFrame(t, m, 140, 40)

	// Toggle help drawer at wide width and check exact frame.
	pressKey(t, m, "?")
	assertExactFrame(t, m, 140, 40)
	pressKey(t, m, "?")

	// Toggle < (hide nav), > (hide details), v (zoom details).
	pressKey(t, m, "<")
	if m.computeLayout().showNav {
		t.Fatal("expected nav hidden after <")
	}
	assertExactFrame(t, m, 140, 40)
	pressKey(t, m, "<")

	pressKey(t, m, ">")
	if m.computeLayout().showDetails {
		t.Fatal("expected details hidden after >")
	}
	assertExactFrame(t, m, 140, 40)
	pressKey(t, m, ">")

	pressKey(t, m, "v")
	pl = m.computeLayout()
	if !pl.showDetails || pl.showList {
		t.Fatalf("expected zoomed details over list, got %+v", pl)
	}
	assertExactFrame(t, m, 140, 40)
	pressKey(t, m, "v")

	// Medium 95x30 -> 1+2 when PaneNav focused, 2+3 when PaneList focused.
	m.Update(tea.WindowSizeMsg{Width: 95, Height: 30})
	m.activePane = PaneNav
	pl = m.computeLayout()
	if !pl.showNav || !pl.showList || pl.showDetails {
		t.Fatalf("expected 1+2 at 95x30 with PaneNav focused, got %+v", pl)
	}
	assertExactFrame(t, m, 95, 30)

	m.activePane = PaneList
	pl = m.computeLayout()
	if pl.showNav || !pl.showList || !pl.showDetails {
		t.Fatalf("expected 2+3 at 95x30 with PaneList focused, got %+v", pl)
	}
	assertExactFrame(t, m, 95, 30)

	// Narrow 65x24 -> active pane only.
	m.Update(tea.WindowSizeMsg{Width: 65, Height: 24})
	for _, p := range []Pane{PaneNav, PaneList, PaneDetails} {
		m.activePane = p
		pl = m.computeLayout()
		visible := 0
		if pl.showNav {
			visible++
		}
		if pl.showList {
			visible++
		}
		if pl.showDetails {
			visible++
		}
		if visible != 1 {
			t.Fatalf("expected 1 pane at 65x24 for pane %v, got %+v", p, pl)
		}
		assertExactFrame(t, m, 65, 24)
	}
}

func TestMouseClicksWheelAndDividerDrag(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	pl := m.computeLayout()

	// Click on scope tab "PRs" in Pane 2 (row 1).
	spans, _ := m.scopeTabSpans()
	prsX := pl.listX + spans[1].start
	_, cmd := m.Update(tea.MouseClickMsg{X: prsX, Y: 1, Button: tea.MouseLeft})
	runAllCmds(t, m, cmd)
	if m.activeScope != ScopePRs || m.activePane != PaneList {
		t.Fatalf("expected ScopePRs + PaneList after clicking PRs tab, got scope=%v pane=%v", m.activeScope, m.activePane)
	}

	// Double-click on first PR row (row 4: 0=hdr, 1=tabs, 2=rule, 3=OPEN heading, 4=PR #42).
	_, cmd = m.Update(tea.MouseClickMsg{X: pl.listX + 4, Y: 4, Button: tea.MouseLeft})
	runAllCmds(t, m, cmd)
	_, cmd = m.Update(tea.MouseClickMsg{X: pl.listX + 4, Y: 4, Button: tea.MouseLeft})
	hostMsgs := runAllCmds(t, m, cmd)
	if len(hostMsgs) != 1 {
		t.Fatalf("expected OpenPRMsg on double-click, got %v", hostMsgs)
	}
	if pr, ok := hostMsgs[0].(OpenPRMsg); !ok || pr.Number != 42 {
		t.Fatalf("unexpected msg on double-click: %#v", hostMsgs[0])
	}

	// Wheel down over PaneDetails scrolls details.
	m.detailsScroll = 0
	_, _ = m.Update(tea.MouseWheelMsg{X: pl.detailsX + 5, Y: 5, Button: tea.MouseWheelDown})
	if m.detailsScroll == 0 {
		t.Fatal("expected detailsScroll to increase on wheel down")
	}

	// Drag divider 1 to resize PaneNav wider.
	origDiv1 := pl.div1X
	_, _ = m.Update(tea.MouseClickMsg{X: origDiv1, Y: 5, Button: tea.MouseLeft})
	if m.draggingDiv != 1 {
		t.Fatalf("expected draggingDiv=1, got %d", m.draggingDiv)
	}
	_, _ = m.Update(tea.MouseMotionMsg{X: origDiv1 + 8, Y: 5})
	_, _ = m.Update(tea.MouseReleaseMsg{X: origDiv1 + 8, Y: 5})
	if m.draggingDiv != 0 {
		t.Fatal("expected draggingDiv cleared on release")
	}
	newPL := m.computeLayout()
	if newPL.navW <= pl.navW {
		t.Fatalf("expected navW to grow after dragging right: old=%d new=%d", pl.navW, newPL.navW)
	}
	assertExactFrame(t, m, 140, 40)

	// Footer click on PRs button emits SwitchViewMsg{View: config.PRsView}.
	// Find a column inside PRs button.
	prsFootX := -1
	for x := 0; x < m.width; x++ {
		if m.footerHitTest(x) == footerHitPRs {
			prsFootX = x
			break
		}
	}
	if prsFootX < 0 {
		t.Fatal("could not locate PRs button in footer")
	}
	_, cmd = m.Update(tea.MouseClickMsg{X: prsFootX, Y: newPL.bodyH, Button: tea.MouseLeft})
	hostMsgs = runAllCmds(t, m, cmd)
	if len(hostMsgs) != 1 {
		t.Fatalf("expected SwitchViewMsg on footer click, got %v", hostMsgs)
	}
	if sw, ok := hostMsgs[0].(SwitchViewMsg); !ok || sw.View != config.PRsView {
		t.Fatalf("unexpected SwitchViewMsg: %#v", hostMsgs[0])
	}
}

func TestClipboardAndBrowserHelpers(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	var opened, copied string
	m.openBrowser = func(u string) error { opened = u; return nil }
	m.copyClipboard = func(s string) error { copied = s; return nil }

	pressKey(t, m, "o")
	if opened != "https://github.com/alice/nightshift" {
		t.Fatalf("opened = %q, want repo URL", opened)
	}
	pressKey(t, m, "y")
	if copied != "https://github.com/alice/nightshift" {
		t.Fatalf("copied URL = %q", copied)
	}
	pressKey(t, m, "Y")
	if copied != "alice/nightshift" {
		t.Fatalf("copied repo ref = %q", copied)
	}
}

func TestSandboxForBranchMatchesHashedRepoVolumeName(t *testing.T) {
	longRepo := "very-long-organization-name/extremely-long-repository-name"
	longBranch := "feature/implement-repository-workspace-branch-matching"
	longVol := v1.BranchVolumeName(longRepo, longBranch)
	got, ok := sandboxForBranch([]ActiveSandbox{
		{Pod: "pi-long-1", Volume: longVol, Phase: "Running"},
	}, longRepo, longBranch)
	if !ok || got.Pod != "pi-long-1" {
		t.Fatalf("sandboxForBranch failed on hashed volume %q: got=(%#v, %v)", longVol, got, ok)
	}
}

func TestSwitchViewKeyAndQuit(t *testing.T) {
	fc := newFakeClient()
	m := newTestModel(t, fc)

	hostMsgs := pressKey(t, m, "s")
	if len(hostMsgs) != 1 {
		t.Fatalf("expected SwitchViewMsg on 's', got %v", hostMsgs)
	}
	if sw, ok := hostMsgs[0].(SwitchViewMsg); !ok || sw.View != config.NotificationsView {
		t.Fatalf("unexpected SwitchViewMsg on 's': %#v, want NotificationsView", hostMsgs[0])
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd on 'q'")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatalf("expected tea.QuitMsg on 'q', got %T", cmd())
	}
}

func TestNavScrollDebouncesFetches(t *testing.T) {
	fc := newFakeClient()
	fc.repos = make([]Repo, 25)
	fc.overviews = make(map[string]Repo, 25)
	fc.branches = make(map[string][]Branch, 25)
	for i := range 25 {
		fn := "alice/repo-" + intStr(i)
		r := Repo{
			Owner:         "alice",
			Name:          "repo-" + intStr(i),
			FullName:      fn,
			DefaultBranch: "main",
			URL:           "https://github.com/" + fn,
			Readme:        "# Repo " + intStr(i) + "\n\nOverview.\n",
		}
		fc.repos[i] = r
		fc.overviews[fn] = r
		fc.branches[fn] = []Branch{{Name: "main", SHA: "abcdef1", IsDefault: true}}
	}

	th := *ghtheme.DefaultTheme
	styles := ghctx.InitStyles(th)
	m := New(th, styles, Options{
		CurrentRepo: "alice/repo-0",
		PinsPath:    filepath.Join(t.TempDir(), "pins.json"),
		Client:      fc,
	})
	if m.fetchDebounce != defaultFetchDebounce {
		t.Fatalf("default fetchDebounce = %v, want %v", m.fetchDebounce, defaultFetchDebounce)
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m.loaded = true
	m.viewer = "alice"
	m.repos = append([]Repo(nil), fc.repos...)
	m.expandedRepos = map[string]bool{}
	m.ensureNavCursorValid()
	m.syncSelectionFromNav()

	// Rapidly press 'j' 20 times across uncached repos without firing the debounce timer.
	var firstCmd tea.Cmd
	nonNilCmds := 0
	for i := range 20 {
		_, cmd := m.Update(keyMsg("j"))
		if cmd != nil {
			nonNilCmds++
			if firstCmd == nil {
				firstCmd = cmd
			}
		}
		_ = m.View()
		if i == 0 && cmd == nil {
			t.Fatal("expected initial debounce timer cmd on first uncached scroll step")
		}
	}

	if nonNilCmds != 1 {
		t.Fatalf("expected 1 coalesced debounce cmd across 20 rapid scroll steps, got %d", nonNilCmds)
	}
	if m.SelectedRepo() != "alice/repo-20" {
		t.Fatalf("SelectedRepo = %q, want alice/repo-20", m.SelectedRepo())
	}

	fc.mu.Lock()
	gotOverview, gotBranches := len(fc.overviewCalls), len(fc.branchCalls)
	fc.mu.Unlock()
	if gotOverview != 0 || gotBranches != 0 {
		t.Fatalf("expected 0 API calls before debounce fires, got overview=%d branches=%d", gotOverview, gotBranches)
	}

	// Stale debounce sequence numbers must be ignored.
	_, staleCmd1 := m.Update(fetchDebounceMsg{seq: m.fetchSeq - 1})
	_, staleCmd2 := m.Update(fetchDebounceMsg{seq: m.fetchSeq + 1})
	if staleCmd1 != nil || staleCmd2 != nil {
		t.Fatalf("expected nil cmd for stale debounce seqs, got %v / %v", staleCmd1, staleCmd2)
	}
	fc.mu.Lock()
	gotOverview, gotBranches = len(fc.overviewCalls), len(fc.branchCalls)
	fc.mu.Unlock()
	if gotOverview != 0 || gotBranches != 0 {
		t.Fatalf("expected 0 API calls after stale debounce msgs, got overview=%d branches=%d", gotOverview, gotBranches)
	}

	// Delivering the matching final debounce msg triggers fetches ONLY for alice/repo-20.
	_, finalCmd := m.Update(fetchDebounceMsg{seq: m.fetchSeq})
	if finalCmd == nil {
		t.Fatal("expected batch fetch cmd when final debounce msg fires")
	}
	runAllCmds(t, m, finalCmd)

	fc.mu.Lock()
	overviewCalls := append([]string(nil), fc.overviewCalls...)
	branchCalls := append([]string(nil), fc.branchCalls...)
	fc.mu.Unlock()
	if len(overviewCalls) != 1 || overviewCalls[0] != "alice/repo-20" {
		t.Fatalf("overviewCalls = %v, want [alice/repo-20]", overviewCalls)
	}
	if len(branchCalls) != 1 || branchCalls[0] != "alice/repo-20" {
		t.Fatalf("branchCalls = %v, want [alice/repo-20]", branchCalls)
	}
}

func TestTrimReadmePreviewAndFullView(t *testing.T) {
	short := "# Short\n\nJust two lines.\n"
	got, trimmed := trimReadmePreview(short, 48)
	if trimmed || got != short {
		t.Fatalf("short readme should not be trimmed: trimmed=%v", trimmed)
	}

	// Cut inside an open triple-backtick code fence must cleanly close the fence.
	var sb strings.Builder
	sb.WriteString("# Big Repo\n\n```go\n")
	for range 60 {
		sb.WriteString("fmt.Println(\"hello\")\n")
	}
	sb.WriteString("```\n\nTAIL_MARKER_ONLY_IN_FULL_README\n")
	longMD := sb.String()

	preview, trimmed := trimReadmePreview(longMD, 48)
	if !trimmed {
		t.Fatal("expected long readme to be trimmed")
	}
	if strings.Contains(preview, "TAIL_MARKER_ONLY_IN_FULL_README") {
		t.Fatal("expected tail marker omitted from preview")
	}
	if strings.Count(preview, "```")%2 != 0 {
		t.Fatalf("expected balanced code fences after trim, got:\n%s", preview)
	}

	// In PaneNav at scroll 0, overview shows preview hint and omits tail marker;
	// focusing PaneDetails ('3') switches to full README.
	fc := newFakeClient()
	r := fc.overviews["alice/nightshift"]
	r.Readme = longMD
	fc.overviews["alice/nightshift"] = r
	m := newTestModel(t, fc)

	plain := assertExactFrame(t, m, 140, 40)
	if strings.Contains(plain, "TAIL_MARKER_ONLY_IN_FULL_README") {
		t.Fatal("did not expect full tail marker in PaneNav preview")
	}
	// Focus Details pane and scroll to bottom ('G') to verify full README renders.
	pressKey(t, m, "3")
	pressKey(t, m, "G")
	fullPlain := assertExactFrame(t, m, 140, 40)
	if !strings.Contains(fullPlain, "TAIL_MARKER_ONLY_IN_FULL_README") {
		t.Fatalf("expected full README tail marker when PaneDetails focused and scrolled:\n%s", fullPlain)
	}
}
