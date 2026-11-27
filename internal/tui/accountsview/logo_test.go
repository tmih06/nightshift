package accountsview

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestEveryProviderLogoIsTheSameSize(t *testing.T) {
	for _, id := range []string{providerClaude, providerCodex, providerAntigravity} {
		logo := drawLogo(id)
		art, ok := logoOf(id)
		if !ok {
			t.Errorf("%s has no logo", id)
			continue
		}
		if len(logo) != logoPixels/2 {
			t.Errorf("%s logo is %d lines, want %d", id, len(logo), logoPixels/2)
		}
		for i, l := range logo {
			if w := lipgloss.Width(l); w != logoWidth {
				t.Errorf("%s logo line %d is %d cells, want %d", id, i, w, logoWidth)
			}
		}
		for y, row := range art.pixels {
			if len(row) != logoWidth {
				t.Errorf("%s pixel row %d is %d wide, want %d", id, y, len(row), logoWidth)
				continue
			}
			for x := range row {
				if row[x] != '.' && art.color(x, y) == "" {
					t.Errorf("%s pixel %d,%d has no color", id, x, y)
				}
			}
		}
	}
}

func TestLogoGoesAboveTextItWouldCut(t *testing.T) {
	long := strings.Repeat("x", 40)
	beside := besideLogo(providerClaude, []string{long}, 80)
	if len(beside) != logoPixels/2 || !strings.HasSuffix(beside[0], long) {
		t.Errorf("wide pane: want the text beside the logo, got %q", beside)
	}
	above := besideLogo(providerClaude, []string{long}, 50)
	if len(above) != logoPixels/2+2 || above[len(above)-1] != long {
		t.Errorf("narrow pane: want the text whole under the logo, got %q", above)
	}
}
