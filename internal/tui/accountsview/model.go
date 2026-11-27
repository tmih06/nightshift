// Package accountsview is the Accounts tab: the coding-agent accounts signed
// in to Nightshift, which one is active for each agent, and how much each has
// been used. It draws what internal/accounts fetches and sends its actions
// back; it reaches the cluster only through API.
package accountsview

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/tui/dialog"
)

// Keys the dialogs and the list both answer to.
const (
	keyEsc   = "esc"
	keyEnter = "enter"
	keyCtrlC = "ctrl+c"
)

const (
	pollEvery   = 15 * time.Second
	fetchWindow = 45 * time.Second
	noticeFor   = 6 * time.Second
)

type modalKind int

const (
	modalNone modalKind = iota
	modalProvider
	modalMethod // how to sign in to the chosen agent
	modalLink   // the sign-in itself: a link, a device code, or a paste
	modalConfirm
)

// pasteLimit is how much a paste may hold: a whole login file.
const pasteLimit = 64 << 10

type modal struct {
	kind modalKind

	// provider and method pickers
	choice   int
	provider v1.Provider

	// sign-in link
	login v1.Login
	input textinput.Model
	busy  bool
	err   string

	// confirmation
	title, verb, progress string
	body                  []string
	run                   tea.Cmd
}

// Model is the tab.
type Model struct {
	svc   API
	theme ghtheme.Theme
	st    styles
	now   func() time.Time

	snap    v1.Accounts
	loaded  bool
	loading bool
	err     error

	flat   []v1.Account // accounts in the order the list shows them
	cursor int
	top    int // first list line in view

	scroll      int  // first line of the detail pane's body in view
	allAccounts bool // the calendar adds up every account

	hidden bool
	gen    int // bumped when polling restarts, so a stale tick is ignored
	width  int
	height int
	help   bool

	notice    string
	noticeBad bool
	noticeAt  time.Time

	modal modal
}

// New returns the tab, hidden until it is first shown.
func New(theme ghtheme.Theme, svc API) Model {
	return Model{svc: svc, theme: theme, st: newStyles(theme), now: time.Now, hidden: true}
}

// Count is the number of accounts, for the tab label.
func (m Model) Count() int { return len(m.snap.Accounts) }

// Capturing is whether the tab holds the keyboard for text, so tab belongs to it.
func (m Model) Capturing() bool { return m.modal.kind != modalNone }

// Messages the tab owns; the root model routes them here and nowhere else.
type (
	snapshotMsg struct {
		snap v1.Accounts
		err  error
	}
	tickMsg struct{ gen int }
	noteMsg struct{ at time.Time }
	doneMsg struct {
		text    string
		err     error
		refetch bool
	}
	loginStartedMsg struct {
		login v1.Login
		err   error
	}
	loginDoneMsg struct {
		label string
		id    string
		done  bool // false while a device sign-in waits for approval
		err   error
	}
	// loginPollMsg asks after the device sign-in id again.
	loginPollMsg struct{ id string }
)

// Owns reports whether the tab handles msg itself.
func Owns(msg tea.Msg) bool {
	switch msg.(type) {
	case snapshotMsg, tickMsg, noteMsg, doneMsg, loginStartedMsg, loginDoneMsg, loginPollMsg:
		return true
	}
	return false
}

// Init does nothing: the tab fetches when it is first shown, so a session
// that never opens it never starts a port-forward.
func (m Model) Init() tea.Cmd { return nil }

// SetHidden shows or hides the tab. Showing it fetches and starts polling;
// hiding it stops polling.
func (m Model) SetHidden(hidden bool) (Model, tea.Cmd) {
	if m.hidden == hidden {
		return m, nil
	}
	m.hidden = hidden
	m.gen++
	if hidden {
		return m, nil
	}
	m.loading = true
	return m, tea.Batch(m.fetch(), m.tick())
}

func (m Model) tick() tea.Cmd {
	gen := m.gen
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

func (m Model) fetch() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchWindow)
		defer cancel()
		snap, err := svc.Accounts(ctx)
		return snapshotMsg{snap: snap, err: err}
	}
}

func (m *Model) say(text string, bad bool) tea.Cmd {
	at := m.now()
	m.notice, m.noticeBad, m.noticeAt = text, bad, at
	return tea.Tick(noticeFor, func(time.Time) tea.Msg { return noteMsg{at: at} })
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.keepCursorInView()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.PasteMsg:
		return m.paste(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m.data(msg)
}

// data handles the results of the tab's own commands.
func (m Model) data(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snapshotMsg:
		return m.onSnapshot(msg), nil
	case tickMsg:
		if msg.gen != m.gen || m.hidden {
			return m, nil
		}
		return m, tea.Batch(m.fetch(), m.tick())
	case noteMsg:
		if msg.at.Equal(m.noticeAt) {
			m.notice = ""
		}
	case doneMsg:
		return m.onDone(msg)
	case loginStartedMsg:
		return m.loginStarted(msg)
	case loginDoneMsg:
		return m.loginDone(msg)
	case loginPollMsg:
		return m.pollLogin(msg)
	}
	return m, nil
}

func (m Model) onSnapshot(msg snapshotMsg) Model {
	m.loading = false
	if msg.err != nil {
		m.err = msg.err
		return m
	}
	m.err = nil
	m.loaded = true
	m.setSnapshot(msg.snap)
	return m
}

func (m Model) onDone(msg doneMsg) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if msg.err != nil {
		cmds = append(cmds, m.say(msg.err.Error(), true))
	} else if msg.text != "" {
		cmds = append(cmds, m.say(msg.text, false))
	}
	if msg.refetch {
		cmds = append(cmds, m.fetch())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) paste(msg tea.PasteMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalLink || m.modal.busy || m.modal.login.Method.Kind == v1.LoginDevice {
		return m, nil
	}
	var cmd tea.Cmd
	m.modal.input, cmd = m.modal.input.Update(msg)
	return m, cmd
}

func (m *Model) setSnapshot(s v1.Accounts) {
	selected := ""
	if a, ok := m.selected(); ok {
		selected = a.Name
	}
	scroll := m.scroll
	m.scroll = 0
	m.snap = s
	m.flat = m.flat[:0]
	for _, p := range m.snap.Providers {
		m.flat = append(m.flat, byProvider(s, p.ID)...)
	}
	m.cursor = 0
	for i := range m.flat {
		if m.flat[i].Name == selected {
			m.cursor, m.scroll = i, scroll
		}
	}
	m.keepCursorInView()
}

func (m Model) selected() (v1.Account, bool) {
	if m.cursor < 0 || m.cursor >= len(m.flat) {
		return v1.Account{}, false
	}
	return m.flat[m.cursor], true
}

func (m *Model) move(delta int) {
	if len(m.flat) == 0 {
		return
	}
	m.jump(m.cursor + delta)
}

// jump selects the account at i, or the nearest one, from the top of its
// details.
func (m *Model) jump(i int) {
	was := m.cursor
	m.cursor = max(0, min(len(m.flat)-1, i))
	if m.cursor != was {
		m.scroll = 0
	}
	m.keepCursorInView()
}

func (m Model) key(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch m.modal.kind {
	case modalProvider:
		return m.keyProvider(msg)
	case modalMethod:
		return m.keyMethod(msg)
	case modalLink:
		return m.keyLink(msg)
	case modalConfirm:
		return m.keyConfirm(msg)
	case modalNone:
	}
	if m.help {
		m.help = false
		if msg.String() != "q" && msg.String() != keyCtrlC {
			return m, nil
		}
	}
	if m.navKey(msg.String()) {
		return m, nil
	}
	return m.actionKey(msg.String())
}

// navKey moves around the list and reports whether it was a movement key.
func (m *Model) navKey(k string) bool {
	switch k {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.jump(0)
	case "G", "end":
		m.jump(len(m.flat) - 1)
	case "ctrl+d", "pgdown":
		m.scrollDetail(m.bodyHeight() / 2)
	case "ctrl+u", "pgup":
		m.scrollDetail(-m.bodyHeight() / 2)
	case "v":
		m.allAccounts = !m.allAccounts
	default:
		return false
	}
	return true
}

func (m Model) actionKey(k string) (Model, tea.Cmd) {
	switch k {
	case "q", keyCtrlC:
		return m, tea.Quit
	case "?":
		m.help = true
	case "r":
		return m.reload()
	case "a":
		return m.openProviderPicker(), nil
	default:
		return m.accountKey(k)
	}
	return m, nil
}

// accountKey acts on the selected account.
func (m Model) accountKey(k string) (Model, tea.Cmd) {
	switch k {
	case "u", keyEnter:
		return m.useSelected()
	case "K":
		return m.moveInChain(-1)
	case "J":
		return m.moveInChain(1)
	case "d":
		return m.toggleSelected()
	case "x":
		return m.askRemove(), nil
	case "t":
		return m.renewSelected()
	}
	return m, nil
}

func (m Model) openProviderPicker() Model {
	choice := 0
	if a, ok := m.selected(); ok {
		for i, p := range m.snap.Providers {
			if p.ID == a.Provider {
				choice = i
			}
		}
	}
	m.modal = modal{kind: modalProvider, choice: choice}
	return m
}

func (m Model) keyProvider(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	n := len(m.snap.Providers)
	switch msg.String() {
	case keyEsc, "q", keyCtrlC:
		m.modal = modal{}
	case "j", "down", "tab":
		m.modal.choice = (m.modal.choice + 1) % n
	case "k", "up", "shift+tab":
		m.modal.choice = (m.modal.choice + n - 1) % n
	case keyEnter:
		p := m.snap.Providers[m.modal.choice]
		if len(p.Methods) > 1 {
			m.modal = modal{kind: modalMethod, provider: p}
			return m, nil
		}
		m.modal.busy, m.modal.provider = true, p
		return m, m.startLogin(p.ID, "")
	}
	return m, nil
}

// keyMethod answers the list of ways to sign in to the chosen agent.
func (m Model) keyMethod(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.modal.busy {
		return m, nil
	}
	n := len(m.modal.provider.Methods)
	switch msg.String() {
	case keyEsc, "q", "backspace":
		return m.openProviderPicker(), nil
	case keyCtrlC:
		m.modal = modal{}
	case "j", "down", "tab":
		m.modal.choice = (m.modal.choice + 1) % n
	case "k", "up", "shift+tab":
		m.modal.choice = (m.modal.choice + n - 1) % n
	case keyEnter:
		m.modal.busy = true
		return m, m.startLogin(m.modal.provider.ID, m.modal.provider.Methods[m.modal.choice].ID)
	}
	return m, nil
}

// startLogin asks the services to begin signing in to provider by method.
func (m Model) startLogin(provider, method string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchWindow)
		defer cancel()
		l, err := svc.StartLogin(ctx, provider, method)
		return loginStartedMsg{login: l, err: err}
	}
}

func (m Model) loginStarted(msg loginStartedMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalProvider && m.modal.kind != modalMethod {
		return m, nil // the picker was closed while the link was fetched
	}
	if msg.err != nil {
		m.modal = modal{}
		cmd := m.say("Could not start sign-in: "+msg.err.Error(), true)
		return m, cmd
	}
	m.modal = modal{kind: modalLink, login: msg.login, input: m.loginInput(msg.login.Method.Kind)}
	if msg.login.Method.Kind == v1.LoginDevice {
		return m, pollAfter(msg.login)
	}
	return m, nil
}

// loginInput is where the person pastes what finishes a sign-in of kind. A
// paste may be a whole login file, and is a secret, so it is not shown.
func (m Model) loginInput(kind string) textinput.Model {
	placeholder, limit := "paste the code, or the address the browser ends on", 4096
	if kind == v1.LoginPaste {
		placeholder, limit = "paste it here", pasteLimit
	}
	ti := dialog.TextInput(m.theme, "▸ ", placeholder, limit)
	if kind == v1.LoginPaste {
		ti.EchoMode = textinput.EchoPassword
	}
	ti.Focus()
	ti.SetWidth(min(70, max(24, m.width-16)))
	return ti
}

// pollAfter asks after device sign-in l once its interval has passed.
func pollAfter(l v1.Login) tea.Cmd {
	every := time.Duration(max(l.PollSeconds, 2)) * time.Second
	return tea.Tick(every, func(time.Time) tea.Msg { return loginPollMsg{id: l.ID} })
}

// pollLogin asks whether the device sign-in on screen was approved.
func (m Model) pollLogin(msg loginPollMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalLink || m.modal.login.ID != msg.id || m.modal.busy || m.modal.err != "" {
		return m, nil // cancelled, finished, or failed
	}
	m.modal.busy = true
	return m, m.completeLogin("")
}

// completeLogin finishes the sign-in on screen with input.
func (m Model) completeLogin(input string) tea.Cmd {
	l, svc := m.modal.login, m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		p, err := svc.CompleteLogin(ctx, l, input)
		return loginDoneMsg{label: l.Provider, id: l.ID, done: p.Done, err: err}
	}
}

func (m Model) keyLink(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.modal.busy {
		return m, nil
	}
	switch msg.String() {
	case keyEsc, keyCtrlC:
		l, svc := m.modal.login, m.svc
		m.modal = modal{}
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := svc.CancelLogin(ctx, l); err != nil {
				log.Debug("accounts: cancel sign-in", "err", err) // the session expires by itself
			}
			return doneMsg{}
		}
	case "ctrl+o":
		if m.modal.login.URL == "" {
			return m, nil
		}
		return m, openBrowser(m.modal.login.URL)
	case "ctrl+y":
		return m.copyLogin()
	case keyEnter:
		return m.submitLogin()
	}
	if m.modal.login.Method.Kind == v1.LoginDevice {
		return m, nil // nothing to type: the code is entered on the provider's page
	}
	var cmd tea.Cmd
	m.modal.input, cmd = m.modal.input.Update(msg)
	return m, cmd
}

// copyLogin copies the device code, or else the sign-in link.
func (m Model) copyLogin() (Model, tea.Cmd) {
	l := m.modal.login
	switch {
	case l.Method.Kind == v1.LoginDevice && l.UserCode != "":
		return m, tea.Batch(tea.SetClipboard(l.UserCode), m.say("Code copied", false))
	case l.URL != "":
		return m, tea.Batch(tea.SetClipboard(l.URL), m.say("Sign-in link copied", false))
	}
	return m, nil
}

// submitLogin sends what was pasted. On a device sign-in that failed, it
// starts asking again.
func (m Model) submitLogin() (Model, tea.Cmd) {
	if m.modal.login.Method.Kind == v1.LoginDevice {
		if m.modal.err == "" {
			return m, nil
		}
		m.modal.err, m.modal.busy = "", true
		return m, m.completeLogin("")
	}
	input := strings.TrimSpace(m.modal.input.Value())
	if input == "" {
		return m, nil
	}
	m.modal.busy, m.modal.err = true, ""
	return m, m.completeLogin(input)
}

func (m Model) loginDone(msg loginDoneMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalLink || (msg.id != "" && msg.id != m.modal.login.ID) {
		return m, nil
	}
	if msg.err != nil {
		m.modal.busy = false
		m.modal.err = msg.err.Error()
		return m, nil
	}
	if !msg.done {
		m.modal.busy = false
		return m, pollAfter(m.modal.login)
	}
	m.modal = modal{}
	p := providerByID(m.snap, msg.label)
	return m.start("Publishing the new "+p.Name+" account…",
		m.change("Signed in to "+p.Name+". Press u on it to use it.", func(context.Context) error { return nil }))
}

func (m Model) keyConfirm(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		c := m.modal
		m.modal = modal{}
		return m.start(c.progress, c.run)
	case "n", "N", keyEsc, "q", keyCtrlC:
		m.modal = modal{}
	}
	return m, nil
}

// useSelected makes the selected account first in its agent's chain, which
// sends that agent's traffic to it.
func (m Model) useSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a.Disabled {
		cmd := m.say("That account is disabled; press d to enable it first.", true)
		return m, cmd
	}
	before := enabledChain(m.snap, a.Provider)
	after := []v1.Account{a}
	for i := range before {
		if before[i].Name != a.Name {
			after = append(after, before[i])
		}
	}
	r := reorder{provider: a.Provider, before: before, after: after}
	if !m.movesTraffic(r) {
		text := label(a) + " is already active."
		if from, to := m.carriers(r); from.Name != a.Name {
			text = label(a) + " is first but cooling down; " + label(to) + " carries the traffic."
		}
		cmd := m.say(text, false)
		return m, cmd
	}
	p, svc := providerByID(m.snap, a.Provider), m.svc
	run := m.change(label(a)+" is first for "+p.Name+". The proxy switches within about 2 minutes.", func(ctx context.Context) error {
		return svc.SetActive(ctx, a.Provider, a.Name)
	})
	return m.confirm("Use "+label(a)+" for "+p.Name+"?", m.warning(r), "switch", "Switching "+p.Name+" to "+label(a)+"…", run), nil
}

// moveInChain moves the selected account up or down the fallback order. It
// asks first when that would move traffic.
func (m Model) moveInChain(delta int) (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a.Disabled {
		cmd := m.say("That account is disabled; press d to enable it first.", true)
		return m, cmd
	}
	before := enabledChain(m.snap, a.Provider)
	idx := slices.IndexFunc(before, func(b v1.Account) bool { return b.Name == a.Name })
	next := idx + delta
	if idx < 0 || next < 0 || next >= len(before) {
		return m, nil
	}
	after := slices.Clone(before)
	after[idx], after[next] = after[next], after[idx]
	r := reorder{provider: a.Provider, before: before, after: after}
	p, svc, order := providerByID(m.snap, a.Provider), m.svc, names(after)
	run := m.change(p.Name+" fallback order updated", func(ctx context.Context) error {
		return svc.SetChain(ctx, a.Provider, order)
	})
	progress := "Updating " + p.Name + " fallback order…"
	if !m.movesTraffic(r) {
		return m.start(progress, run)
	}
	return m.confirm("Put "+label(after[0])+" first for "+p.Name+"?", m.warning(r), "switch", progress, run), nil
}

func names(accts []v1.Account) []string {
	out := make([]string, len(accts))
	for i := range accts {
		out[i] = accts[i].Name
	}
	return out
}

// without is the agent's chain with a taken out.
func (m Model) without(a v1.Account) reorder {
	before := enabledChain(m.snap, a.Provider)
	after := slices.DeleteFunc(slices.Clone(before), func(b v1.Account) bool { return b.Name == a.Name })
	return reorder{provider: a.Provider, before: before, after: after}
}

// leaving is what taking a out of the chain does to running sandboxes, or nil
// when it carries no traffic.
func (m Model) leaving(a v1.Account) []string {
	r := m.without(a)
	if len(r.before) == 0 {
		return nil
	}
	if len(r.after) == 0 {
		return []string{"It is the only enabled account for this agent."}
	}
	if !m.movesTraffic(r) {
		return nil
	}
	return m.warning(r)
}

// toggleSelected disables or enables the selected account and publishes that.
func (m Model) toggleSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	svc := m.svc
	if a.Disabled {
		return m.start("Enabling "+label(a)+"…", m.change("Enabled "+label(a), func(ctx context.Context) error {
			return svc.SetDisabled(ctx, a.Name, false)
		}))
	}
	rest := names(m.without(a).after)
	run := m.change("Disabled "+label(a), func(ctx context.Context) error {
		if err := svc.SetDisabled(ctx, a.Name, true); err != nil {
			return fmt.Errorf("disable: %w", err)
		}
		if len(rest) == 0 {
			return nil
		}
		// Take it out of the saved order too, so that enabling it later adds
		// it at the end and moves no traffic unasked.
		return svc.SetChain(ctx, a.Provider, rest)
	})
	progress := "Disabling " + label(a) + "…"
	if body := m.leaving(a); body != nil {
		return m.confirm("Disable "+label(a)+"?", body, "disable", progress, run), nil
	}
	return m.start(progress, run)
}

func (m Model) askRemove() Model {
	a, ok := m.selected()
	if !ok {
		return m
	}
	body := []string{"This deletes its saved tokens."}
	if more := m.leaving(a); !a.Disabled && more != nil {
		body = append(append(body, ""), more...)
	}
	svc := m.svc
	run := m.change("Removed "+label(a), func(ctx context.Context) error {
		return svc.RemoveAccount(ctx, a.Name)
	})
	return m.confirm("Remove "+label(a)+"?", body, "remove", "Removing "+label(a)+"…", run)
}

// renewSelected renews the selected account's token now.
func (m Model) renewSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	svc := m.svc
	return m, act(fetchWindow, "Renewed the token of "+label(a), func(ctx context.Context) error {
		return svc.RefreshAccount(ctx, a.Name)
	})
}

// reload reads the accounts again and has the proxy fetch live quotas.
func (m Model) reload() (Model, tea.Cmd) {
	m.loading = true
	svc := m.svc
	quotas := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchWindow)
		defer cancel()
		if err := svc.RefreshQuotas(ctx); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "Live quotas updated", refetch: true}
	}
	return m, tea.Batch(m.fetch(), quotas)
}

// act runs one change to the accounts, then says ok and reads them again, or
// says what went wrong.
func act(timeout time.Duration, ok string, change func(ctx context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := change(ctx); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: ok, refetch: true}
	}
}

var errBrowser = errors.New("could not open a browser here; copy the link with ctrl+y instead")

// openBrowser asks the desktop to open url. It is best effort: on a machine
// with no browser the link is still on screen to copy.
func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// The link goes straight to the opener's argv, never through a shell.
		cmd := exec.CommandContext(ctx, "xdg-open")
		if runtime.GOOS == "darwin" {
			cmd = exec.CommandContext(ctx, "open")
		}
		cmd.Args = append(cmd.Args, url)
		if err := cmd.Run(); err != nil {
			return doneMsg{err: errBrowser}
		}
		return doneMsg{}
	}
}
