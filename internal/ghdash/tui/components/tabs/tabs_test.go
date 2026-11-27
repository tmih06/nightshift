package tabs

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func newTestTabs(t *testing.T, width int) Model {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := &context.ProgramContext{
		Config:      &cfg,
		ScreenWidth: width,
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)
	return NewModel(ctx)
}

func TestHitTestSwallowsOutOfRangeX(t *testing.T) {
	m := newTestTabs(t, 100)

	if kind, _ := m.HitTest(-1); kind != HitNone {
		t.Errorf("HitTest(-1) = %v, want HitNone", kind)
	}
	if kind, _ := m.HitTest(m.ctx.ScreenWidth + 1); kind != HitNone {
		t.Errorf("HitTest(past the edge) = %v, want HitNone", kind)
	}
}

func TestHitTestWithoutContextIsSafe(t *testing.T) {
	m := Model{}
	if kind, _ := m.HitTest(3); kind != HitNone {
		t.Errorf("HitTest with nil context = %v, want HitNone", kind)
	}
}

func TestHitTestFindsTheNewSectionButton(t *testing.T) {
	m := newTestTabs(t, 100)

	logoW := lipgloss.Width(m.viewLogo())
	btnW := lipgloss.Width(m.viewNewSectionButton())
	cw := m.ctx.ScreenWidth - logoW - btnW

	for x := cw; x < cw+btnW; x++ {
		if kind, _ := m.HitTest(x); kind != HitNewSection {
			t.Errorf("HitTest(%d) inside the button = %v, want HitNewSection", x, kind)
		}
	}
	if kind, _ := m.HitTest(cw + btnW); kind == HitNewSection {
		t.Error("HitTest past the button still hits the button")
	}
}

func TestHitTestInsideAnEmptyCarouselHitsNothing(t *testing.T) {
	m := newTestTabs(t, 100)

	if kind, _ := m.HitTest(2); kind != HitNone {
		t.Errorf("HitTest inside a sectionless carousel = %v, want HitNone", kind)
	}
}
