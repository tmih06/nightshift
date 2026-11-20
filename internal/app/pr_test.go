package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	zgh "github.com/tmih06/nightshift/internal/zenocto/gh"
	zapp "github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

// fakeGitHub is zen-octo's client as far as opening a pull request goes.
// Anything else it is asked panics on the nil interface, which a test would
// see; the tests here never run the view's own fetches.
type fakeGitHub struct {
	zapp.GitHub
	found   []zgh.PRRef
	findErr error
}

func (f *fakeGitHub) FindPullRequest(_ context.Context, ref zgh.PRRef) (zgh.PullRequest, error) {
	f.found = append(f.found, ref)
	if f.findErr != nil {
		return zgh.PullRequest{}, f.findErr
	}
	return zgh.PullRequest{ID: "PR_7", Number: ref.Number, Repository: ref.Repo(),
		Title: "Fix the auth retry", State: zgh.PRStateOpen, BaseRefName: "main", HeadRefName: "fix-auth"}, nil
}

func withGitHub(m Model, f *fakeGitHub) Model {
	m.newPRClient = func() (PullRequests, error) { return f, nil }
	return m
}

// drain runs cmd and what it leads to, feeding each message back, skipping
// any command that does not answer at once (ticks and fetches).
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; i < 64 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		got := make(chan tea.Msg, 1)
		go func() {
			defer func() {
				if recover() != nil {
					got <- nil // a fetch the fake does not serve
				}
			}()
			got <- c()
		}()
		var msg tea.Msg
		select {
		case msg = <-got:
		case <-time.After(50 * time.Millisecond):
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		next, more := m.Update(msg)
		m = next.(Model)
		queue = append(queue, more)
	}
	return m
}

func openOn(t *testing.T) (Model, *fakeGitHub) {
	t.Helper()
	m, _ := newModel(t)
	f := &fakeGitHub{}
	m = withGitHub(press(m, "tab"), f)
	next, cmd := m.Update(ghtui.OpenPullRequestMsg{Repo: "acme/rocket", Number: 7, URL: "https://github.com/acme/rocket/pull/7"})
	return drain(t, next.(Model), cmd), f
}

func TestEnterOnAPullRequestOpensItInZenOctosView(t *testing.T) {
	m, f := openOn(t)

	if len(f.found) != 1 || f.found[0] != (zgh.PRRef{Owner: "acme", Name: "rocket", Number: 7}) {
		t.Fatalf("looked up %v, want acme/rocket#7 once", f.found)
	}
	if !m.prOpen {
		t.Fatal("the pull request view did not open")
	}
	body := strings.Join(frame(m), "\n")
	for _, want := range []string{"acme/rocket · #7", "Fix the auth retry", "main ← fix-auth", "Conversation"} {
		if !strings.Contains(body, want) {
			t.Errorf("the frame lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "GH-DASH BODY") {
		t.Error("gh-dash still shows under the pull request")
	}
	if !strings.Contains(frame(m)[0], "GitHub") {
		t.Error("the tab bar is gone")
	}
}

func TestEscLeavesThePullRequestForGhDash(t *testing.T) {
	m, _ := openOn(t)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = drain(t, next.(Model), cmd)

	if m.prOpen {
		t.Fatal("esc left the pull request open")
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "GH-DASH BODY") {
		t.Errorf("gh-dash did not come back:\n%s", body)
	}
}

func TestKeysReachThePullRequestAndTabStillSwitches(t *testing.T) {
	m, _ := openOn(t)
	m, gh := m, m.gh.(*fakePRs)
	before := len(gh.keys)

	m = press(m, "]")
	if len(gh.keys) != before {
		t.Error("a key meant for the pull request reached gh-dash")
	}
	if got := m.pr.View().Content; !strings.Contains(got, "Commits") {
		t.Error("the pull request view lost its tabs")
	}

	m = press(m, "tab")
	if m.active != TabSandboxes {
		t.Errorf("tab inside a pull request landed on %d, want the sandboxes", m.active)
	}
	m = press(m, "tab")
	if !m.prOpen || m.active != TabGitHub {
		t.Error("coming back did not find the pull request still open")
	}
}

func TestAPullRequestThatCannotBeFoundSaysSoInTheBar(t *testing.T) {
	m, _ := newModel(t)
	f := &fakeGitHub{findErr: errors.New("no such pull request")}
	m = withGitHub(press(m, "tab"), f)
	next, cmd := m.Update(ghtui.OpenPullRequestMsg{Repo: "acme/rocket", Number: 9})
	m = drain(t, next.(Model), cmd)

	if m.prOpen {
		t.Fatal("a failed lookup opened a view")
	}
	if bar := frame(m)[0]; !strings.Contains(bar, "could not open acme/rocket#9") {
		t.Errorf("bar = %q, want the failure named", bar)
	}
	if body := strings.Join(frame(m), "\n"); !strings.Contains(body, "GH-DASH BODY") {
		t.Error("gh-dash is not showing after the failure")
	}
}

func TestThePullRequestIsPaintedFromGhDashsTheme(t *testing.T) {
	m, _ := newModel(t)
	zt := zenTheme(m.theme, m.styles)
	if zt.Text != m.theme.PrimaryText || zt.SelectedBackground != m.theme.SelectedBackground || zt.Accent != m.styles.Colors.OpenPR {
		t.Error("zen-octo's theme is not gh-dash's colours")
	}
}

func TestOnlyThePullRequestsOwnMessagesReachIt(t *testing.T) {
	if prWants(unrelatedMsg{}) || prWants(ghtui.OpenPullRequestMsg{}) {
		t.Error("gh-dash's traffic would reach the pull request view")
	}
	if !prWants(zapp.ClosedMsg{}) || !prWants(tea.WindowSizeMsg{}) || !prWants(tea.KeyPressMsg{}) {
		t.Error("the pull request view would miss its own messages")
	}
}
