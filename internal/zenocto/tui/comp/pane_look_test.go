package comp_test

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/zenocto/tui/comp"
)

// nightshift paints panes the way gh-dash paints its views. These pin that
// look; zen-octo's own boxed look is comp.ClassicChrome, kept for its tests.

func lines(s string) []string { return strings.Split(s, "\n") }

func TestAPanesHeadingIsABarOnTheSelectedBackground(t *testing.T) {
	out := pane().Title("Files").Footer("3/9").Size(40, 5).Render("a")
	top := lines(out)[0]
	bg := sgrParams(lipgloss.NewStyle().Background(testTheme.SelectedBackground))

	if !strings.Contains(top, bg) {
		t.Errorf("heading = %q, want it on the selected background", top)
	}
	bare := ansi.Strip(top)
	if !strings.HasPrefix(bare, "  Files") || !strings.HasSuffix(strings.TrimRight(bare, " "), "3/9") {
		t.Errorf("heading = %q, want the title at the start and the scroll position at the end", bare)
	}
	for _, glyph := range []string{"╭", "╮", "╰", "╯", "─"} {
		if strings.Contains(ansi.Strip(out), glyph) {
			t.Errorf("the pane still draws %q, want no box", glyph)
		}
	}
}

func TestTheFocusedPaneHeadsInBoldTextAndTheRestMuted(t *testing.T) {
	focused := lines(pane().Title("Feed").Focus(true).Size(30, 4).Render(""))[0]
	idle := lines(pane().Title("Feed").Size(30, 4).Render(""))[0]

	if !strings.Contains(focused, "\x1b[1") {
		t.Error("the focused heading is not bold")
	}
	if !strings.Contains(idle, fgSeq(testTheme.MutedOrSubtle())) {
		t.Error("the idle heading is not muted")
	}
}

func TestTabsInAHeadingAreSplitByABar(t *testing.T) {
	top := ansi.Strip(lines(pane().Tabs([]comp.Tab{{Label: "My PRs", Badge: "12"}, {Label: "Review"}}, 0).Size(60, 4).Render(""))[0])
	if !strings.Contains(top, "My PRs 12  |  Review") {
		t.Errorf("heading = %q, want gh-dash's tabs split by a bar", top)
	}
}

func TestARuledPaneDrawsOneRuleDownItsLeftEdge(t *testing.T) {
	out := ansi.Strip(pane().Title("Diff").Ruled(true).Size(20, 4).Render("x\ny"))
	for i, line := range lines(out) {
		if !strings.HasPrefix(line, "│") {
			t.Errorf("line %d = %q, want the rule down the left edge", i, line)
		}
	}
	plain := ansi.Strip(pane().Title("Diff").Size(20, 4).Render("x"))
	if strings.Contains(plain, "│") {
		t.Errorf("an unruled pane drew a rule: %q", plain)
	}
}

func TestACardMarksFocusWithAnAccentBarAndClosesOnAFaintRule(t *testing.T) {
	lit := pane().Header(" octocat said").Focus(true).Footer("r reply").Size(30, 6).Render("body")
	rows := lines(lit)

	if !strings.Contains(rows[3], fgSeq(testTheme.Accent)) || !strings.HasPrefix(ansi.Strip(rows[3]), "▎") {
		t.Errorf("content row = %q, want the accent bar on its left", rows[3])
	}
	last := ansi.Strip(rows[len(rows)-1])
	if !strings.Contains(last, "─") || !strings.Contains(last, "r reply") {
		t.Errorf("closing row = %q, want a faint rule carrying the hints", last)
	}

	idle := ansi.Strip(lines(pane().Header(" octocat said").Size(30, 6).Render("body"))[3])
	if strings.HasPrefix(idle, "▎") {
		t.Errorf("an idle card drew the focus bar: %q", idle)
	}
}

func TestAHeaderIsSetOffByAFaintRule(t *testing.T) {
	out := lines(pane().Title("Diff").Header("internal/x.go").Size(30, 6).Render("a"))
	if got := ansi.Strip(out[1]); !strings.Contains(got, "internal/x.go") {
		t.Errorf("line 1 = %q, want the header", got)
	}
	if got := ansi.Strip(out[2]); strings.Trim(got, " ─") != "" || !strings.Contains(got, "─") {
		t.Errorf("line 2 = %q, want a faint rule", got)
	}
}

func TestABoxedPaneKeepsASquareFrame(t *testing.T) {
	out := ansi.Strip(pane().Title("Keys").Boxed(true).Size(20, 4).Render("x"))
	rows := lines(out)
	if !strings.HasPrefix(rows[0], "┌") || !strings.HasSuffix(rows[0], "┐") ||
		!strings.HasPrefix(rows[len(rows)-1], "└") || !strings.HasSuffix(rows[len(rows)-1], "┘") {
		t.Errorf("boxed pane = %q, want square corners", out)
	}
}

func TestClassicChromeIsZenOctosBoxInTheSameGeometry(t *testing.T) {
	gh := pane().Title("Files").Header("h").Size(30, 8)
	flat := gh.Render("a\nb")

	comp.ClassicChrome = true
	t.Cleanup(func() { comp.ClassicChrome = false })
	classic := gh.Render("a\nb")

	if !strings.HasPrefix(ansi.Strip(classic), "╭") {
		t.Errorf("classic chrome = %q, want zen-octo's rounded box", ansi.Strip(classic))
	}
	if len(lines(flat)) != len(lines(classic)) {
		t.Errorf("flat is %d rows, classic %d: the geometry moved", len(lines(flat)), len(lines(classic)))
	}
	for i := range lines(flat) {
		if a, b := ansi.StringWidth(lines(flat)[i]), ansi.StringWidth(lines(classic)[i]); a != b {
			t.Errorf("row %d is %d wide flat and %d classic", i, a, b)
		}
	}
	if gh.Above() != 3 {
		t.Errorf("Above() = %d, want 3 either way", gh.Above())
	}
}
