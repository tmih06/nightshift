package tui

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

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

func newBenchPRModel(b *testing.B, numSections, prsPerSection int, sidebarOpen, enriched bool) *Model {
	b.Helper()
	log.SetOutput(io.Discard)

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		b.Fatalf("parse config: %v", err)
	}
	cfg.Defaults.Preview.Open = sidebarOpen
	cfg.Defaults.Preview.Position = "right"
	cfg.Defaults.Preview.Width = 60

	ctx := &context.ProgramContext{
		Config:            &cfg,
		View:              config.PRsView,
		ScreenWidth:       160,
		ScreenHeight:      44,
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
	m.sidebar.IsOpen = sidebarOpen
	m.syncMainContentDimensions()

	now := time.Now()
	sampleBody := func(n int) string {
		return fmt.Sprintf(`<!-- generated template comment -->
## Summary

Implements feature #%d for the pull request list view.

- Keeps navigation fast across large repositories
- Preserves **markdown** formatting and `+"`inline code`"+`
- Updates tests and benchmarks

`+"```go"+`
func handleUpdate(msg tea.Msg) error {
	for i := 0; i < %d; i++ {
		processRow(i)
	}
	return nil
}
`+"```"+`

### Test plan

- [x] Unit tests pass
- [x] Benchmarks show reduced latency
`, n, n) + strings.Repeat("\nAdditional context paragraph explaining design trade-offs and edge cases.\n", 4)
	}

	m.prs = make([]section.Section, numSections)
	for s := range numSections {
		sec := prssection.NewModel(s, ctx, config.PrsSectionConfig{
			Title: fmt.Sprintf("Section %d", s+1),
		}, now, now)
		prs := make([]prrow.Data, prsPerSection)
		for i := range prsPerSection {
			num := s*1000 + i + 1
			pr := data.PullRequestData{
				Number:            num,
				Title:             fmt.Sprintf("feat(tui): improve PR navigation latency for case #%d", num),
				Url:               fmt.Sprintf("https://github.com/acme/nightshift/pull/%d", num),
				State:             "OPEN",
				HeadRefName:       fmt.Sprintf("feat/pr-nav-%d", num),
				BaseRefName:       "main",
				Additions:         120 + i*7,
				Deletions:         45 + i*3,
				Author:            struct{ Login string }{Login: "alice"},
				AuthorAssociation: "MEMBER",
				Repository: data.Repository{
					Name:          "nightshift",
					NameWithOwner: "acme/nightshift",
				},
				Labels: data.PRLabels{
					Nodes: []data.Label{
						{Name: "enhancement", Color: "a2eeef"},
						{Name: "performance", Color: "d73a4a"},
					},
				},
				UpdatedAt: now.Add(-time.Duration(i) * time.Minute),
				CreatedAt: now.Add(-time.Duration(i+1) * time.Hour),
			}
			row := prrow.Data{Primary: &pr}
			if enriched {
				row.IsEnriched = true
				row.Enriched = data.EnrichedPullRequestData{
					Url:    pr.Url,
					Number: pr.Number,
					Title:  pr.Title,
					Body:   sampleBody(num),
				}
			}
			prs[i] = row
		}
		sec.Prs = prs
		sec.TotalCount = len(prs)
		sec.Table.SetRows(sec.BuildRows())
		m.prs[s] = &sec
	}

	m.updateTabs()
	m.setCurrSectionId(0)
	m.syncProgramContext()
	m.syncSidebar()
	m.syncFooterPager()
	return m
}

func BenchmarkPRNavKeyToFrame(b *testing.B) {
	m := newBenchPRModel(b, 4, 30, true, true)
	down := tea.KeyPressMsg{Text: "j"}
	up := tea.KeyPressMsg{Text: "k"}
	b.ResetTimer()
	for b.Loop() {
		m.Update(down)
		_ = m.View()
		m.Update(up)
		_ = m.View()
	}
}

func BenchmarkPRNavKeyToFrameSidebarClosed(b *testing.B) {
	m := newBenchPRModel(b, 4, 30, false, true)
	down := tea.KeyPressMsg{Text: "j"}
	up := tea.KeyPressMsg{Text: "k"}
	b.ResetTimer()
	for b.Loop() {
		m.Update(down)
		_ = m.View()
		m.Update(up)
		_ = m.View()
	}
}

func BenchmarkPRNavWheelToFrame(b *testing.B) {
	m := newBenchPRModel(b, 4, 30, true, true)
	down := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 10, Y: 10, Button: tea.MouseWheelUp}
	b.ResetTimer()
	for b.Loop() {
		m.Update(down)
		_ = m.View()
		m.Update(up)
		_ = m.View()
	}
}

func BenchmarkPRNavSpinnerTick(b *testing.B) {
	m := newBenchPRModel(b, 4, 30, true, true)
	tick := spinner.TickMsg{Time: time.Now()}
	b.ResetTimer()
	for b.Loop() {
		m.Update(tick)
		_ = m.View()
	}
}

func BenchmarkPRViewOnly(b *testing.B) {
	m := newBenchPRModel(b, 4, 30, true, true)
	b.ResetTimer()
	for b.Loop() {
		_ = m.View()
	}
}

func BenchmarkPRNavColdSweep(b *testing.B) {
	down := tea.KeyPressMsg{Text: "j"}
	for b.Loop() {
		b.StopTimer()
		m := newBenchPRModel(b, 4, 30, true, true)
		b.StartTimer()
		for range 20 {
			m.Update(down)
			_ = m.View()
		}
	}
}

func BenchmarkPRNavColdLargeBody(b *testing.B) {
	m := newBenchPRModel(b, 1, 10, true, true)
	sec := m.prs[0].(*prssection.Model)
	block := "\n```go\nfunc process(items []string) int {\n\ttotal := 0\n\tfor _, item := range items {\n\t\ttotal += len(item)\n\t}\n\treturn total\n}\n```\n\n" +
		"- Bullet point describing **architecture**, `config.yml`, and edge cases.\n" +
		"- Follow-up item covering rollout and verification.\n"
	bigTail := strings.Repeat(block, 20)
	down := tea.KeyPressMsg{Text: "j"}
	up := tea.KeyPressMsg{Text: "k"}

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		// Unique body per iteration so markdown and prview caches are truly cold.
		idx := (i % 2) ^ 1
		sec.Prs[idx].Enriched.Body = fmt.Sprintf("## PR Change %d\n\nShort intro paragraph for change %d.\n", i, i) + bigTail
		b.StartTimer()
		if i%2 == 0 {
			m.Update(down)
		} else {
			m.Update(up)
		}
		_ = m.View()
	}
}

func BenchmarkPRCommitsTab(b *testing.B) {
	m := newBenchPRModel(b, 1, 2, true, true)
	sec := m.prs[0].(*prssection.Model)
	now := time.Now()
	commits := make([]struct {
		Commit struct {
			AbbreviatedOid  string
			CommittedDate   time.Time
			MessageHeadline string
			Author          struct {
				Name string
				User struct {
					Login string
				}
			}
			StatusCheckRollup data.StatusCheckRollupStats
		}
	}, 80)
	for i := range commits {
		commits[i].Commit.AbbreviatedOid = fmt.Sprintf("%07x", i+1)
		commits[i].Commit.CommittedDate = now.Add(-time.Duration(i) * time.Hour)
		commits[i].Commit.MessageHeadline = fmt.Sprintf("feat(tui): commit headline number %d", i+1)
		commits[i].Commit.Author.Name = "alice"
		commits[i].Commit.Author.User.Login = "alice"
	}
	sec.Prs[0].Enriched.AllCommits.Nodes = commits
	m.prView.SetRow(&sec.Prs[0])
	nextTab := tea.KeyPressMsg{Text: "]"}
	prevTab := tea.KeyPressMsg{Text: "["}
	// Move to Activity (1), then toggle to Commits (2) and back.
	m.Update(nextTab)

	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		// Bust prView cache with fresh width/headline variation only on first render:
		sec.Prs[0].Primary.Additions = i
		b.StartTimer()
		m.Update(nextTab)
		_ = m.View()
		m.Update(prevTab)
	}
}
