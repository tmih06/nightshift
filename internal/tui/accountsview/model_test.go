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

func (f *fakeService) StartLogin(_ context.Context, p, method string) (v1.Login, error) {
	f.note(strings.TrimSpace("start " + p + " " + method))
	switch method {
	case "device":
		return v1.Login{Provider: p, ID: "abc", Method: v1.LoginMethod{ID: method, Kind: v1.LoginDevice},
			URL: "https://auth.openai.com/codex/device", UserCode: "ABCD-1234", PollSeconds: 5}, f.loginErr
	case "token":
		return v1.Login{Provider: p, ID: "abc", Method: v1.LoginMethod{ID: method, Kind: v1.LoginPaste,
			Label: "Token from claude setup-token", Hint: "Run claude setup-token and paste the token."}}, f.loginErr
	}
	return v1.Login{Provider: p, URL: "https://claude.ai/oauth/authorize?state=abc", ID: "abc",
		Method: v1.LoginMethod{ID: "browser", Kind: v1.LoginBrowser}}, f.loginErr
}

// CompleteLogin finishes at once, except a device sign-in, which is approved
// on the second ask.
func (f *fakeService) CompleteLogin(_ context.Context, l v1.Login, input string) (v1.LoginProgress, error) {
	f.note("complete " + input)
	if l.Method.Kind == v1.LoginDevice && strings.Count(f.log(), "complete") < 2 {
		return v1.LoginProgress{}, f.loginErr
	}
	return v1.LoginProgress{Done: true, Account: "new"}, f.loginErr
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
func (f *fakeService) SetChain(_ context.Context, p string, names []string) error {
	f.note("chain " + p + " " + strings.Join(names, ","))
	return nil
}
func (f *fakeService) Publish(context.Context) (string, error) { f.note("publish"); return "", nil }
func (f *fakeService) RefreshQuotas(context.Context) error     { f.note("refresh-quotas"); return nil }

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
	svc := &fakeService{snap: chainSnapshot()}
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
		"120 requests", "published 2m ago", "active on the chain", "disabled",
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
	if !strings.Contains(view, "claude-b.json") || !strings.Contains(view, "Not active") {
		t.Errorf("after moving down the detail must show claude-b and say it is not active:\n%s", view)
	}
}

func TestEnterSwitchesTheAgentsAccountAndPublishes(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	press(t, m, "j", "enter", "y")
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

func TestDisablingTheAccountInUseAsksThenPublishes(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "d")
	if !m.Capturing() || !strings.Contains(plain(m), "Disable a@example.com?") || !strings.Contains(plain(m), "Cache miss") {
		t.Fatalf("disabling the account in use must warn first:\n%s", plain(m))
	}
	press(t, m, "y")
	// The saved order drops it, so enabling it later moves no traffic.
	if want := "disabled claude-a.json on|chain claude claude-b.json|publish"; !strings.Contains(svc.log(), want) {
		t.Errorf("calls lack %q: %s", want, svc.log())
	}
}

func TestDisablingASpareAccountDoesNotAsk(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "j", "d")
	if m.Capturing() || !strings.Contains(svc.log(), "disabled claude-b.json on|chain claude claude-a.json|publish") {
		t.Errorf("a spare account moves no traffic, so d must just do it: %s", svc.log())
	}
}

func TestTRenewsTheToken(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	press(t, shown(t, svc), "t")
	if !strings.Contains(svc.log(), "refresh claude-a.json") {
		t.Errorf("t must renew the token: %s", svc.log())
	}
}

// chainSnapshot is Claude's two accounts in order, the proxy on the first.
func chainSnapshot() v1.Accounts {
	snap := sampleSnapshot()
	snap.Accounts[0].Priority, snap.Accounts[0].ActiveNow = 1, true
	snap.Accounts[1].Priority = 2
	return snap
}

func TestUsingAnotherAccountWarnsOfTheCacheMiss(t *testing.T) {
	svc := &fakeService{snap: chainSnapshot()}
	m := press(t, shown(t, svc), "j", "u")
	view := plain(m)
	for _, want := range []string{"Use b@example.com for Claude Code?", "from a@example.com", "Cache miss", "y switch", "n cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("the warning lacks %q:\n%s", want, view)
		}
	}
	m = press(t, m, "n")
	if m.Capturing() || strings.Contains(svc.log(), "active") {
		t.Errorf("n must change nothing: %s", svc.log())
	}
	press(t, m, "u", "y")
	if !strings.Contains(svc.log(), "active claude claude-b.json|publish") {
		t.Errorf("y must switch and publish: %s", svc.log())
	}
}

func TestUsingTheAccountInUseChangesNothing(t *testing.T) {
	svc := &fakeService{snap: chainSnapshot()}
	m := press(t, shown(t, svc), "u")
	if m.Capturing() || strings.Contains(svc.log(), "active") || !strings.Contains(plain(m), "already active") {
		t.Errorf("u on the account in use must only say so: %s\n%s", svc.log(), plain(m))
	}
}

// The order says who should carry the traffic; only the proxy says who does.
func TestInUseFollowsTheProxyNotTheOrder(t *testing.T) {
	snap := chainSnapshot()
	snap.Accounts[0].ActiveNow, snap.Accounts[1].ActiveNow = false, true
	snap.Accounts[0].CooldownUntil = testNow().Add(4 * time.Minute)
	m := shown(t, &fakeService{snap: snap})
	if view := plain(m); strings.Contains(view, "active on the chain") || !strings.Contains(view, "first, but not active") {
		t.Errorf("a cooling first account is not in use:\n%s", view)
	}
	if view := plain(press(t, m, "j")); !strings.Contains(view, "active on the chain (failover)") {
		t.Errorf("the account the proxy picked is in use:\n%s", view)
	}

	snap.Accounts[0].CooldownUntil = time.Time{}
	m = shown(t, &fakeService{snap: snap})
	if view := plain(m); !strings.Contains(view, "waiting for the proxy") || !strings.Contains(view, "pending") {
		t.Errorf("a switch the proxy has not picked up must show as pending:\n%s", view)
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
	if !strings.Contains(svc.log(), "remove claude-a.json|publish") {
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
	for _, want := range []string{"Sign in to Claude Code", "https://claude.ai/oauth/authorize?state=abc", "the address the browser ends on"} {
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

// methodSnapshot is sampleSnapshot whose agents offer more than the browser.
func methodSnapshot() v1.Accounts {
	s := sampleSnapshot()
	s.Providers[0].Methods = []v1.LoginMethod{
		{ID: "browser", Label: "Claude account, in the browser", Kind: v1.LoginBrowser, Hint: "Approve, then paste the code."},
		{ID: "token", Label: "Token from claude setup-token", Kind: v1.LoginPaste, Hint: "Run claude setup-token and paste the token."},
	}
	s.Providers[1].Methods = []v1.LoginMethod{
		{ID: "browser", Label: "ChatGPT account, in the browser", Kind: v1.LoginBrowser, Hint: "Approve."},
		{ID: "device", Label: "ChatGPT account, with a device code", Kind: v1.LoginDevice, Hint: "Enter the code on OpenAI's page."},
	}
	return s
}

func TestSigningInWithAPastedTokenHidesIt(t *testing.T) {
	svc := &fakeService{snap: methodSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "a", "enter")
	view := plain(m)
	for _, want := range []string{"Claude account, in the browser", "Token from claude setup-token", "Approve, then paste the code."} {
		if !strings.Contains(view, want) {
			t.Errorf("the ways to sign in lack %q:\n%s", want, view)
		}
	}
	m = press(t, m, "j", "enter")
	if !strings.Contains(svc.log(), "start claude token") || !strings.Contains(plain(m), "Run claude setup-token") {
		t.Fatalf("calls %s, view:\n%s", svc.log(), plain(m))
	}
	m, _ = m.Update(tea.PasteMsg{Content: "sk-ant-oat01-secret"})
	if strings.Contains(plain(m), "sk-ant-oat01-secret") {
		t.Errorf("a pasted token is shown on screen:\n%s", plain(m))
	}
	m = press(t, m, "enter")
	if !strings.Contains(svc.log(), "complete sk-ant-oat01-secret") || m.Capturing() {
		t.Errorf("the token was not handed over and the dialog closed: %s", svc.log())
	}
}

func TestADeviceSignInShowsTheCodeAndFinishesByItself(t *testing.T) {
	svc := &fakeService{snap: methodSnapshot()}
	m := shown(t, svc)
	m = press(t, m, "j", "j") // the Codex account
	m = press(t, m, "a", "enter", "j", "enter")
	view := plain(m)
	if !strings.Contains(svc.log(), "start codex device") || !strings.Contains(view, "ABCD-1234") || !strings.Contains(view, "https://auth.openai.com/codex/device") {
		t.Fatalf("calls %s, view:\n%s", svc.log(), view)
	}
	next, cmd := m.Update(loginPollMsg{id: "abc"})
	m = drain(t, next, cmd)
	if !m.Capturing() || !strings.Contains(plain(m), "waiting for approval") {
		t.Fatalf("an unapproved code must keep waiting:\n%s", plain(m))
	}
	next, cmd = m.Update(loginPollMsg{id: "abc"})
	m = drain(t, next, cmd)
	if m.Capturing() || strings.Count(svc.log(), "complete") != 2 {
		t.Errorf("the approved sign-in must close the dialog (calls: %s)", svc.log())
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

func TestUnreachableLoginServiceExplainsHowToInstall(t *testing.T) {
	svc := &fakeService{fetchErr: errors.New("reach nightshift-auth: port-forward svc/nightshift-auth ended")}
	m := shown(t, svc)
	view := plain(m)
	for _, want := range []string{"unreachable", "Could not read the accounts", "auth-setup.sh", "port-forward"} {
		if !strings.Contains(view, want) {
			t.Errorf("the failure screen lacks %q:\n%s", want, view)
		}
	}
	svc.fetchErr, svc.snap = nil, sampleSnapshot()
	m = press(t, m, "r")
	if !strings.Contains(plain(m), "a@example.com") {
		t.Error("r must try again")
	}
}

func TestAFailedRefreshKeepsTheLastReading(t *testing.T) {
	svc := &fakeService{snap: sampleSnapshot()}
	m := shown(t, svc)
	svc.fetchErr = errors.New("timeout")
	m = press(t, m, "r")
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

func TestReorderChainMovesAccountAndPublishes(t *testing.T) {
	snap := sampleSnapshot()
	snap.Accounts[0].Priority = 1
	snap.Accounts[1].Priority = 2
	snap.Accounts[0].CooldownUntil = testNow().Add(4 * time.Minute)
	snap.Accounts[0].CooldownReason = "rate limited (429)"
	snap.Accounts[0].Failovers = 2
	snap.Accounts[1].ActiveNow = true

	svc := &fakeService{snap: snap}
	m := shown(t, svc)
	view := plain(m)
	for _, want := range []string{"#1", "#2", "cooldown", "rate limited (429)", "Failovers"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	// Move down to claude-b (#2) and press K to move it above claude-a.
	m = press(t, m, "j", "K", "y")
	got := svc.log()
	if !strings.Contains(got, "chain claude claude-b.json,claude-a.json|publish") {
		t.Errorf("K must reorder the fallback chain and publish, calls: %s", got)
	}
	_ = m
}

func TestQuotaAndTokenAnalyticsRenderAndRefresh(t *testing.T) {
	snap := sampleSnapshot()
	snap.Analytics = &v1.AnalyticsSummary{
		Requests: 120, OK: 115, RateLimited: 5,
		Input: 120000, Output: 45000, CacheRead: 1150000, CacheWrite: 110000,
		Total: 1425000, CacheHitPercent: 83.3, AvgLatencyMS: 640,
	}
	snap.Accounts[0].Usage.Input = 120000
	snap.Accounts[0].Usage.Output = 45000
	snap.Accounts[0].Usage.CacheRead = 1150000
	snap.Accounts[0].Usage.CacheWrite = 110000
	snap.Accounts[0].Usage.Reasoning = 12000
	snap.Accounts[0].Usage.Total = 1437000
	snap.Accounts[0].Usage.CacheHitPercent = 83.3
	snap.Accounts[0].Usage.AvgLatencyMS = 640
	snap.Accounts[0].Usage.ByModel = map[string]v1.ModelUsage{
		"claude-sonnet-4-6": {Requests: 120, Input: 120000, Output: 45000, CacheRead: 1150000, Total: 1437000},
	}
	snap.Accounts[0].Quota = &v1.AccountQuota{
		Plan: "max", Source: "fetch",
		FetchedAt:   testNow().Add(-1 * time.Minute),
		NextResetAt: testNow().Add(2*time.Hour + 15*time.Minute),
		Windows: []v1.QuotaWindow{
			{Name: "5h", Label: "5-hour window", UsedPercent: 42, ResetsAt: testNow().Add(2*time.Hour + 15*time.Minute)},
		},
	}

	svc := &fakeService{snap: snap}
	m := shown(t, svc)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 130, Height: 55})
	view := plain(m)
	for _, want := range []string{
		"Quota", "5-hour window", "42%", "next reset in 2h15m",
		"120.0K", "45.0K", "1.44M", "Cache 1.15M read", "83.3% hit", "Reasoning 12.0K",
		"claude-sonnet-4-6", "avg 640ms",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail pane lacks %q:\n%s", want, view)
		}
	}
	m = press(t, m, "r")
	if !strings.Contains(svc.log(), "refresh-quotas") || !strings.HasSuffix(svc.log(), "fetch") {
		t.Errorf("r must refresh live quotas and refetch, calls: %s", svc.log())
	}
	_ = m
}

// A sandbox has no account: the proxy picks one for each request outside it,
// so the tab says which is active and never that sandboxes use one.
func TestTheTabNeverSaysASandboxUsesAnAccount(t *testing.T) {
	m := shown(t, &fakeService{snap: chainSnapshot()})
	for range m.flat {
		m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 80})
		if view := strings.ToLower(plain(m)); strings.Contains(view, "sandbox") {
			t.Errorf("the tab ties an account to a sandbox:\n%s", view)
		}
		m = press(t, m, "j")
	}
}
