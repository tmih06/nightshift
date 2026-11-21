package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/stretchr/testify/require"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/footer"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/issuessection"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/issueview"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/notificationssection"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/notificationview"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/prrow"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/prssection"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/prview"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/section"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/sidebar"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/tabs"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/keys"
	"github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func newMouseTestModel(t *testing.T, width, height int, previewPos string) *Model {
	t.Helper()
	zone.NewGlobal()
	zone.SetEnabled(false)

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	cfg.Defaults.Preview.Open = true
	cfg.Defaults.Preview.Position = previewPos
	cfg.Defaults.Preview.Width = 50
	cfg.Defaults.Preview.Height = 0.4

	ctx := &context.ProgramContext{
		Config:            &cfg,
		View:              config.PRsView,
		ScreenWidth:       width,
		ScreenHeight:      height,
		HasDarkBackground: true,
		StartTask:         func(task context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	m := &Model{
		ctx:              ctx,
		keys:             keys.Keys,
		sidebar:          sidebar.NewModel(),
		prView:           prview.NewModel(ctx),
		issueSidebar:     issueview.NewModel(ctx),
		notificationView: notificationview.NewModel(ctx),
		footer:           footer.NewModel(ctx),
		tabs:             tabs.NewModel(ctx),
		tasks:            map[string]context.Task{},
	}
	m.sidebar.IsOpen = true
	m.syncMainContentDimensions()
	m.syncProgramContext()

	now := time.Now()
	makePRs := func(prefix string, count int) []prrow.Data {
		out := make([]prrow.Data, count)
		for i := range count {
			num := i + 1
			pr := data.PullRequestData{
				Number:      num,
				Title:       fmt.Sprintf("%s PR #%d", prefix, num),
				Url:         fmt.Sprintf("https://github.com/acme/repo/pull/%d", num),
				State:       "OPEN",
				HeadRefName: fmt.Sprintf("branch-%d", num),
				BaseRefName: "main",
				Author:      struct{ Login string }{Login: "alice"},
				Repository: data.Repository{
					Name:          "repo",
					NameWithOwner: "acme/repo",
				},
				UpdatedAt: now,
				CreatedAt: now,
			}
			body := strings.Repeat("Long line of PR description to make the sidebar scroll.\n", 40)
			out[i] = prrow.Data{
				Primary:    &pr,
				Enriched:   data.EnrichedPullRequestData{Url: pr.Url, Body: body},
				IsEnriched: true,
			}
		}
		return out
	}

	sec0 := prssection.NewModel(0, ctx, config.PrsSectionConfig{Title: "Mine"}, now, now)
	sec0.Prs = makePRs("Mine", 6)
	sec0.TotalCount = len(sec0.Prs)
	sec0.Table.SetRows(sec0.BuildRows())

	sec1 := prssection.NewModel(1, ctx, config.PrsSectionConfig{Title: "Review"}, now, now)
	sec1.Prs = makePRs("Review", 4)
	sec1.TotalCount = len(sec1.Prs)
	sec1.Table.SetRows(sec1.BuildRows())

	sec2 := prssection.NewModel(2, ctx, config.PrsSectionConfig{Title: "All"}, now, now)
	sec2.Prs = makePRs("All", 3)
	sec2.TotalCount = len(sec2.Prs)
	sec2.Table.SetRows(sec2.BuildRows())

	m.prs = []section.Section{&sec0, &sec1, &sec2}

	issueSec := issuessection.NewModel(0, ctx, config.IssuesSectionConfig{Title: "Assigned"}, now, now)
	m.issues = []section.Section{&issueSec}

	notifSec := notificationssection.NewModel(0, ctx, config.NotificationsSectionConfig{Title: "Inbox"}, now)
	m.notifications = []section.Section{&notifSec}

	m.updateTabs()
	m.setCurrSectionId(0)
	m.syncProgramContext()
	m.syncSidebar()
	m.syncFooterPager()
	return m
}

func findTextInView(t *testing.T, view, substr string) (x, y int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(view), "\n")
	for row, line := range lines {
		if idx := strings.Index(line, substr); idx >= 0 {
			// Compute display column width before idx (handles wide/nerd-font runes).
			col := ansi.StringWidth(line[:idx])
			return col, row
		}
	}
	t.Fatalf("could not find %q in view:\n%s", substr, ansi.Strip(view))
	return 0, 0
}

func TestMouseClicksSectionTabsAndNewSectionButton(t *testing.T) {
	m := newMouseTestModel(t, 140, 35, "right")
	view := m.View().Content

	reviewX, reviewY := findTextInView(t, view, "Review")
	require.Equal(t, 0, reviewY)

	m.Update(tea.MouseClickMsg{X: reviewX + 1, Y: 0, Button: tea.MouseLeft})
	require.Equal(t, 1, m.currSectionId, "clicking Review tab should select section 1")

	allX, _ := findTextInView(t, m.View().Content, "All")
	m.Update(tea.MouseClickMsg{X: allX + 1, Y: 0, Button: tea.MouseLeft})
	require.Equal(t, 2, m.currSectionId, "clicking All tab should select section 2")

	// Clicking the new-section button appends a section and selects it.
	btnX := m.HitTestButtonXForTest()
	before := len(m.prs)
	m.Update(tea.MouseClickMsg{X: btnX, Y: 0, Button: tea.MouseLeft})
	require.Equal(t, before+1, len(m.prs), "clicking new-section button should add a section")
	require.Equal(t, before, m.currSectionId)
}

func TestMouseClickSelectsRowAndDoubleClickOpensPR(t *testing.T) {
	m := newMouseTestModel(t, 140, 35, "right")
	view := m.View().Content

	pr3X, pr3Y := findTextInView(t, view, "acme/repo #3")
	_, cmd := m.Update(tea.MouseClickMsg{X: pr3X, Y: pr3Y, Button: tea.MouseLeft})
	require.Equal(t, 2, m.getCurrSection().CurrRow(), "clicking PR #3 should select row index 2")
	// Single click must not open the PR.
	if cmd != nil {
		msg := cmd()
		_, isOpen := msg.(OpenPullRequestMsg)
		require.False(t, isOpen, "single click must not emit OpenPullRequestMsg")
	}

	// Clicking the same row again immediately (double click) opens the PR.
	_, cmd = m.Update(tea.MouseClickMsg{X: pr3X, Y: pr3Y, Button: tea.MouseLeft})
	require.NotNil(t, cmd, "double click on a PR row should return a command")
	openMsg, ok := cmd().(OpenPullRequestMsg)
	require.True(t, ok, "double click on a PR row should emit OpenPullRequestMsg")
	require.Equal(t, 3, openMsg.Number)
	require.Equal(t, "acme/repo", openMsg.Repo)

	// Clicking the separator line below row #3 (row is 2 content lines + 1 separator) does not change selection.
	m.Update(tea.MouseClickMsg{X: pr3X, Y: pr3Y + 2, Button: tea.MouseLeft})
	require.Equal(t, 2, m.getCurrSection().CurrRow())
}

func TestMouseWheelNavigatesTableAndScrollsSidebar(t *testing.T) {
	m := newMouseTestModel(t, 140, 35, "right")

	// Wheel down over the table moves row selection without scrolling sidebar.
	m.Update(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown})
	require.Equal(t, 1, m.getCurrSection().CurrRow())
	require.Equal(t, 0, m.sidebar.YOffset())

	m.Update(tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp})
	require.Equal(t, 0, m.getCurrSection().CurrRow())

	// Wheel down over the sidebar scrolls the sidebar without changing the row.
	sideX := m.ctx.MainContentWidth + 5
	m.Update(tea.MouseWheelMsg{X: sideX, Y: 10, Button: tea.MouseWheelDown})
	require.Equal(t, 0, m.getCurrSection().CurrRow())
	require.Equal(t, mouseWheelLines, m.sidebar.YOffset())

	m.Update(tea.MouseWheelMsg{X: sideX, Y: 10, Button: tea.MouseWheelUp})
	require.Equal(t, 0, m.sidebar.YOffset())
}

func TestMouseDragResizesRightAndBottomDivider(t *testing.T) {
	// Right mode drag.
	m := newMouseTestModel(t, 140, 35, "right")
	initialWidth := m.ctx.DynamicPreviewWidth
	divX := m.ctx.MainContentWidth

	m.Update(tea.MouseClickMsg{X: divX, Y: 10, Button: tea.MouseLeft})
	require.True(t, m.dragging)
	m.Update(tea.MouseMotionMsg{X: divX - 15, Y: 10, Button: tea.MouseLeft})
	require.Greater(t, m.ctx.DynamicPreviewWidth, initialWidth)
	afterDrag := m.ctx.DynamicPreviewWidth

	m.Update(tea.MouseReleaseMsg{X: divX - 15, Y: 10, Button: tea.MouseLeft})
	require.False(t, m.dragging)
	m.Update(tea.MouseMotionMsg{X: divX + 10, Y: 10, Button: tea.MouseLeft})
	require.Equal(t, afterDrag, m.ctx.DynamicPreviewWidth, "motion after release must not resize")

	// Bottom mode drag.
	mb := newMouseTestModel(t, 140, 35, "bottom")
	initialHeight := mb.ctx.DynamicPreviewHeight
	divY := mb.bodyTop() + mb.ctx.MainContentHeight

	mb.Update(tea.MouseClickMsg{X: 20, Y: divY, Button: tea.MouseLeft})
	require.True(t, mb.dragging)
	mb.Update(tea.MouseMotionMsg{X: 20, Y: divY - 4, Button: tea.MouseLeft})
	require.Greater(t, mb.ctx.DynamicPreviewHeight, initialHeight)
	mb.Update(tea.MouseReleaseMsg{X: 20, Y: divY - 4, Button: tea.MouseLeft})
	require.False(t, mb.dragging)
}

func TestMouseClicksSidebarPRPreviewTabs(t *testing.T) {
	m := newMouseTestModel(t, 150, 35, "right")
	view := m.View().Content

	actX, actY := findTextInView(t, view, "Activity")
	m.Update(tea.MouseClickMsg{X: actX + 1, Y: actY, Button: tea.MouseLeft})
	require.Contains(t, m.prView.SelectedTab(), "Activity")

	checksX, checksY := findTextInView(t, m.View().Content, "Checks")
	m.Update(tea.MouseClickMsg{X: checksX + 1, Y: checksY, Button: tea.MouseLeft})
	require.Contains(t, m.prView.SelectedTab(), "Checks")
}

func TestMouseClicksSearchBarAndFooterControls(t *testing.T) {
	m := newMouseTestModel(t, 140, 35, "right")

	// Click search bar to focus it.
	m.Update(tea.MouseClickMsg{X: 5, Y: m.bodyTop() + 1, Button: tea.MouseLeft})
	require.True(t, m.getCurrSection().IsSearchFocused())

	// Click a table row to blur search and select the row.
	pr2X, pr2Y := findTextInView(t, m.View().Content, "acme/repo #2")
	m.Update(tea.MouseClickMsg{X: pr2X, Y: pr2Y, Button: tea.MouseLeft})
	require.False(t, m.getCurrSection().IsSearchFocused())
	require.Equal(t, 1, m.getCurrSection().CurrRow())

	// Click Issues in the footer bar to switch view.
	issuesX, issuesY := findTextInView(t, m.View().Content, "Issues")
	require.Equal(t, m.footerY(), issuesY)
	m.Update(tea.MouseClickMsg{X: issuesX + 1, Y: issuesY, Button: tea.MouseLeft})
	require.Equal(t, config.IssuesView, m.ctx.View)

	// Click ? help in the footer bar to toggle expanded help.
	helpX, helpY := findTextInView(t, m.View().Content, "? help")
	require.Equal(t, m.footerY(), helpY)
	m.Update(tea.MouseClickMsg{X: helpX + 1, Y: helpY, Button: tea.MouseLeft})
	require.True(t, m.footer.ShowAll)
}

func TestRepoViewBridge(t *testing.T) {
	m := newMouseTestModel(t, 140, 35, "right")

	// Clicking Repos in the footer returns a cmd producing OpenRepoViewMsg.
	reposX, reposY := findTextInView(t, m.View().Content, "Repos")
	require.Equal(t, m.footerY(), reposY)
	_, cmd := m.Update(tea.MouseClickMsg{X: reposX + 1, Y: reposY, Button: tea.MouseLeft})
	require.NotNil(t, cmd, "clicking Repos should return a command")
	_, ok := cmd().(OpenRepoViewMsg)
	require.True(t, ok, "clicking Repos should emit OpenRepoViewMsg")

	// Pressing s in IssuesView returns a cmd producing OpenRepoViewMsg.
	m.SwitchView(config.IssuesView)
	require.Equal(t, config.IssuesView, m.ctx.View)
	_, cmd = m.Update(tea.KeyPressMsg{Text: "s"})
	require.NotNil(t, cmd, "pressing s in IssuesView should return a command")
	_, ok = cmd().(OpenRepoViewMsg)
	require.True(t, ok, "pressing s in IssuesView should emit OpenRepoViewMsg")

	// Calling SwitchView switches cleanly to NotificationsView.
	m.SwitchView(config.NotificationsView)
	require.Equal(t, config.NotificationsView, m.ctx.View)

	// Calling SwitchView before config is loaded updates ctx.View without panicking.
	uninit := NewModel(config.Location{}, Repositories{})
	require.NotPanics(t, func() {
		uninit.SwitchView(config.NotificationsView)
	})
	require.Equal(t, config.NotificationsView, uninit.ctx.View)
}

func (m *Model) HitTestButtonXForTest() int {
	for x := 0; x < m.ctx.ScreenWidth; x++ {
		if kind, _ := m.tabs.HitTest(x); kind == tabs.HitNewSection {
			return x
		}
	}
	return -1
}

func BenchmarkMouseDragSameWidth(b *testing.B) {
	t := &testing.T{}
	m := newMouseTestModel(t, 140, 35, "right")
	divX := m.ctx.MainContentWidth
	m.Update(tea.MouseClickMsg{X: divX, Y: 10, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: divX - 10, Y: 10, Button: tea.MouseLeft})
	for b.Loop() {
		m.Update(tea.MouseMotionMsg{X: divX - 10, Y: 11, Button: tea.MouseLeft})
	}
}
