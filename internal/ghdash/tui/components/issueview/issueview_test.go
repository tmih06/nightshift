package issueview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func newTestContext(t *testing.T) *context.ProgramContext {
	t.Helper()

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)
	return &context.ProgramContext{
		Config:            &cfg,
		Theme:             thm,
		Styles:            context.InitStyles(thm),
		HasDarkBackground: true,
		BackgroundSource:  "default",
	}
}

func TestNewModelSetsProgramContext(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)

	require.NotNil(t, m.ctx)
	require.Same(t, ctx, m.ctx)
}

func TestRenderBodyDoesNotPanicBeforeContextSync(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)
	m.SetWidth(80)
	m.SetRow(&data.IssueData{
		Title: "Example issue",
		Body:  "Hello **world**",
	})

	require.NotPanics(t, func() {
		_ = m.renderBody()
	})
}
