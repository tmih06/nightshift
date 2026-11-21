package dialog

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
)

func TestRenderAtAndOverlayLayers(t *testing.T) {
	th := *ghtheme.DefaultTheme
	parent := Menu{
		Title:        "Parent",
		Lines:        []string{"subtitle"},
		OptionsLabel: "Pool",
		Options:      []Option{{Text: " 1  codex-pool  › "}, {Text: " 2  pi-pool     › "}},
		Cursor:       1,
		Dimmed:       true,
	}.Render(th, 80).At(4, 3)

	if parent.X != 4 || parent.Y != 3 {
		t.Fatalf("parent pos = (%d, %d), want (4, 3)", parent.X, parent.Y)
	}
	if !parent.Inside(parent.X+2, parent.OptionY(1)) {
		t.Fatal("OptionY(1) fell outside parent box")
	}
	if idx, ok := parent.OptionAt(parent.X+2, parent.OptionY(1)); !ok || idx != 1 {
		t.Fatalf("OptionAt = (%d, %v), want (1, true)", idx, ok)
	}

	sub := Menu{
		Title:        "Pi",
		Lines:        []string{"pi-pool · pi"},
		OptionsLabel: "Model",
		Options:      []Option{{Text: " 1  anthropic/claude-sonnet "}},
		Cursor:       0,
	}.Render(th, 80).At(parent.X+parent.W+1, parent.OptionY(1)-1)

	out := OverlayLayers("", 20,
		lipgloss.NewLayer(parent.Box).X(parent.X).Y(parent.Y).Z(1),
		lipgloss.NewLayer(sub.Box).X(sub.X).Y(sub.Y).Z(2),
	)
	rows := strings.Split(out, "\n")
	if len(rows) != 20 {
		t.Errorf("overlay height = %d, want 20", len(rows))
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"Parent", "pi-pool", "Pi", "anthropic/claude-sonnet"} {
		if !strings.Contains(plain, want) {
			t.Errorf("overlay missing %q:\n%s", want, plain)
		}
	}
}
