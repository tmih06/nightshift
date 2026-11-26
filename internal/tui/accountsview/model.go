// Package accountsview is the Accounts tab: the coding-agent accounts signed
// in to Nightshift, which one each agent's sandboxes use, and how much each has
// been used. It draws what internal/accounts fetches and sends its actions
// back; it reaches the cluster only through API.
package accountsview

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
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
	modalLink
	modalRemove
)

type modal struct {
	kind modalKind

	// provider picker
	choice int

	// sign-in link
	login v1.Login
	input textinput.Model
	busy  bool
	err   string

	// removal
	name  string
	label string
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
		err   error
	}
)

// Owns reports whether the tab handles msg itself.
func Owns(msg tea.Msg) bool {
	switch msg.(type) {
	case snapshotMsg, tickMsg, noteMsg, doneMsg, loginStartedMsg, loginDoneMsg:
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
	if m.modal.kind != modalLink || m.modal.busy {
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
	case modalLink:
		return m.keyLink(msg)
	case modalRemove:
		return m.keyRemove(msg)
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
	case "R", "ctrl+r":
		m.loading = true
		return m, m.fetch()
	case "a":
		return m.openProviderPicker(), nil
	case "Q":
		return m.refreshQuotas()
	case "p":
		return m.publishNow("Publishing…")
	default:
		return m.accountKey(k)
	}
	return m, nil
}

// accountKey acts on the selected account.
func (m Model) accountKey(k string) (Model, tea.Cmd) {
	switch k {
	case keyEnter, "s":
		return m.useSelected()
	case "K", "[":
		return m.moveInChain(-1)
	case "J", "]":
		return m.moveInChain(1)
	case "d":
		return m.toggleSelected()
	case "x":
		if a, ok := m.selected(); ok {
			m.modal = modal{kind: modalRemove, name: a.Name, label: label(a)}
		}
	case "r":
		return m.refreshSelected()
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
		m.modal.busy = true
		svc := m.svc
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), fetchWindow)
			defer cancel()
			l, err := svc.StartLogin(ctx, p.ID)
			return loginStartedMsg{login: l, err: err}
		}
	}
	return m, nil
}

func (m Model) loginStarted(msg loginStartedMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalProvider {
		return m, nil // the picker was closed while the link was fetched
	}
	if msg.err != nil {
		m.modal = modal{}
		cmd := m.say("Could not start sign-in: "+msg.err.Error(), true)
		return m, cmd
	}
	ti := dialog.TextInput(m.theme, "▸ ", "paste the address the browser ends on", 4096)
	ti.Focus()
	ti.SetWidth(min(70, max(24, m.width-16)))
	m.modal = modal{kind: modalLink, login: msg.login, input: ti}
	return m, nil
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
		return m, openBrowser(m.modal.login.URL)
	case "ctrl+y":
		return m, tea.Batch(tea.SetClipboard(m.modal.login.URL), m.say("Sign-in link copied", false))
	case keyEnter:
		redirect := strings.TrimSpace(m.modal.input.Value())
		if redirect == "" {
			return m, nil
		}
		m.modal.busy, m.modal.err = true, ""
		l, svc := m.modal.login, m.svc
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			err := svc.CompleteLogin(ctx, l, redirect)
			return loginDoneMsg{label: l.Provider, err: err}
		}
	}
	var cmd tea.Cmd
	m.modal.input, cmd = m.modal.input.Update(msg)
	return m, cmd
}

func (m Model) loginDone(msg loginDoneMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalLink {
		return m, nil
	}
	if msg.err != nil {
		m.modal.busy = false
		m.modal.err = msg.err.Error()
		return m, nil
	}
	m.modal = modal{}
	p := providerByID(m.snap, msg.label)
	m.loading = true
	return m, tea.Batch(m.fetch(), m.say("Signed in to "+p.Name+". Press enter on it to use it for sandboxes.", false))
}

func (m Model) keyRemove(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name, label, svc := m.modal.name, m.modal.label, m.svc
		m.modal = modal{}
		return m, act(fetchWindow, "Removed "+label, func(ctx context.Context) error {
			return svc.RemoveAccount(ctx, name)
		})
	case "n", "N", keyEsc, "q", keyCtrlC:
		m.modal = modal{}
	}
	return m, nil
}

func (m Model) useSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a.Disabled {
		cmd := m.say("That account is disabled; press d to enable it first.", true)
		return m, cmd
	}
	p := providerByID(m.snap, a.Provider)
	svc := m.svc
	m.notice, m.noticeBad, m.noticeAt = "Switching "+p.Name+" to "+label(a)+"…", false, m.now()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.SetActive(ctx, a.Provider, a.Name); err != nil {
			return doneMsg{err: err}
		}
		if _, err := svc.Publish(ctx); err != nil {
			return doneMsg{err: err, refetch: true}
		}
		return doneMsg{text: p.Name + " primary is now " + label(a) + " (fallback chain updated)", refetch: true}
	}
}

func (m Model) moveInChain(delta int) (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	if a.Disabled {
		cmd := m.say("That account is disabled; press d to enable it first.", true)
		return m, cmd
	}
	accts := byProvider(m.snap, a.Provider)
	enabled := make([]string, 0, len(accts))
	idx := -1
	for i := range accts {
		if accts[i].Disabled {
			continue
		}
		if accts[i].Name == a.Name {
			idx = len(enabled)
		}
		enabled = append(enabled, accts[i].Name)
	}
	next := idx + delta
	if idx < 0 || next < 0 || next >= len(enabled) {
		return m, nil
	}
	enabled[idx], enabled[next] = enabled[next], enabled[idx]
	p := providerByID(m.snap, a.Provider)
	svc := m.svc
	m.notice, m.noticeBad, m.noticeAt = "Updating "+p.Name+" fallback order…", false, m.now()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := svc.SetChain(ctx, a.Provider, enabled); err != nil {
			return doneMsg{err: err}
		}
		if _, err := svc.Publish(ctx); err != nil {
			return doneMsg{err: err, refetch: true}
		}
		return doneMsg{text: p.Name + " fallback order updated", refetch: true}
	}
}

func (m Model) toggleSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	svc := m.svc
	disable := !a.Disabled
	verb := "Disabled "
	if !disable {
		verb = "Enabled "
	}
	return m, act(fetchWindow, verb+label(a)+". Press p to publish the change.", func(ctx context.Context) error {
		return svc.SetDisabled(ctx, a.Name, disable)
	})
}

func (m Model) refreshSelected() (Model, tea.Cmd) {
	a, ok := m.selected()
	if !ok {
		return m, nil
	}
	svc := m.svc
	return m, act(fetchWindow, "Refreshed "+label(a), func(ctx context.Context) error {
		return svc.RefreshAccount(ctx, a.Name)
	})
}

func (m Model) refreshQuotas() (Model, tea.Cmd) {
	svc := m.svc
	m.notice, m.noticeBad, m.noticeAt = "Fetching live quotas…", false, m.now()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchWindow)
		defer cancel()
		if err := svc.RefreshQuotas(ctx); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "Live quotas updated", refetch: true}
	}
}

func (m Model) publishNow(label string) (Model, tea.Cmd) {
	svc := m.svc
	m.notice, m.noticeBad, m.noticeAt = label, false, m.now()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := svc.Publish(ctx); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "Published. New sandboxes pick it up; running ones keep their login.", refetch: true}
	}
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
