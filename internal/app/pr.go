package app

import (
	"context"
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
	ghctx "github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	zgh "github.com/tmih06/nightshift/internal/zenocto/gh"
	zapp "github.com/tmih06/nightshift/internal/zenocto/tui/app"
	"github.com/tmih06/nightshift/internal/zenocto/tui/syntax"
	ztheme "github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

// PullRequests is what the pull request view needs from GitHub: zen-octo's
// client, plus finding a pull request by owner/name#number.
type PullRequests interface {
	zapp.GitHub
	FindPullRequest(ctx context.Context, ref zgh.PRRef) (zgh.PullRequest, error)
}

const (
	findTimeout = 30 * time.Second
	noticeFor   = 5 * time.Second
)

type prFoundMsg struct {
	pr  zgh.PullRequest
	err error
	ref zgh.PRRef
}

type noticeExpiredMsg struct{ at time.Time }

// StartOn opens nightshift on the GitHub tab with ref's pull request.
func (m Model) StartOn(ref zgh.PRRef) Model {
	m.active = TabGitHub
	m.sandboxes = m.sandboxes.SetHidden(true)
	m.start = &ghtui.OpenPullRequestMsg{Repo: ref.Repo(), Number: ref.Number,
		URL: fmt.Sprintf("https://github.com/%s/pull/%d", ref.Repo(), ref.Number)}
	return m
}

// openPR starts opening the pull request gh-dash asked for. The client is
// made on first use, so nightshift starts without touching zen-octo's code.
func (m Model) openPR(msg ghtui.OpenPullRequestMsg) (Model, tea.Cmd) {
	owner, name, ok := strings.Cut(msg.Repo, "/")
	if !ok || msg.Number <= 0 {
		return m.noticeErr(fmt.Sprintf("cannot open %q", msg.URL))
	}
	ref := zgh.PRRef{Owner: owner, Name: name, Number: msg.Number}
	if m.prClient == nil {
		if m.newPRClient == nil {
			return m.noticeErr("pull requests are not available")
		}
		c, err := m.newPRClient()
		if err != nil {
			return m.noticeErr(err.Error())
		}
		m.prClient = c
	}
	m.notice, m.noticeBad = fmt.Sprintf("opening %s#%d…", msg.Repo, msg.Number), false
	m.noticeAt = time.Time{}
	client := m.prClient
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
		defer cancel()
		pr, err := client.FindPullRequest(ctx, ref)
		return prFoundMsg{pr: pr, err: err, ref: ref}
	}
}

// prFound opens the view on one host kept for the session, so its cache and
// its single refresh timer carry over from one pull request to the next.
func (m Model) prFound(msg prFoundMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.noticeErr(fmt.Sprintf("could not open %s/%s#%d: %v", msg.ref.Owner, msg.ref.Name, msg.ref.Number, msg.err))
	}
	m.notice = ""
	var cmds []tea.Cmd
	if m.pr == nil {
		syn, _ := syntax.New(ztheme.SyntaxDark)
		host := zapp.New(m.prClient, zenTheme(m.theme, m.styles), syn).StartOn(msg.pr)
		m.pr = &host
		cmds = append(cmds, host.Init())
	} else {
		host := m.pr.StartOn(msg.pr)
		m.pr = &host
		cmds = append(cmds, host.Opening())
	}
	m.prOpen = true
	cmds = append(cmds, m.updatePR(tea.WindowSizeMsg{Width: m.width, Height: max(0, m.height-barHeight)}))
	return m, tea.Batch(cmds...)
}

// prWants is whether msg can matter to the pull request view: its own
// messages, the bubbles it runs (spinners, text cursors), and the size and
// focus of the screen. zen-octo's host is a ~95 KB value copied on every
// Update, so the rest (gh-dash's traffic) is kept from it.
func prWants(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.WindowSizeMsg, tea.FocusMsg, tea.BlurMsg,
		tea.KeyPressMsg, tea.KeyReleaseMsg, tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg,
		tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg, tea.MouseMotionMsg:
		return true
	}
	t := reflect.TypeOf(msg)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	path := t.PkgPath()
	return strings.HasPrefix(path, "github.com/tmih06/nightshift/internal/zenocto") ||
		strings.HasPrefix(path, "charm.land/bubbles/v2")
}

func (m *Model) updatePR(msg tea.Msg) tea.Cmd {
	if m.pr == nil || !prWants(msg) {
		return nil
	}
	next, cmd := m.pr.Update(msg)
	if host, ok := next.(zapp.Model); ok {
		m.pr = &host
	}
	return cmd
}

func (m Model) noticeErr(text string) (Model, tea.Cmd) {
	at := time.Now()
	m.notice, m.noticeBad, m.noticeAt = text, true, at
	return m, tea.Tick(noticeFor, func(time.Time) tea.Msg { return noticeExpiredMsg{at: at} })
}

// zenTheme paints zen-octo's view from gh-dash's theme, so the pull request
// view reads as one more gh-dash view rather than another program.
func zenTheme(th ghtheme.Theme, s ghctx.Styles) ztheme.Theme {
	return ztheme.Theme{
		Syntax:             ztheme.SyntaxDark,
		Text:               th.PrimaryText,
		Accent:             s.Colors.OpenPR,
		Subtle:             th.SecondaryText,
		Muted:              th.FaintText,
		Inverted:           th.InvertedText,
		Success:            th.SuccessText,
		Warning:            th.WarningText,
		Error:              th.ErrorText,
		Actor:              th.ActorText,
		SelectedBackground: th.SelectedBackground,
		AddedBackground:    adaptive("#e6ffec", "#12261e"),
		RemovedBackground:  adaptive("#ffebe9", "#2d1214"),
		Border:             th.SecondaryBorder,
		BorderSubtle:       th.PrimaryBorder,
		BorderMuted:        th.FaintBorder,
	}
}

func adaptive(light, dark string) color.Color {
	return compat.AdaptiveColor{Light: lipgloss.Color(light), Dark: lipgloss.Color(dark)}
}
