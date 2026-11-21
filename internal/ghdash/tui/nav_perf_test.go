package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/footer"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/issueview"
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

func newPerfTestPRModel(t *testing.T, count int, enriched bool) *Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	cfg.Defaults.Preview.Open = true
	cfg.Defaults.Preview.Position = "right"
	cfg.Defaults.Preview.Width = 50

	ctx := &context.ProgramContext{
		Config:            &cfg,
		View:              config.PRsView,
		ScreenWidth:       140,
		ScreenHeight:      30,
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

	now := time.Now()
	sec := prssection.NewModel(0, ctx, config.PrsSectionConfig{Title: "Mine"}, now, now)
	prs := make([]prrow.Data, count)
	for i := range count {
		num := i + 1
		pr := data.PullRequestData{
			Number:      num,
			Title:       fmt.Sprintf("PR title #%d", num),
			Url:         fmt.Sprintf("https://github.com/acme/repo/pull/%d", num),
			State:       "OPEN",
			HeadRefName: fmt.Sprintf("branch-%d", num),
			BaseRefName: "main",
			Author:      struct{ Login string }{Login: "alice"},
			Repository:  data.Repository{Name: "repo", NameWithOwner: "acme/repo"},
			UpdatedAt:   now,
			CreatedAt:   now,
		}
		row := prrow.Data{Primary: &pr}
		if enriched {
			row.IsEnriched = true
			row.Enriched = data.EnrichedPullRequestData{
				Url:    pr.Url,
				Number: num,
				Title:  pr.Title,
				Body:   fmt.Sprintf("Summary body for PR #%d", num),
			}
		}
		prs[i] = row
	}
	sec.Prs = prs
	sec.TotalCount = len(prs)
	sec.Table.SetRows(sec.BuildRows())
	m.prs = []section.Section{&sec}
	m.updateTabs()
	m.setCurrSectionId(0)
	m.syncProgramContext()
	m.syncSidebar()
	m.syncFooterPager()
	return m
}

func TestPRNavLazyTableScrollResizeAndAlignment(t *testing.T) {
	m := newPerfTestPRModel(t, 25, true)

	// Every line of the left pane should have exact width MainContentWidth so
	// joinHorizontalFixed keeps the sidebar border aligned.
	secView := m.getCurrSection().View()
	for idx, line := range strings.Split(secView, "\n") {
		require.Equal(t, m.ctx.MainContentWidth, ansi.StringWidth(line),
			"section line %d width mismatch", idx)
	}

	// Sidebar height in right mode must equal MainContentHeight.
	sideLines := strings.Split(m.sidebar.View(), "\n")
	require.Equal(t, m.ctx.MainContentHeight, len(sideLines))

	// Jump to last item (past initial viewport) and verify last PR renders in both table and sidebar.
	m.Update(tea.KeyPressMsg{Text: "G"})
	require.Equal(t, 24, m.getCurrSection().CurrRow())
	view := ansi.Strip(m.View().Content)
	require.Contains(t, view, "PR title #25")
	require.Contains(t, view, "Summary body for PR #25")

	// Jump back to first item (use Home because test-config.yml binds "g" to lazygit).
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	require.Equal(t, 0, m.getCurrSection().CurrRow())
	view = ansi.Strip(m.View().Content)
	require.Contains(t, view, "PR title #1")
	require.Contains(t, view, "Summary body for PR #1")

	// Resize window and verify alignment still holds.
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 24})
	for idx, line := range strings.Split(m.getCurrSection().View(), "\n") {
		require.Equal(t, m.ctx.MainContentWidth, ansi.StringWidth(line),
			"resized section line %d width mismatch", idx)
	}
}

func TestPRNavEnrichDebounceAndStaleEnrichedMsg(t *testing.T) {
	m := newPerfTestPRModel(t, 5, false)

	// Step rapidly down 3 rows: enrichSeq advances on each step.
	m.Update(tea.KeyPressMsg{Text: "j"})
	staleSeq := m.enrichSeq
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Text: "j"})
	require.Equal(t, 3, m.getCurrSection().CurrRow())
	require.Greater(t, m.enrichSeq, staleSeq)

	// Stale debounce tick must not trigger enrichment.
	_, cmd := m.Update(enrichDebounceMsg{seq: staleSeq})
	require.Nil(t, cmd)
	require.Empty(t, m.enriching)

	// Matching debounce tick starts enrichment for PR #4 and records it in-flight.
	_, cmd = m.Update(enrichDebounceMsg{seq: m.enrichSeq})
	require.NotNil(t, cmd)
	pr4URL := "https://github.com/acme/repo/pull/4"
	require.True(t, m.enriching[pr4URL])

	// Duplicate trigger while in-flight returns nil.
	_, dupCmd := m.Update(enrichDebounceMsg{seq: m.enrichSeq})
	require.Nil(t, dupCmd)

	// Stale EnrichedPrMsg for PR #1 enriches PR #1's data without overwriting sidebar for PR #4.
	pr1URL := "https://github.com/acme/repo/pull/1"
	m.Update(prview.EnrichedPrMsg{
		Id:  0,
		Url: pr1URL,
		Data: data.EnrichedPullRequestData{
			Url:    pr1URL,
			Number: 1,
			Body:   "Stale PR 1 body",
		},
	})
	sec := m.prs[0].(*prssection.Model)
	require.True(t, sec.Prs[0].IsEnriched)
	require.NotContains(t, ansi.Strip(m.sidebar.View()), "Stale PR 1 body")

	// Matching EnrichedPrMsg for PR #4 clears in-flight state and updates sidebar.
	m.Update(prview.EnrichedPrMsg{
		Id:  0,
		Url: pr4URL,
		Data: data.EnrichedPullRequestData{
			Url:    pr4URL,
			Number: 4,
			Body:   "Current PR 4 body",
		},
	})
	require.False(t, m.enriching[pr4URL])
	require.True(t, sec.Prs[3].IsEnriched)
	require.Contains(t, ansi.Strip(m.sidebar.View()), "Current PR 4 body")
}
