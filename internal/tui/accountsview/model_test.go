package accountsview

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

// testNow is the fixed time every test runs at.
func testNow() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

type fakeService struct {
	mu       sync.Mutex
	calls    []string
	snap     v1.Accounts
	fetchErr error
	loginErr error
}

func (f *fakeService) note(s string) {
	f.mu.Lock()
	f.calls = append(f.calls, s)
	f.mu.Unlock()
}

func (f *fakeService) log() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "|")
}

func (f *fakeService) Accounts(context.Context) (v1.Accounts, error) {
	f.note("fetch")
	return f.snap, f.fetchErr
}

func (f *fakeService) StartLogin(_ context.Context, p string) (v1.Login, error) {
	f.note("start " + p)
	return v1.Login{Provider: p, URL: "https://claude.ai/oauth/authorize?state=abc", ID: "abc"}, f.loginErr
}

func (f *fakeService) CompleteLogin(_ context.Context, _ v1.Login, redirect string) error {
	f.note("complete " + redirect)
	return f.loginErr
}
func (f *fakeService) CancelLogin(context.Context, v1.Login) error {
	f.note("cancel")
	return nil
}
func (f *fakeService) SetDisabled(_ context.Context, n string, d bool) error {
	f.note("disabled " + n + " " + map[bool]string{true: "on", false: "off"}[d])
	return nil
}
func (f *fakeService) RemoveAccount(_ context.Context, n string) error {
	f.note("remove " + n)
	return nil
}
func (f *fakeService) RefreshAccount(_ context.Context, n string) error {
	f.note("refresh " + n)
	return nil
}
func (f *fakeService) SetActive(_ context.Context, p, n string) error {
	f.note("active " + p + " " + n)
	return nil
}
func (f *fakeService) Publish(context.Context) (string, error) { f.note("publish"); return "", nil }

func sampleSnapshot() v1.Accounts {
	return v1.Accounts{
		ObservedAt:  testNow(),
		PublishedAt: testNow().Add(-2 * time.Minute),
		Providers: []v1.Provider{
			{ID: "claude", Name: "Claude Code"}, {ID: "codex", Name: "Codex"}, {ID: "antigravity", Name: "Antigravity"},
		},
		Published: map[string]v1.Published{"claude": {Account: "claude-a.json", Label: "a@example.com"}},
		Accounts: []v1.Account{
			{Name: "claude-a.json", Provider: "claude", Email: "a@example.com", Active: true, State: v1.AccountOK,
				ExpiresAt: testNow().Add(54 * time.Minute), LastRefreshAt: testNow().Add(-6 * time.Minute),
				Usage: &v1.AccountUsage{Requests: 120, OK: 115, RateLimited: 5, LastAt: testNow().Add(-3 * time.Minute), Recent: append(make([]int, 143), 9)}},
			{Name: "claude-b.json", Provider: "claude", Email: "b@example.com", State: v1.AccountOK, ExpiresAt: testNow().Add(3 * time.Hour)},
			{Name: "codex-a.json", Provider: "codex", Email: "c@example.com", Active: true, State: v1.AccountOK, ExpiresAt: testNow().Add(5 * 24 * time.Hour)},
			{Name: "agy-a.json", Provider: "antigravity", Email: "d@example.com", Disabled: true, State: v1.AccountDisabled},
		},
	}
}

func newTestModel(svc API) Model {
	m := New(*ghtheme.DefaultTheme, svc)
	m.now = testNow
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

// drain runs cmd and feeds every message it yields back into the model,
// giving up on commands that block, such as the poll timer.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = drain(t, m, c)
			}
			return m
		}
		if msg == nil {
			return m
		}
		next, c := m.Update(msg)
		return drain(t, next, c)
	case <-time.After(80 * time.Millisecond):
		return m
	}
}

func shown(t *testing.T, svc *fakeService) Model {
	t.Helper()
	m := newTestModel(svc)
	m, cmd := m.SetHidden(false)
	return drain(t, m, cmd)
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			r := []rune(k)
			msg = tea.KeyPressMsg{Code: r[0], Text: k}
		}
		next, cmd := m.Update(msg)
		m = drain(t, next, cmd)
	}
	return m
}

func plain(m Model) string { return ansi.Strip(m.View()) }

func TestShowingTheTabFetchesAndDrawsAccounts(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := newTestModel(svc)
	if svc.log() != "" {
		t.Fatal("a tab that was never shown must not touch the cluster")
	}
	m, cmd := m.SetHidden(false)
	m = drain(t, m, cmd)
	view := plain(m)
	for _, want := range []string{
		"Claude Code", "Codex", "Antigravity",
		"a@example.com", "b@example.com", "c@example.com", "d@example.com",
		"120 requests", "published 2m ago", "in use by sandboxes", "disabled",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the tab does not show %q:\n%s", want, view)
		}
	}
	if m.Count() != 4 {
		t.Errorf("Count = %d, want 4", m.Count())
	}
}

func TestEveryLineFitsTheWidth(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	for _, size := range [][2]int{{120, 30}, {90, 24}, {70, 24}, {50, 20}} {
		m := shown(t, svc)
		m, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines, want at most %d", size[0], size[1], len(lines), size[1])
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: line %d is %d cells wide: %q", size[0], size[1], i, w, ansi.Strip(l))
			}
		}
	}
}

func TestMovingSelectsAnotherAccountsDetail(t *testing.T) {
	m := shown(t, &fakeService{snap: sampleSnapshot()})
	if !strings.Contains(plain(m), "claude-a.json") {
		t.Fatal("the first account's file name must be in the detail pane")
	}
	m = press(t, m, "j")
	view := plain(m)
	if !strings.Contains(view, "claude-b.json") || !strings.Contains(view, "Not in use") {
		t.Errorf("after moving down the detail must show claude-b and say it is not in use:\n%s", view)
	}
}

func TestEnterSwitchesTheAgentsAccountAndPublishes(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	press(t, m, "j", "enter")
	got := svc.log()
	if !strings.Contains(got, "active claude claude-b.json|publish") {
		t.Errorf("enter must choose the account and then publish, calls: %s", got)
	}
	if !strings.HasSuffix(got, "fetch") {
		t.Errorf("the list must be read again afterwards, calls: %s", got)
	}
}

func TestDisabledAccountCannotBeChosen(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "G", "enter") // the Antigravity account is disabled
	if strings.Contains(svc.log(), "active") {
		t.Errorf("a disabled account must not be made active: %s", svc.log())
	}
	if !strings.Contains(plain(m), "disabled") {
		t.Error("the tab must say why")
	}
}

func TestToggleRefreshAndPublishCallTheService(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	press(t, m, "d", "r", "p")
	got := svc.log()
	for _, want := range []string{"disabled claude-a.json on", "refresh claude-a.json", "publish"} {
		if !strings.Contains(got, want) {
			t.Errorf("calls lack %q: %s", want, got)
		}
	}
}

func TestRemoveAsksFirst(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "x")
	if !m.Capturing() || !strings.Contains(plain(m), "Remove a@example.com?") {
		t.Fatalf("x must ask for confirmation:\n%s", plain(m))
	}
	m = press(t, m, "n")
	if strings.Contains(svc.log(), "remove") {
		t.Error("n must not remove anything")
	}
	m = press(t, m, "x", "y")
	if !strings.Contains(svc.log(), "remove claude-a.json") {
		t.Errorf("y must remove the account: %s", svc.log())
	}
	if m.Capturing() {
		t.Error("the dialog must close")
	}
}

func TestSigningInAnAccount(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "a")
	if !strings.Contains(plain(m), "Sign in an account") {
		t.Fatalf("a must offer the agents:\n%s", plain(m))
	}
	m = press(t, m, "enter") // Claude Code is preselected: the selected row is a Claude account
	if !strings.Contains(svc.log(), "start claude") {
		t.Fatalf("calls: %s", svc.log())
	}
	view := plain(m)
	for _, want := range []string{"Sign in to Claude Code", "https://claude.ai/oauth/authorize?state=abc", "paste the address"} {
		if !strings.Contains(view, want) {
			t.Errorf("the sign-in step lacks %q:\n%s", want, view)
		}
	}
	for _, r := range "http://localhost:54545/callback?code=xyz&state=abc" {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = press(t, m, "enter")
	if !strings.Contains(svc.log(), "complete http://localhost:54545/callback?code=xyz&state=abc") {
		t.Errorf("the pasted address was not handed over: %s", svc.log())
	}
	if m.Capturing() {
		t.Error("the dialog must close once the sign-in succeeds")
	}
}

func TestFailedSignInStaysOpenWithTheReason(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "a", "enter")
	svc.loginErr = errors.New("account is on a free plan")
	for _, r := range "http://localhost/cb?code=1&state=abc" {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = press(t, m, "enter")
	if !m.Capturing() || !strings.Contains(plain(m), "free plan") {
		t.Errorf("a failed sign-in must show why and let the person retry:\n%s", plain(m))
	}
}

func TestEscapingASignInCancelsIt(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "a", "enter", "esc")
	if !strings.Contains(svc.log(), "cancel") || m.Capturing() {
		t.Errorf("esc must abandon the pending sign-in (calls: %s)", svc.log())
	}
}

func TestUnreachableCLIProxyAPIExplainsHowToInstall(t *testing.T) {
	svc := &fakeService{fetchErr: errors.New("reach CLIProxyAPI: port-forward svc/cpa ended")}
	m := shown(t, svc)
	view := plain(m)
	for _, want := range []string{"unreachable", "Could not read the accounts", "creds-setup.sh", "port-forward"} {
		if !strings.Contains(view, want) {
			t.Errorf("the failure screen lacks %q:\n%s", want, view)
		}
	}
	svc.fetchErr, svc.snap = nil, sampleSnapshot()
	m = press(t, m, "R")
	if !strings.Contains(plain(m), "a@example.com") {
		t.Error("R must try again")
	}
}

func TestAFailedRefreshKeepsTheLastReading(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	svc.fetchErr = errors.New("timeout")
	m = press(t, m, "R")
	if !strings.Contains(plain(m), "a@example.com") || !strings.Contains(plain(m), "last read") {
		t.Errorf("accounts must stay on screen when a refresh fails:\n%s", plain(m))
	}
}

func TestPollingOnlyWhileShown(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	gen := m.gen
	m, _ = m.SetHidden(true)
	before := svc.log()
	m = drain(t, m, func() tea.Msg { return tickMsg{gen: gen} })
	if svc.log() != before {
		t.Error("a hidden tab must not poll")
	}
	m, cmd := m.SetHidden(false)
	m = drain(t, m, cmd)
	if svc.log() == before {
		t.Error("showing the tab again must read the cluster")
	}
	stale := m.gen - 1
	count := strings.Count(svc.log(), "fetch")
	_ = drain(t, m, func() tea.Msg { return tickMsg{gen: stale} })
	if strings.Count(svc.log(), "fetch") != count {
		t.Error("a tick from before the tab was hidden must be ignored")
	}
}

func TestOwnsOnlyItsOwnMessages(t *testing.T) {
	if !Owns(snapshotMsg{}) || !Owns(tickMsg{}) || !Owns(loginDoneMsg{}) {
		t.Error("the tab's own messages must be owned")
	}
	if Owns(tea.WindowSizeMsg{}) || Owns(struct{}{}) {
		t.Error("other messages must not be")
	}
}

func TestClickSelectsAnAccount(t *testing.T) {
	m := shown(t, &fakeService{snap: sampleSnapshot()})
	lines := m.listLines()
	target := -1
	for i, l := range lines {
		if l.acc == 2 {
			target = i
		}
	}
	m, _ = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: target + 1 - m.top})
	if m.cursor != 2 {
		t.Errorf("cursor = %d after clicking the third account, want 2", m.cursor)
	}
}
