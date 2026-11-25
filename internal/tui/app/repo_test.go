package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/ghdash/config"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	"github.com/tmih06/nightshift/internal/tui/dashboard"
	"github.com/tmih06/nightshift/internal/tui/fleet"
	"github.com/tmih06/nightshift/internal/tui/repoview"
)

// fakeRepoClient is a minimal in-memory repoview.Client for app integration tests.
type fakeRepoClient struct{}

func (fakeRepoClient) Viewer(context.Context) (string, error) { return "alice", nil }

func (fakeRepoClient) ListRepos(context.Context, int) ([]repoview.Repo, error) {
	return []repoview.Repo{
		{
			Owner:         "acme",
			Name:          "rocket",
			FullName:      "acme/rocket",
			Description:   "Orbital launch control",
			URL:           "https://github.com/acme/rocket",
			DefaultBranch: "main",
			Language:      "Go",
			Visibility:    "public",
			UpdatedAt:     time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC),
		},
	}, nil
}

func (fakeRepoClient) SearchRepos(context.Context, string, int) ([]repoview.Repo, error) {
	return nil, nil
}

func (fakeRepoClient) GetRepoOverview(_ context.Context, repo string) (repoview.Repo, error) {
	return repoview.Repo{
		Owner:         "acme",
		Name:          "rocket",
		FullName:      repo,
		Description:   "Orbital launch control",
		URL:           "https://github.com/" + repo,
		DefaultBranch: "main",
		Language:      "Go",
		Visibility:    "public",
		Readme:        "# Rocket\n",
	}, nil
}

func (fakeRepoClient) ListBranches(context.Context, string) ([]repoview.Branch, error) {
	return []repoview.Branch{
		{Name: "main", SHA: "abc1234", IsDefault: true},
		{Name: "develop", SHA: "def5678"},
	}, nil
}

func (fakeRepoClient) ListBranchCommits(context.Context, string, string, int) ([]repoview.Commit, error) {
	return []repoview.Commit{{SHA: "abc1234", ShortSHA: "abc1234", Message: "init", Author: "alice"}}, nil
}

func (fakeRepoClient) CreateBranch(context.Context, string, string, string) error { return nil }
func (fakeRepoClient) DeleteBranch(context.Context, string, string) error         { return nil }

func (fakeRepoClient) ListPRs(context.Context, string, int) ([]repoview.PullRequest, error) {
	return []repoview.PullRequest{
		{
			Number:  7,
			Title:   "Fix the auth retry",
			State:   "OPEN",
			Author:  "alice",
			HeadRef: "fix-auth",
			BaseRef: "main",
			URL:     "https://github.com/acme/rocket/pull/7",
		},
	}, nil
}

func (fakeRepoClient) ListIssues(context.Context, string, int) ([]repoview.Issue, error) {
	return []repoview.Issue{
		{
			Number: 42,
			Title:  "Fix login",
			State:  "OPEN",
			Author: "alice",
			URL:    "https://github.com/acme/rocket/issues/42",
		},
	}, nil
}

func (fakeRepoClient) ListIssueComments(context.Context, string, int) ([]repoview.IssueComment, error) {
	return nil, nil
}
func (fakeRepoClient) CreateIssue(context.Context, string, string, string) (repoview.Issue, error) {
	return repoview.Issue{}, nil
}
func (fakeRepoClient) CommentIssue(context.Context, string, int, string) (repoview.IssueComment, error) {
	return repoview.IssueComment{}, nil
}
func (fakeRepoClient) SetIssueState(context.Context, string, int, string) error { return nil }
func (fakeRepoClient) ListWorkflowRuns(context.Context, string, int) ([]repoview.WorkflowRun, error) {
	return nil, nil
}
func (fakeRepoClient) ListWorkflowJobs(context.Context, string, int64) ([]repoview.WorkflowJob, error) {
	return nil, nil
}
func (fakeRepoClient) RerunWorkflow(context.Context, string, int64, bool) error { return nil }
func (fakeRepoClient) ListDir(context.Context, string, string, string) ([]repoview.TreeEntry, error) {
	return nil, nil
}
func (fakeRepoClient) GetFile(context.Context, string, string, string) (repoview.FileContent, error) {
	return repoview.FileContent{}, nil
}

func withFakeRepos(t *testing.T, m Model) Model {
	t.Helper()
	pinsPath := filepath.Join(t.TempDir(), "pins.json")
	m = m.WithCurrentRepo("acme/rocket")
	m.newRepos = func() *repoview.Model {
		return repoview.New(m.theme, m.styles, repoview.Options{
			CurrentRepo: m.currentRepo,
			PinsPath:    pinsPath,
			Client:      fakeRepoClient{},
		})
	}
	return m
}

func openRepoModel(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, gh := withPools(t)
	m = sized(withFakeRepos(t, m), 140, 40)
	next, cmd := m.Update(ghtui.OpenRepoViewMsg{})
	m = drain(t, next.(Model), cmd)
	return m, gh
}

func TestOpenRepoViewRenders3PanesAndPreservesStateAcrossTabs(t *testing.T) {
	m, _ := openRepoModel(t)

	if m.ghScreen != ghScreenRepos {
		t.Fatalf("ghScreen = %v, want ghScreenRepos", m.ghScreen)
	}
	rows := frame(m)
	if len(rows) != 40 {
		t.Fatalf("frame height = %d, want 40", len(rows))
	}
	for i, r := range rows {
		if w := ansi.StringWidth(r); w > 140 {
			t.Errorf("row %d width = %d, want <= 140", i, w)
		}
	}
	if !strings.Contains(rows[0], "GitHub") || !strings.Contains(rows[0], "NIGHTSHIFT") {
		t.Errorf("tab bar missing expected content: %q", rows[0])
	}
	body := strings.Join(rows[barHeight:], "\n")
	for _, want := range []string{"[1]", "[2]", "[3]", "acme/rocket", "Branches"} {
		if !strings.Contains(body, want) {
			t.Errorf("3-pane Repo view lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "GH-DASH BODY") {
		t.Error("gh-dash still rendered under Repo view")
	}

	// Tab goes round the other tabs and back while keeping Repo view open.
	m = press(m, "tab")
	if m.active != TabAccounts {
		t.Fatalf("active = %v, want TabAccounts", m.active)
	}
	m = press(m, "tab", "tab")
	if m.active != TabGitHub || m.ghScreen != ghScreenRepos {
		t.Fatalf("active=%v ghScreen=%v, want TabGitHub + ghScreenRepos", m.active, m.ghScreen)
	}

	// StartOnRepos emits OpenRepoViewMsg from Init.
	startM, _ := newModel(t)
	startM = sized(withFakeRepos(t, startM).StartOnRepos(), 140, 40)
	if startM.active != TabGitHub || !startM.startRepos {
		t.Fatalf("StartOnRepos did not configure GitHub tab: active=%v startRepos=%v", startM.active, startM.startRepos)
	}
	startM = drain(t, startM, startM.Init())
	if startM.ghScreen != ghScreenRepos || startM.repos == nil {
		t.Fatalf("Init after StartOnRepos did not open Repo view: ghScreen=%v repos=%v", startM.ghScreen, startM.repos != nil)
	}
}

func TestRepoViewSwitchViewReturnsToGhDash(t *testing.T) {
	m, gh := openRepoModel(t)

	next, _ := m.Update(repoview.SwitchViewMsg{View: config.PRsView})
	m = next.(Model)

	if m.ghScreen != ghScreenDash {
		t.Fatalf("ghScreen = %v, want ghScreenDash", m.ghScreen)
	}
	if len(gh.switchedView) != 1 || gh.switchedView[0] != config.PRsView {
		t.Fatalf("fakePRs.SwitchView got %v, want [%v]", gh.switchedView, config.PRsView)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "GH-DASH BODY") {
		t.Errorf("gh-dash did not render after SwitchViewMsg:\n%s", body)
	}
}

func TestOpenPRFromRepoViewReturnsToRepoViewWhileGhDashReturnsToGhDash(t *testing.T) {
	// 1. From Repo view -> zen-octo -> esc -> back to Repo view.
	m, _ := openRepoModel(t)
	f := &fakeGitHub{}
	m = withGitHub(m, f)

	next, cmd := m.Update(repoview.OpenPRMsg{
		Repo:   "acme/rocket",
		Number: 7,
		URL:    "https://github.com/acme/rocket/pull/7",
	})
	m = drain(t, next.(Model), cmd)

	if m.ghScreen != ghScreenPR {
		t.Fatalf("ghScreen = %v, want ghScreenPR", m.ghScreen)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Fix the auth retry") {
		t.Errorf("zen-octo did not render PR:\n%s", body)
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drain(t, next.(Model), cmd)

	if m.ghScreen != ghScreenRepos {
		t.Fatalf("ghScreen after esc = %v, want ghScreenRepos", m.ghScreen)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "[1]") || strings.Contains(body, "GH-DASH BODY") {
		t.Errorf("esc from PR opened in Repo view did not return to Repo view:\n%s", body)
	}

	// 2. From gh-dash -> zen-octo -> esc -> back to gh-dash.
	next, _ = m.Update(repoview.SwitchViewMsg{View: config.PRsView})
	m = next.(Model)
	next, cmd = m.Update(ghtui.OpenPullRequestMsg{
		Repo:   "acme/rocket",
		Number: 7,
		URL:    "https://github.com/acme/rocket/pull/7",
	})
	m = drain(t, next.(Model), cmd)
	if m.ghScreen != ghScreenPR {
		t.Fatalf("ghScreen = %v, want ghScreenPR", m.ghScreen)
	}

	next, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drain(t, next.(Model), cmd)
	if m.ghScreen != ghScreenDash {
		t.Fatalf("ghScreen after esc = %v, want ghScreenDash", m.ghScreen)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "GH-DASH BODY") {
		t.Errorf("esc from PR opened in gh-dash did not return to gh-dash:\n%s", body)
	}
}

func TestRepoViewSandboxMessagesOpenPickersOverRepoView(t *testing.T) {
	// SandboxRepoMsg
	m, _ := openRepoModel(t)
	next, cmd := m.Update(repoview.SandboxRepoMsg{
		Repo:   "acme/rocket",
		Branch: "develop",
		URL:    "https://github.com/acme/rocket",
	})
	m = next.(Model)
	if cmd != nil || m.picker == nil || m.picker.kind != dashboard.CheckoutRepo {
		t.Fatalf("SandboxRepoMsg picker = %+v, cmd=%v", m.picker, cmd != nil)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Check out acme/rocket in a sandbox") ||
		!strings.Contains(body, "volume repo-acme-rocket-develop") {
		t.Errorf("repo picker missing expected text:\n%s", body)
	}

	// SandboxPRMsg
	m, _ = openRepoModel(t)
	next, cmd = m.Update(repoview.SandboxPRMsg{
		Repo:   "acme/rocket",
		Number: 7,
		URL:    "https://github.com/acme/rocket/pull/7",
		Branch: "fix-auth",
	})
	m = next.(Model)
	if cmd != nil || m.picker == nil || m.picker.kind != dashboard.CheckoutPR {
		t.Fatalf("SandboxPRMsg picker = %+v, cmd=%v", m.picker, cmd != nil)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Check out acme/rocket#7 in a sandbox") ||
		!strings.Contains(body, "volume pr-acme-rocket-7") {
		t.Errorf("PR picker missing expected text:\n%s", body)
	}

	// SandboxIssueMsg
	m, _ = openRepoModel(t)
	next, cmd = m.Update(repoview.SandboxIssueMsg{
		Repo:   "acme/rocket",
		Number: 42,
		URL:    "https://github.com/acme/rocket/issues/42",
		Title:  "Fix login",
	})
	m = next.(Model)
	if cmd != nil || m.picker == nil || m.picker.kind != dashboard.CheckoutIssue {
		t.Fatalf("SandboxIssueMsg picker = %+v, cmd=%v", m.picker, cmd != nil)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "Check out acme/rocket#42 in a sandbox") ||
		!strings.Contains(body, "volume issue-acme-rocket-42") {
		t.Errorf("issue picker missing expected text:\n%s", body)
	}
}

func TestFocusSandboxMsgSwitchesToSandboxesAndSelectsPod(t *testing.T) {
	m, _ := openRepoModel(t)
	m.sandboxes = m.sandboxes.WithFleet([]fleet.Sandbox{
		{Name: "pi-warm-1", OwnerKind: "SandboxWarmPool", Owner: "pi-pool", Template: "pi-template", Phase: "Running"},
		{Name: "codex-repo-dev", Volume: v1.BranchVolumeName("acme/rocket", "develop"), Template: "codex-template", Phase: "Pending"},
	}, map[string]fleet.Pool{
		"pi-pool": {Name: "pi-pool", Template: "pi-template", Ready: 1, Desired: 1},
	})

	next, _ := m.Update(repoview.FocusSandboxMsg{Pod: "codex-repo-dev"})
	m = next.(Model)

	if m.active != TabSandboxes {
		t.Fatalf("active = %v, want TabSandboxes", m.active)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "codex-repo-dev") {
		t.Errorf("sandboxes view does not show focused pod:\n%s", body)
	}
}

func TestActiveSandboxSyncPropagatesToRepoView(t *testing.T) {
	m, _ := withPools(t)
	m = sized(withFakeRepos(t, m), 140, 40)

	vol1 := v1.BranchVolumeName("acme/rocket", "main")
	m.sandboxes = m.sandboxes.WithFleet([]fleet.Sandbox{
		{Name: "pi-warm-1", OwnerKind: "SandboxWarmPool", Owner: "pi-pool", Phase: "Running"},
		{Name: "pi-rocket-main", Volume: vol1, Phase: "Running"},
	}, nil)

	// Opening Repo view syncs active volume-backed sandboxes immediately.
	next, cmd := m.Update(ghtui.OpenRepoViewMsg{})
	m = drain(t, next.(Model), cmd)

	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "ACTIVE SANDBOXES (1)") || !strings.Contains(body, "pi-rocket-main") {
		t.Fatalf("expected synced sandbox in Repo overview:\n%s", body)
	}

	// Updating fleet and running broadcast propagates the new sandbox into m.repos.
	vol2 := v1.PRVolumeName("acme/rocket", 7)
	m.sandboxes = m.sandboxes.WithFleet([]fleet.Sandbox{
		{Name: "pi-rocket-main", Volume: vol1, Phase: "Running"},
		{Name: "codex-rocket-pr7", Volume: vol2, Phase: "Running"},
	}, nil)
	next, _ = m.Update(unrelatedMsg{})
	m = next.(Model)

	body = strings.Join(frame(m), "\n")
	if !strings.Contains(body, "ACTIVE SANDBOXES (2)") || !strings.Contains(body, "codex-rocket-pr7") {
		t.Fatalf("expected broadcast sync to update Repo view sandboxes:\n%s", body)
	}
}

func TestRepoViewMessageIsolation(t *testing.T) {
	m, gh := openRepoModel(t)
	ghOtherBefore := gh.other

	// Trigger a repoview internal async command (e.g. pressing 'r' refreshes).
	next, cmd := m.Update(key("r"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected refresh command from Repo view")
	}

	// Extract and deliver all internal repoview.Owns messages through root Update.
	sawOwned := false
	queue := []tea.Cmd{cmd}
	for i := 0; i < 32 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if repoview.Owns(msg) {
			sawOwned = true
			if prWants(msg) {
				t.Errorf("zen-octo prWants(%T) = true, want false", msg)
			}
			next, more := m.Update(msg)
			m = next.(Model)
			queue = append(queue, more)
		}
	}
	if !sawOwned {
		t.Fatal("did not observe any repoview.Owns message from refresh")
	}
	if gh.other != ghOtherBefore {
		t.Errorf("repoview.Owns messages reached gh-dash: got %d extra calls", gh.other-ghOtherBefore)
	}

	// Arbitrary gh-dash/unknown messages are rejected by repoview.Wants.
	if repoview.Wants(unrelatedMsg{}) {
		t.Error("repoview.Wants(unrelatedMsg{}) = true, want false")
	}
	if repoview.Wants(ghtui.OpenPullRequestMsg{}) {
		t.Error("repoview.Wants(OpenPullRequestMsg{}) = true, want false")
	}
}
