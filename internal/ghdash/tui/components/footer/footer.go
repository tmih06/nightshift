package footer

import (
	"fmt"
	"path"
	"strings"

	bbHelp "charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/git"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/keys"
	"github.com/tmih06/nightshift/internal/ghdash/utils"
)

const viewSeparator = " │ "

type viewCache struct {
	valid           bool
	view            config.ViewType
	leftSection     string
	rightSection    string
	showAll         bool
	showConfirmQuit bool
	screenWidth     int
	repoPath        string
	repoUrl         string
	user            string
	rendered        string

	switcherValid    bool
	switcherView     config.ViewType
	switcherRepoPath string
	switcherRepoUrl  string
	switcherUser     string
	switcher         string
	switcherWidth    int
	helpIndicator    string
	helpWidth        int
}

type Model struct {
	ctx             *context.ProgramContext
	leftSection     *string
	rightSection    *string
	help            bbHelp.Model
	ShowAll         bool
	ShowConfirmQuit bool
	cache           *viewCache
}

func NewModel(ctx *context.ProgramContext) Model {
	help := bbHelp.New()
	help.ShowAll = true
	help.Styles = ctx.Styles.Help.BubbleStyles
	l := ""
	r := ""
	return Model{
		ctx:          ctx,
		help:         help,
		leftSection:  &l,
		rightSection: &r,
		cache:        &viewCache{},
	}
}

func (m Model) View() string {
	leftSection := ""
	if m.leftSection != nil {
		leftSection = *m.leftSection
	}
	rightSection := ""
	if m.rightSection != nil {
		rightSection = *m.rightSection
	}
	if m.cache != nil && m.cache.valid &&
		m.cache.view == m.ctx.View &&
		m.cache.leftSection == leftSection &&
		m.cache.rightSection == rightSection &&
		m.cache.showAll == m.ShowAll &&
		m.cache.showConfirmQuit == m.ShowConfirmQuit &&
		m.cache.screenWidth == m.ctx.ScreenWidth &&
		m.cache.repoPath == m.ctx.RepoPath &&
		m.cache.repoUrl == m.ctx.RepoUrl &&
		m.cache.user == m.ctx.User {
		return m.cache.rendered
	}

	var footer string

	if m.ShowConfirmQuit {
		footer = lipgloss.NewStyle().
			Render("Really quit? (Press y/enter to confirm, any other key to cancel)")
	} else {
		var viewSwitcher, helpIndicator string
		var switcherW, helpW int
		if m.cache != nil && m.cache.switcherValid &&
			m.cache.switcherView == m.ctx.View &&
			m.cache.switcherRepoPath == m.ctx.RepoPath &&
			m.cache.switcherRepoUrl == m.ctx.RepoUrl &&
			m.cache.switcherUser == m.ctx.User {
			viewSwitcher = m.cache.switcher
			switcherW = m.cache.switcherWidth
			helpIndicator = m.cache.helpIndicator
			helpW = m.cache.helpWidth
		} else {
			helpIndicator = lipgloss.NewStyle().
				Background(m.ctx.Theme.FaintText).
				Foreground(m.ctx.Theme.SelectedBackground).
				Padding(0, 1).
				Render("? help")
			helpW = lipgloss.Width(helpIndicator)
			viewSwitcher = m.renderViewSwitcher(m.ctx)
			switcherW = lipgloss.Width(viewSwitcher)
			if m.cache != nil {
				m.cache.switcherValid = true
				m.cache.switcherView = m.ctx.View
				m.cache.switcherRepoPath = m.ctx.RepoPath
				m.cache.switcherRepoUrl = m.ctx.RepoUrl
				m.cache.switcherUser = m.ctx.User
				m.cache.switcher = viewSwitcher
				m.cache.switcherWidth = switcherW
				m.cache.helpIndicator = helpIndicator
				m.cache.helpWidth = helpW
			}
		}
		gap := utils.Max(0, m.ctx.ScreenWidth-switcherW-lipgloss.Width(leftSection)-
			lipgloss.Width(rightSection)-helpW)
		spacing := lipgloss.NewStyle().
			Background(m.ctx.Theme.SelectedBackground).
			Render(strings.Repeat(" ", gap))

		footer = viewSwitcher + leftSection + spacing + rightSection + helpIndicator
	}

	rendered := footer
	if m.ShowAll {
		keymap := keys.CreateKeyMapForView(m.ctx.View)
		fullHelp := m.help.View(keymap)
		rendered = lipgloss.JoinVertical(lipgloss.Top, footer, fullHelp)
	}

	if m.cache != nil {
		m.cache.valid = true
		m.cache.view = m.ctx.View
		m.cache.leftSection = leftSection
		m.cache.rightSection = rightSection
		m.cache.showAll = m.ShowAll
		m.cache.showConfirmQuit = m.ShowConfirmQuit
		m.cache.screenWidth = m.ctx.ScreenWidth
		m.cache.repoPath = m.ctx.RepoPath
		m.cache.repoUrl = m.ctx.RepoUrl
		m.cache.user = m.ctx.User
		m.cache.rendered = rendered
	}
	return rendered
}

func (m *Model) SetShowConfirmQuit(val bool) {
	m.ShowConfirmQuit = val
}

func (m *Model) SetWidth(width int) {
	m.help.SetWidth(width)
	if m.cache != nil {
		m.cache.valid = false
	}
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	m.help.Styles = ctx.Styles.Help.BubbleStyles
	if m.cache != nil {
		m.cache.valid = false
		m.cache.switcherValid = false
	}
}

func (m *Model) renderViewButton(view config.ViewType) string {
	isActive := m.ctx.View == view

	// Define icons and labels for each view
	var icon, label string
	// Define icons - notifications has solid/outline variants
	solidBell := ""
	outlineBell := ""

	switch view {
	case config.NotificationsView:
		if m.ctx.View == config.NotificationsView {
			icon = solidBell
		} else {
			icon = outlineBell
		}
		label = ""
	case config.PRsView:
		icon = ""
		label = " PRs"
	case config.IssuesView:
		icon = ""
		label = " Issues"
	}

	if isActive {
		// Active: colored icon + prominent background
		// Use gold for notifications bell, green for others
		iconColor := m.ctx.Theme.SuccessText
		if view == config.NotificationsView {
			iconColor = compat.AdaptiveColor{
				Light: lipgloss.Color("#B8860B"),
				Dark:  lipgloss.Color("#FFD700"),
			} // Gold
		}
		activeStyle := lipgloss.NewStyle().
			Foreground(iconColor).
			Background(m.ctx.Styles.ViewSwitcher.ActiveView.GetBackground()).
			Bold(true)
		if label != "" {
			return activeStyle.Render(icon) + activeStyle.Render(label)
		}
		return activeStyle.Render(icon)
	}

	// Inactive: faint styling
	return m.ctx.Styles.ViewSwitcher.InactiveView.Render(icon + label)
}

func (m *Model) renderViewSwitcher(ctx *context.ProgramContext) string {
	var repo string
	if m.ctx.RepoPath != "" {
		name := path.Base(m.ctx.RepoPath)
		if m.ctx.RepoUrl != "" {
			name = git.GetRepoShortName(m.ctx.RepoUrl)
		}
		repo = ctx.Styles.Common.FooterStyle.Render(fmt.Sprintf(" %s", name))
	}

	var user string
	if ctx.User != "" {
		user = ctx.Styles.Common.FooterStyle.Render("@" + ctx.User)
	}

	view := lipgloss.JoinHorizontal(
		lipgloss.Top,
		ctx.Styles.ViewSwitcher.ViewsSeparator.PaddingLeft(1).
			Render(m.renderViewButton(config.NotificationsView)),
		ctx.Styles.ViewSwitcher.ViewsSeparator.Render(viewSeparator),
		m.renderViewButton(config.PRsView),
		ctx.Styles.ViewSwitcher.ViewsSeparator.Render(viewSeparator),
		m.renderViewButton(config.IssuesView),
		lipgloss.NewStyle().Background(ctx.Styles.Common.FooterStyle.GetBackground()).Foreground(
			ctx.Styles.ViewSwitcher.ViewsSeparator.GetBackground()).Render(" "),
		repo,
		ctx.Styles.Common.FooterStyle.Foreground(m.ctx.Theme.FaintText).Render(" • "),
		user,
		ctx.Styles.Common.FooterStyle.Foreground(m.ctx.Theme.FaintBorder).Render(" │"),
	)

	return ctx.Styles.ViewSwitcher.Root.Render(view)
}

func (m *Model) SetLeftSection(leftSection string) {
	*m.leftSection = leftSection
}

func (m *Model) SetRightSection(rightSection string) {
	*m.rightSection = rightSection
}

// HitKind describes which control on the footer bar a column landed on.
type HitKind int

const (
	HitNone HitKind = iota
	HitNotifications
	HitPRs
	HitIssues
	HitHelp
)

// HitTest maps a column x on the footer bar to its view-switcher button or help button.
func (m Model) HitTest(x int) HitKind {
	if m.ctx == nil || m.ShowConfirmQuit || x < 0 || x >= m.ctx.ScreenWidth {
		return HitNone
	}
	helpIndicator := lipgloss.NewStyle().
		Background(m.ctx.Theme.FaintText).
		Foreground(m.ctx.Theme.SelectedBackground).
		Padding(0, 1).
		Render("? help")
	if x >= m.ctx.ScreenWidth-lipgloss.Width(helpIndicator) {
		return HitHelp
	}
	pos := 0
	notif := m.ctx.Styles.ViewSwitcher.ViewsSeparator.PaddingLeft(1).
		Render(m.renderViewButton(config.NotificationsView))
	notifW := lipgloss.Width(notif)
	if x < pos+notifW {
		return HitNotifications
	}
	pos += notifW
	sepW := lipgloss.Width(m.ctx.Styles.ViewSwitcher.ViewsSeparator.Render(viewSeparator))
	pos += sepW
	prsW := lipgloss.Width(m.renderViewButton(config.PRsView))
	if x >= pos && x < pos+prsW {
		return HitPRs
	}
	pos += prsW + sepW
	issuesW := lipgloss.Width(m.renderViewButton(config.IssuesView))
	if x >= pos && x < pos+issuesW {
		return HitIssues
	}
	return HitNone
}
