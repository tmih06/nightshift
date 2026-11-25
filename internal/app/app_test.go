package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/accounts"
	"github.com/tmih06/nightshift/internal/ghdash/config"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

// fakePRs stands in for gh-dash: it records what reaches it and can claim
// the keyboard like a focused search bar.
type fakePRs struct {
	capturing    bool
	keys         []string
	mice         []tea.Mouse
	other        int
	views        int
	switchedView []config.ViewType
}

func (f *fakePRs) Init() tea.Cmd { return nil }

func (f *fakePRs) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		f.keys = append(f.keys, msg.String())
	case tea.MouseMsg:
		f.mice = append(f.mice, msg.Mouse())
	case tea.WindowSizeMsg:
	default:
		f.other++
	}
	return f, nil
}

func (f *fakePRs) View() tea.View {
	f.views++
	view := tea.NewView("GH-DASH BODY")
	view.Cursor = tea.NewCursor(3, 2)
	return view
}

func (f *fakePRs) Capturing() bool { return f.capturing }

func (f *fakePRs) SwitchView(view config.ViewType) tea.Cmd {
	f.switchedView = append(f.switchedView, view)
	return nil
}

func sized(m Model, w, h int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

// fakeAccounts is the Accounts tab's backend; it counts reads of the cluster.
type fakeAccounts struct{ fetches int }

func (f *fakeAccounts) Fetch(context.Context) (accounts.Snapshot, error) {
	f.fetches++
	return accounts.Snapshot{Accounts: []accounts.Account{{Name: "claude-a.json", Provider: "claude", Email: "a@example.com", Active: true}}}, nil
}
func (*fakeAccounts) StartLogin(context.Context, string) (accounts.Login, error) {
	return accounts.Login{}, nil
}
func (*fakeAccounts) CompleteLogin(context.Context, accounts.Login, string) error { return nil }
func (*fakeAccounts) CancelLogin(context.Context, accounts.Login) error           { return nil }
func (*fakeAccounts) SetDisabled(context.Context, string, bool) error             { return nil }
func (*fakeAccounts) Remove(context.Context, string) error                        { return nil }
func (*fakeAccounts) Refresh(context.Context, string) error                       { return nil }
func (*fakeAccounts) SetActive(context.Context, string, string) error             { return nil }
func (*fakeAccounts) PublishNow(context.Context) (string, error)                  { return "", nil }
func (*fakeAccounts) Close()                                                      {}

func newModel(t *testing.T) (Model, *fakePRs) {
	t.Helper()
	m, f, _ := newModelWithAccounts(t)
	return m, f
}

func newModelWithAccounts(t *testing.T) (Model, *fakePRs, *fakeAccounts) {
	t.Helper()
	f, acc := &fakePRs{}, &fakeAccounts{}
	return sized(NewWithAccounts(*ghtheme.DefaultTheme, f, nil, acc), 120, 40), f, acc
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(Model)
	}
	return m
}

func frame(m Model) []string { return strings.Split(ansi.Strip(m.render()), "\n") }

func TestActiveTabRendersOnceWithItsCursor(t *testing.T) {
	m, tab := newModel(t)
	view := press(m, "tab").View()
	if tab.views != 1 || !strings.Contains(view.Content, "GH-DASH BODY") {
		t.Fatalf("view used %d frames from active tab", tab.views)
	}
	if view.Cursor == nil || view.Cursor.X != 3 || view.Cursor.Y != 2+barHeight {
		t.Errorf("cursor = %+v, want active tab's cursor below the tab bar", view.Cursor)
	}
}

func TestTabCyclesTheTabsBothWays(t *testing.T) {
	m, _ := newModel(t)
	steps := []struct {
		key  string
		want Tab
	}{
		{"tab", TabGitHub},
		{"tab", TabAccounts},
		{"tab", TabSandboxes},
		{"shift+tab", TabAccounts},
		{"shift+tab", TabGitHub},
		{"shift+tab", TabSandboxes},
	}
	for _, s := range steps {
		m = press(m, s.key)
		if m.active != s.want {
			t.Fatalf("%s landed on tab %d, want %d", s.key, m.active, s.want)
		}
	}
}

func TestTheBarIsGhDashsViewSwitcher(t *testing.T) {
	m, _ := newModel(t)
	rows := frame(m)
	if len(rows) != 40 {
		t.Fatalf("frame is %d rows, want 40", len(rows))
	}
	for _, want := range []string{"Pods", "GitHub", "Accounts", "NIGHTSHIFT", "│"} {
		if !strings.Contains(rows[0], want) {
			t.Errorf("bar %q is missing %q", rows[0], want)
		}
	}
	if w := ansi.StringWidth(strings.Split(m.render(), "\n")[0]); w != 120 {
		t.Errorf("bar is %d wide, want 120", w)
	}
	// The active view is bold and the idle one is not.
	bar := strings.Split(m.renderBar(), "GitHub")[0]
	if !strings.Contains(bar, "1;") && !strings.Contains(bar, ";1m") && !strings.Contains(bar, "[1m") {
		t.Errorf("active tab is not bold: %q", bar)
	}
}

func TestClickingATabSwitchesToIt(t *testing.T) {
	m, _ := newModel(t)
	s := tabSpans(m.counts())[TabGitHub]
	next, _ := m.Update(tea.MouseClickMsg{X: s.from + 2, Y: 0, Button: tea.MouseLeft})
	if got := next.(Model).active; got != TabGitHub {
		t.Errorf("click on the GitHub tab landed on %d", got)
	}
}

func TestThePullRequestTabIsGhDash(t *testing.T) {
	m, f := newModel(t)
	m = press(m, "tab", "j")
	if body := strings.Join(frame(m)[barHeight:], "\n"); !strings.Contains(body, "GH-DASH BODY") {
		t.Errorf("pull request tab body = %q", body)
	}
	if len(f.keys) != 1 || f.keys[0] != "j" {
		t.Errorf("gh-dash got keys %v, want [j]", f.keys)
	}

	// Mouse events reach gh-dash shifted under the bar, so its zones line up.
	next, _ := m.Update(tea.MouseClickMsg{X: 7, Y: 9, Button: tea.MouseLeft})
	m = next.(Model)
	if len(f.mice) != 1 || f.mice[0].Y != 9-barHeight || f.mice[0].X != 7 {
		t.Errorf("gh-dash got mice %v, want one at (7, %d)", f.mice, 9-barHeight)
	}

	// Releasing a drag over the tab bar still notifies the active tab.
	next, _ = m.Update(tea.MouseReleaseMsg{X: 7, Y: 0, Button: tea.MouseLeft})
	m = next.(Model)
	if len(f.mice) != 2 || f.mice[1].Y != -barHeight {
		t.Errorf("gh-dash did not receive MouseReleaseMsg over tab bar: %v", f.mice)
	}
}

func TestTabStaysInsideAGhDashTextBox(t *testing.T) {
	m, f := newModel(t)
	m = press(m, "tab")
	f.capturing = true
	m = press(m, "tab")
	if m.active != TabGitHub {
		t.Errorf("tab in a focused search bar switched to tab %d", m.active)
	}
	if len(f.keys) != 1 || f.keys[0] != "tab" {
		t.Errorf("gh-dash got keys %v, want the tab", f.keys)
	}
}

func TestSandboxMessagesSkipGhDash(t *testing.T) {
	m, f := newModel(t)
	type unknownMsg struct{}
	next, _ := m.Update(unknownMsg{})
	if f.other != 1 {
		t.Fatalf("an unknown message reached gh-dash %d times, want 1", f.other)
	}
	_ = next
}

func TestWithoutGhDashThePullRequestTabSaysWhy(t *testing.T) {
	m := sized(New(*ghtheme.DefaultTheme, nil, errors.New("not logged in")), 100, 30)
	m = press(m, "tab")
	body := strings.Join(frame(m), "\n")
	if !strings.Contains(body, "not logged in") || !strings.Contains(body, "gh auth login") {
		t.Errorf("unavailable tab = %q, want the error and the way out", body)
	}
	if _, cmd := m.Update(key("q")); cmd == nil {
		t.Error("q on the unavailable tab does not quit")
	}
}

// asModel narrows what Update returns back to the root model.
func asModel(t *testing.T, m tea.Model) Model {
	t.Helper()
	root, ok := m.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want the root Model", m)
	}
	return root
}

// run executes cmd and feeds what it returns back into m, as the program
// would. A command that blocks, like a poll timer, is dropped after a moment.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(80 * time.Millisecond):
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = run(t, m, c)
		}
		return m
	}
	if msg == nil {
		return m
	}
	next, c := m.Update(msg)
	return run(t, asModel(t, next), c)
}

func TestTheAccountsTabReadsTheClusterOnlyOnceOpened(t *testing.T) {
	m, _, acc := newModelWithAccounts(t)
	if acc.fetches != 0 {
		t.Fatalf("fetched %d times before the tab was opened", acc.fetches)
	}
	next, cmd := m.Update(key("tab"))
	m = run(t, asModel(t, next), cmd) // GitHub
	if acc.fetches != 0 {
		t.Fatalf("fetched %d times on the way past", acc.fetches)
	}
	next, cmd = m.Update(key("tab"))
	m = run(t, asModel(t, next), cmd) // Accounts
	if acc.fetches != 1 {
		t.Errorf("opening the Accounts tab fetched %d times, want 1", acc.fetches)
	}
	if body := strings.Join(frame(m)[barHeight:], "\n"); !strings.Contains(body, "a@example.com") {
		t.Errorf("the Accounts tab does not show the account:\n%s", body)
	}
	if !strings.Contains(frame(m)[0], "Accounts (1)") {
		t.Errorf("the tab bar must count the accounts: %q", frame(m)[0])
	}
}

func TestLeavingTheAccountsTabStopsItsPolling(t *testing.T) {
	m, _, acc := newModelWithAccounts(t)
	m = run(t, m, func() tea.Msg { return nil })
	next, cmd := m.Update(key("shift+tab")) // Accounts
	m = run(t, asModel(t, next), cmd)
	before := acc.fetches
	next, cmd = m.Update(key("tab")) // Pods
	m = run(t, asModel(t, next), cmd)
	if m.active != TabSandboxes {
		t.Fatalf("active = %d", m.active)
	}
	if got := m.accounts.Capturing(); got {
		t.Error("a hidden tab must not hold the keyboard")
	}
	if acc.fetches != before {
		t.Errorf("leaving the tab read the cluster %d more times", acc.fetches-before)
	}
}

func TestTabBelongsToTheAccountsDialogWhileItIsOpen(t *testing.T) {
	m, _, _ := newModelWithAccounts(t)
	next, cmd := m.Update(key("shift+tab")) // Accounts
	m = run(t, asModel(t, next), cmd)
	m = press(m, "a") // the sign-in dialog opens
	if !m.capturing() {
		t.Fatal("the dialog must hold the keyboard")
	}
	m = press(m, "tab")
	if m.active != TabAccounts {
		t.Errorf("tab left the Accounts tab while its dialog was open (active = %d)", m.active)
	}
}
