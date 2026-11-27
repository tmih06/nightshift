package tabs

import (
	"fmt"
	"slices"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/carousel"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/section"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/keys"
	"github.com/tmih06/nightshift/internal/ghdash/utils"
)

type SectionTab struct {
	section section.Section
	spinner spinner.Model
}

type viewCache struct {
	valid         bool
	carouselView  string
	screenWidth   int
	version       string
	latestVersion string
	newSecKey     string
	rendered      string
}

type Model struct {
	sectionTabs   []SectionTab
	carousel      carousel.Model
	ctx           *context.ProgramContext
	latestVersion string
	cache         *viewCache
}

func NewModel(ctx *context.ProgramContext) Model {
	c := carousel.NewModel(
		carousel.WithHeight(1),
		carousel.WithOverflowIndicators("←", "→"),
		carousel.WithSeparators(),
	)
	m := Model{
		carousel: c,
		cache:    &viewCache{},
	}
	m.UpdateProgramContext(ctx)

	return m
}

func (m Model) Init() tea.Cmd {
	return m.fetchHasNewVersion()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0)
	switch msg := msg.(type) {
	case latestVersionMsg:
		m.latestVersion = msg.version
	case spinner.TickMsg:
		for i, tab := range m.sectionTabs {
			if tab.section.GetIsLoading() {
				var cmd tea.Cmd
				m.sectionTabs[i].spinner, cmd = tab.spinner.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	m.UpdateTabTitles()

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	crsl := m.carousel.View()
	newSecKey := keys.Keys.NewSection.Help().Key
	if m.cache != nil && m.cache.valid &&
		m.cache.carouselView == crsl &&
		m.cache.screenWidth == m.ctx.ScreenWidth &&
		m.cache.version == m.ctx.Version &&
		m.cache.latestVersion == m.latestVersion &&
		m.cache.newSecKey == newSecKey {
		return m.cache.rendered
	}
	logo := m.viewLogo()
	rendered := m.ctx.Styles.Tabs.TabsRow.
		Width(m.ctx.ScreenWidth).
		Height(common.HeaderHeight).
		Render(lipgloss.JoinHorizontal(lipgloss.Bottom, crsl, m.viewNewSectionButton(), logo))
	if m.cache != nil {
		m.cache.valid = true
		m.cache.carouselView = crsl
		m.cache.screenWidth = m.ctx.ScreenWidth
		m.cache.version = m.ctx.Version
		m.cache.latestVersion = m.latestVersion
		m.cache.newSecKey = newSecKey
		m.cache.rendered = rendered
	}
	return rendered
}

type latestVersionMsg struct {
	version string
	err     error
}

func (m Model) viewNewSectionButton() string {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(m.ctx.Styles.Tabs.TabSeparator.GetForeground()).
		Render(
			lipgloss.JoinHorizontal(lipgloss.Top,
				lipgloss.NewStyle().
					Foreground(m.ctx.Styles.Colors.SuccessText).
					Render("󱅃 "),
				keys.Keys.NewSection.Help().Key,
			))
}

func (m Model) carouselWidth() int {
	logo := m.viewLogo()
	newSectionButton := m.viewNewSectionButton()
	return m.ctx.ScreenWidth - lipgloss.Width(logo) -
		lipgloss.Width(newSectionButton)
}

func (m *Model) fetchHasNewVersion() tea.Cmd {
	return func() tea.Msg {
		r, err := data.FetchLatestVersion()
		return latestVersionMsg{
			version: r.Repository.LatestRelease.TagName,
			err:     err,
		}
	}
}

func (m *Model) CurrSectionId() int {
	return m.carousel.Cursor()
}

func (m *Model) SetCurrSectionId(id int) {
	m.carousel.SetCursor(id)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = ctx
	if m.cache != nil {
		m.cache.valid = false
	}
	m.carousel.SetStyles(carousel.Styles{
		Item:              ctx.Styles.Tabs.Tab,
		Selected:          ctx.Styles.Tabs.ActiveTab,
		OverflowIndicator: ctx.Styles.Tabs.OverflowIndicator,
		Separator:         ctx.Styles.Tabs.TabSeparator,
	})

	m.carousel.SetWidth(m.carouselWidth())
}

func (m *Model) SetSections(sections []section.Section) {
	sectionTabs := make([]SectionTab, 0)
	for _, s := range sections {
		tab := SectionTab{section: s, spinner: spinner.New(
			spinner.WithSpinner(spinner.Dot), spinner.WithStyle(
				lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).PaddingLeft(2)))}
		sectionTabs = append(sectionTabs, tab)
	}
	m.sectionTabs = sectionTabs
	m.UpdateTabTitles()
}

func (m *Model) UpdateTabTitles() {
	titles := make([]string, 0)
	for i, tab := range m.sectionTabs {
		cfg := tab.section.GetConfig()
		title := cfg.Title
		if tab.section.GetIsLoading() {
			title = fmt.Sprintf("%s %s", title, m.sectionTabs[i].spinner.View())
		} else if m.ctx.Config.Theme.Ui.SectionsShowCount {
			title = fmt.Sprintf("%s (%s)", title,
				utils.ShortNumber(tab.section.GetTotalCount()))
		}

		titles = append(titles, title)
	}

	if slices.Equal(titles, m.carousel.Items()) {
		return
	}

	oldCursor := m.carousel.Cursor()
	m.carousel.SetItems(titles)
	m.carousel.SetCursor(oldCursor)
}

func (m *Model) viewLogo() string {
	version := lipgloss.NewStyle().Foreground(m.ctx.Theme.SecondaryText).Render(m.ctx.Version)
	if m.latestVersion != "" && m.ctx.Version != "dev" && m.ctx.Version != m.latestVersion {
		version = lipgloss.JoinHorizontal(
			lipgloss.Top,
			version,
			lipgloss.NewStyle().
				Foreground(m.ctx.Styles.Colors.SuccessText).
				Render("  Update available!"),
		)
	}

	return lipgloss.NewStyle().
		Margin(0, 1, 0, 2).
		Render(lipgloss.JoinHorizontal(
			lipgloss.Bottom,
			lipgloss.NewStyle().
				Background(lipgloss.Darken(context.LogoColor, 0.7)).
				Padding(0, 1).
				Foreground(context.LogoColor).
				Bold(true).
				Render("DASH"),
			" ",
			version,
		))
}

// HitKind describes what part of the section tabs row a column landed on.
type HitKind int

const (
	HitNone HitKind = iota
	HitSection
	HitPrevOverflow
	HitNextOverflow
	HitNewSection
)

// HitTest maps a column on the tabs row to a section tab, overflow arrow, or
// the new-section button.
func (m Model) HitTest(x int) (HitKind, int) {
	if x < 0 || m.ctx == nil {
		return HitNone, -1
	}
	btnW := lipgloss.Width(m.viewNewSectionButton())
	cw := m.ctx.ScreenWidth - lipgloss.Width(m.viewLogo()) - btnW
	if x < cw {
		kind, id := m.carousel.HitTest(x)
		switch kind {
		case carousel.HitItem:
			return HitSection, id
		case carousel.HitLeftOverflow:
			return HitPrevOverflow, -1
		case carousel.HitRightOverflow:
			return HitNextOverflow, -1
		}
		return HitNone, -1
	}
	if x < cw+btnW {
		return HitNewSection, -1
	}
	return HitNone, -1
}

func (m *Model) SetAllLoading() []tea.Cmd {
	cmds := make([]tea.Cmd, 0)
	for i := range m.sectionTabs {
		cmds = append(cmds, m.sectionTabs[i].spinner.Tick)
	}

	return cmds
}
