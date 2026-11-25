// Package app is the root Bubble Tea model. It draws a view switcher across
// the top and gives the rest of the frame to the active tab: the sandbox
// board or gh-dash.
package app

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/services/accounts"
	"github.com/tmih06/nightshift/internal/tui/accountsview"
	"github.com/tmih06/nightshift/internal/tui/dashboard"
	"github.com/tmih06/nightshift/internal/tui/repoview"
	zgh "github.com/tmih06/nightshift/internal/zenocto/gh"
	zapp "github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

// Tab names one page behind the tab bar.
type Tab int

const (
	TabSandboxes Tab = iota
	TabGitHub
	TabAccounts
)

type ghScreen int

const (
	ghScreenDash ghScreen = iota
	ghScreenRepos
	ghScreenPR
)

var tabLabels = [...]string{
	TabSandboxes: "Pods",
	TabGitHub:    "GitHub",
	TabAccounts:  "Accounts",
}

// GitHub is the model behind the GitHub tab: gh-dash's root.
type GitHub interface {
	Init() tea.Cmd
	Update(tea.Msg) (tea.Model, tea.Cmd)
	View() tea.View
	Capturing() bool
	SwitchView(config.ViewType) tea.Cmd
}

// Model routes input to the active tab and every other message to both, so a
// tab keeps refreshing while it is out of sight.
type Model struct {
	theme  ghtheme.Theme
	styles context.Styles
	active Tab

	sandboxes dashboard.Model

	// accounts is the Accounts tab; accountsSvc reaches the cluster for it and
	// is closed on exit.
	accounts    accountsview.Model
	accountsSvc accounts.Service

	// gh is nil when gh-dash could not start; ghErr then says why.
	gh    GitHub
	ghErr error

	// ghScreen tracks which view fills the GitHub tab; prReturn remembers
	// where closing zen-octo returns (gh-dash or the Repo view).
	ghScreen ghScreen
	prReturn ghScreen

	// pr is zen-octo's pull request view, made on the first enter on a pull
	// request and kept.
	pr          *zapp.Model
	prClient    PullRequests
	newPRClient func() (PullRequests, error)

	// repos is Nightshift's 3-pane repository workspace, made on first open.
	repos       *repoview.Model
	currentRepo string
	newRepos    func() *repoview.Model
	startRepos  bool

	// notice is a one-line message in the bar: opening, or why it failed.
	notice    string
	noticeBad bool
	noticeAt  time.Time

	// picker, while open, asks which pool a pull request's sandbox comes
	// from; it holds the keyboard.
	picker *picker

	// start is the pull request -pr asked for, opened once the screen is up.
	start *ghtui.OpenPullRequestMsg

	width  int
	height int
}

// New builds the root model over gh-dash's theme. gh is nil when ghErr says
// why the GitHub tab cannot run.
func New(theme ghtheme.Theme, gh GitHub, ghErr error) Model {
	return NewWithAccounts(theme, gh, ghErr, accounts.NewLive())
}

// NewWithAccounts is New with the Accounts tab backed by svc.
func NewWithAccounts(theme ghtheme.Theme, gh GitHub, ghErr error, svc accounts.Service) Model {
	return Model{
		theme:       theme,
		styles:      context.InitStyles(theme),
		sandboxes:   dashboard.New(theme),
		accounts:    accountsview.New(theme, svc),
		accountsSvc: svc,
		gh:          gh,
		ghErr:       ghErr,
		newPRClient: func() (PullRequests, error) {
			c, err := zgh.New()
			if err != nil {
				return nil, err
			}
			return c, nil
		},
	}
}

// Close releases what the tabs hold open, such as the Accounts tab's
// port-forwards. Call it after the program ends.
func (m Model) Close() {
	if m.accountsSvc != nil {
		m.accountsSvc.Close()
	}
}

// NewGhDash wraps gh-dash's root model for the GitHub tab.
func NewGhDash(m *ghtui.Model) GitHub { return m }

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.sandboxes.Init()}
	if m.gh != nil {
		cmds = append(cmds, m.gh.Init())
	}
	if m.start != nil {
		open := *m.start
		cmds = append(cmds, func() tea.Msg { return open })
	} else if m.startRepos {
		cmds = append(cmds, func() tea.Msg { return ghtui.OpenRepoViewMsg{} })
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		body := tea.WindowSizeMsg{Width: msg.Width, Height: max(0, msg.Height-barHeight)}
		m.accounts, _ = m.accounts.Update(body)
		return m.broadcast(body)

	case tea.KeyPressMsg:
		if m.picker != nil {
			return m.updatePicker(msg)
		}
		if !m.capturing() {
			switch msg.String() {
			case "tab":
				return m.switchTo((m.active + 1) % Tab(len(tabLabels)))
			case "shift+tab":
				return m.switchTo((m.active + Tab(len(tabLabels)) - 1) % Tab(len(tabLabels)))
			}
		}
		return m.toActive(msg)

	case tea.PasteMsg:
		if m.picker != nil {
			return m.updatePickerPaste(msg)
		}
		return m.toActive(msg)

	case ghtui.OpenPullRequestMsg:
		m.prReturn = ghScreenDash
		return m.openPR(msg)

	case ghtui.OpenRepoViewMsg:
		return m.openRepos()

	case repoview.SwitchViewMsg:
		m.ghScreen = ghScreenDash
		if m.gh != nil {
			return m, m.gh.SwitchView(msg.View)
		}
		return m, nil

	case repoview.OpenPRMsg:
		m.prReturn = ghScreenRepos
		return m.openPR(ghtui.OpenPullRequestMsg{
			Repo:   msg.Repo,
			Number: msg.Number,
			URL:    msg.URL,
		})

	case repoview.SandboxRepoMsg:
		return m.openRepoPicker(msg.Repo, msg.Branch, msg.URL)

	case repoview.SandboxPRMsg:
		return m.openPicker(ghtui.SandboxPullRequestMsg{
			Repo:   msg.Repo,
			Number: msg.Number,
			URL:    msg.URL,
			Branch: msg.Branch,
		})

	case repoview.SandboxIssueMsg:
		return m.openIssuePicker(ghtui.SandboxIssueMsg{
			Repo:   msg.Repo,
			Number: msg.Number,
			URL:    msg.URL,
			Title:  msg.Title,
		})

	case repoview.FocusSandboxMsg:
		next, switchCmd := m.goTo(TabSandboxes)
		m = next
		var cmd tea.Cmd
		m.sandboxes, cmd = m.sandboxes.FocusPod(msg.Pod)
		return m, tea.Batch(switchCmd, cmd)

	case ghtui.SandboxPullRequestMsg:
		return m.openPicker(msg)

	case ghtui.SandboxIssueMsg:
		return m.openIssuePicker(msg)

	case prFoundMsg:
		return m.prFound(msg)

	case zapp.ClosedMsg:
		m.ghScreen = m.prReturn
		return m, nil

	case noticeExpiredMsg:
		if msg.at.Equal(m.noticeAt) {
			m.notice = ""
		}
		return m, nil

	case tea.MouseMsg:
		if m.picker != nil {
			return m.pickerMouse(msg)
		}
		mouse := msg.Mouse()
		if mouse.Y < barHeight {
			if _, rel := msg.(tea.MouseReleaseMsg); rel {
				return m.toActive(shiftMouse(msg, -barHeight))
			}
			if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
				if tab, hit := m.tabAt(mouse.X); hit {
					return m.switchTo(tab)
				}
			}
			return m, nil
		}
		return m.toActive(shiftMouse(msg, -barHeight))
	}
	return m.broadcast(msg)
}

// capturing is whether the active tab holds the keyboard for text, a prompt,
// or the embedded shell, so tab belongs to it rather than to the tab bar.
func (m Model) capturing() bool {
	if m.picker != nil {
		return true
	}
	switch m.active {
	case TabSandboxes:
		return m.sandboxes.Capturing()
	case TabAccounts:
		return m.accounts.Capturing()
	case TabGitHub:
		switch m.ghScreen {
		case ghScreenPR:
			return m.pr != nil && m.pr.Capturing()
		case ghScreenRepos:
			return m.repos != nil && m.repos.Capturing()
		default:
			return m.gh != nil && m.gh.Capturing()
		}
	}
	return false
}

// goTo makes tab the active one. The Accounts tab polls only while it is shown,
// so leaving and entering it start and stop that.
func (m Model) goTo(tab Tab) (Model, tea.Cmd) {
	m.active = tab
	m.sandboxes = m.sandboxes.SetHidden(tab != TabSandboxes)
	var cmd tea.Cmd
	m.accounts, cmd = m.accounts.SetHidden(tab != TabAccounts)
	return m, cmd
}

func (m Model) switchTo(tab Tab) (tea.Model, tea.Cmd) {
	next, cmd := m.goTo(tab)
	return next, cmd
}

func (m Model) toActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.active {
	case TabSandboxes:
		m.sandboxes, cmd = m.sandboxes.Update(msg)
	case TabAccounts:
		m.accounts, cmd = m.accounts.Update(msg)
	case TabGitHub:
		switch m.ghScreen {
		case ghScreenPR:
			return m, m.updatePR(msg)
		case ghScreenRepos:
			return m, m.updateRepos(msg)
		default:
			if m.gh == nil {
				if key, ok := msg.(tea.KeyPressMsg); ok && (key.String() == "q" || key.String() == "ctrl+c") {
					return m, tea.Quit
				}
				return m, nil
			}
			cmd = m.updateGitHub(msg)
		}
	}
	return m, cmd
}

func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	if accountsview.Owns(msg) {
		var cmd tea.Cmd
		m.accounts, cmd = m.accounts.Update(msg)
		return m, cmd
	}
	var sandboxCmd, ghCmd tea.Cmd
	m.sandboxes, sandboxCmd = m.sandboxes.Update(msg)
	if m.repos != nil {
		m.syncRepoSandboxes()
	}
	if dashboard.Owns(msg) {
		return m, sandboxCmd
	}
	if prOwns(msg) {
		return m, tea.Batch(sandboxCmd, m.updatePR(msg))
	}
	if repoview.Owns(msg) {
		return m, tea.Batch(sandboxCmd, m.updateRepos(msg))
	}
	if m.gh != nil {
		ghCmd = m.updateGitHub(msg)
	}
	return m, tea.Batch(sandboxCmd, ghCmd, m.updatePR(msg), m.updateRepos(msg))
}

// updateGitHub keeps gh-dash's pointer model; it updates in place, as it does
// under its own program.
func (m *Model) updateGitHub(msg tea.Msg) tea.Cmd {
	next, cmd := m.gh.Update(msg)
	if gh, ok := next.(GitHub); ok {
		m.gh = gh
	}
	return cmd
}

func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "nightshift"
	if m.width <= 0 || m.height <= 0 {
		return v
	}
	body := m.body()
	if m.picker != nil {
		body.Content, body.Cursor = m.overPicker(body.Content), nil
	}
	v.Content = m.renderBar() + "\n" + body.Content
	if body.Cursor != nil {
		moved := *body.Cursor
		moved.Y += barHeight
		v.Cursor = &moved
	}
	return v
}

func (m Model) render() string { return m.View().Content }

func (m Model) body() tea.View {
	h := max(0, m.height-barHeight)
	if m.active == TabSandboxes {
		return tea.NewView(m.sandboxes.View())
	}
	if m.active == TabAccounts {
		return tea.NewView(m.accounts.View())
	}
	switch m.ghScreen {
	case ghScreenPR:
		if m.pr != nil {
			return m.pr.View()
		}
	case ghScreenRepos:
		if m.repos != nil {
			return m.repos.View()
		}
	}
	if m.gh != nil {
		return m.gh.View()
	}
	return tea.NewView(unavailable(m.theme, m.ghErr, m.width, h))
}

// unavailable fills the GitHub tab when gh-dash cannot run.
func unavailable(th ghtheme.Theme, err error, width, height int) string {
	text := "gh-dash could not start."
	if err != nil {
		text = fmt.Sprintf("gh-dash could not start: %v", err)
	}
	body := lipgloss.NewStyle().Foreground(th.PrimaryText).Bold(true).Render(text) + "\n\n" +
		lipgloss.NewStyle().Foreground(th.FaintText).
			Render("Authenticate with gh auth login, then restart nightshift.")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().MaxWidth(max(1, width-4)).Render(body))
}

// Once renders one frame of the sandbox tab under the tab bar, for -once.
func Once(width, height int) string {
	th := *ghtheme.DefaultTheme
	m := Model{theme: th, styles: context.InitStyles(th), sandboxes: dashboard.Fetched(th)}
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(Model).render()
}

func shiftMouse(msg tea.MouseMsg, dy int) tea.MouseMsg {
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		msg.Y += dy
		return msg
	case tea.MouseReleaseMsg:
		msg.Y += dy
		return msg
	case tea.MouseWheelMsg:
		msg.Y += dy
		return msg
	case tea.MouseMotionMsg:
		msg.Y += dy
		return msg
	}
	return msg
}
