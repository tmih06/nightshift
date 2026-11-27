package accountsview

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// Every provider's logo is pixel art on one grid, drawn with half blocks so
// that a cell holds two square pixels.
const (
	logoWidth  = 12
	logoPixels = 10
	logoGap    = 3
	logoWaist  = "...######..."
)

// logoArt is a logo's pixels: each letter picks a color from its palette, and a
// dot is empty. A row palette, when set, colors '#' by the row it is on.
type logoArt struct {
	pixels  [logoPixels]string
	palette map[byte]string
	rows    []string
}

// logoOf is a provider's logo; ok is false for a provider with none.
func logoOf(provider string) (art logoArt, ok bool) {
	switch provider {
	case providerClaude:
		return claudeLogo(), true
	case providerCodex:
		return codexLogo(), true
	case providerAntigravity:
		return antigravityLogo(), true
	}
	return logoArt{}, false
}

// claudeLogo is Claude's orange spark.
func claudeLogo() logoArt {
	return logoArt{
		pixels: [logoPixels]string{
			".....##.....",
			".##..##..##.",
			"..##.##.##..",
			logoWaist,
			"############",
			"############",
			logoWaist,
			"..##.##.##..",
			".##..##..##.",
			".....##.....",
		},
		palette: map[byte]string{'#': "#d97757"},
	}
}

// codexLogo is Codex's cloud with its prompt, shaded top to bottom.
func codexLogo() logoArt {
	return logoArt{
		pixels: [logoPixels]string{
			"....####....",
			"..########..",
			".##########.",
			"###w########",
			"####w#######",
			"#####w######",
			"####w#######",
			"###w##wwww##",
			".##########.",
			logoWaist,
		},
		palette: map[byte]string{'w': "#ffffff"},
		rows: []string{
			"#b6a8ff", "#ab9fff", "#9d95ff", "#8c89ff", "#7b7cff",
			"#6a6eff", "#5a60fb", "#4b52f5", "#3f46ee", "#363de6",
		},
	}
}

// antigravityLogo is Antigravity's arch, one color to a pixel.
func antigravityLogo() logoArt {
	return logoArt{
		pixels: [logoPixels]string{
			".....ab.....",
			"....cdef....",
			"...ghijk....",
			"...lmnopq...",
			"..rstuvwxy..",
			"..zAB..CDE..",
			"..FG....HI..",
			".JKL....MNO.",
			".PQ......RS.",
			"TQ........UV",
		},
		palette: map[byte]string{
			'a': "#e98e2f", 'b': "#e76f36", 'c': "#d3ab32", 'd': "#ed8d2f", 'e': "#ea7037", 'f': "#e7573b",
			'g': "#98bc44", 'h': "#afae3e", 'i': "#da943d", 'j': "#ed7734", 'k': "#ef6835", 'l': "#81be4c",
			'm': "#71ad5b", 'n': "#c5914c", 'o': "#e67646", 'p': "#d96451", 'q': "#d84d57", 'r': "#78ba4f",
			's': "#6dba59", 't': "#59a38a", 'u': "#598cac", 'v': "#7e6fa9", 'w': "#706bbb", 'x': "#935aa2",
			'y': "#965891", 'z': "#7cbe52", 'A': "#52b17d", 'B': "#3f91d5", 'C': "#487adb", 'D': "#6c6ac6",
			'E': "#8a61ad", 'F': "#5dba78", 'G': "#42a7a4", 'H': "#487ce0", 'I': "#696fcf", 'J': "#69bd8e",
			'K': "#5eb2cb", 'L': "#44a1d2", 'M': "#3c84f0", 'N': "#487de6", 'O': "#6275d8", 'P': "#67be9c",
			'Q': "#60aeeb", 'R': "#3781f0", 'S': "#467dea", 'T': "#63b1e9", 'U': "#377eef", 'V': "#3c80f1",
		},
	}
}

// color is the hex color of the pixel at x, y, or "" when it is empty.
func (l *logoArt) color(x, y int) string {
	c := l.pixels[y][x]
	if c == '.' {
		return ""
	}
	if hex, ok := l.palette[c]; ok {
		return hex
	}
	return l.rows[y]
}

// drawLogo is a provider's logo, logoPixels/2 lines of logoWidth cells, or nil.
func drawLogo(provider string) []string {
	art, ok := logoOf(provider)
	if !ok {
		return nil
	}
	lines := make([]string, 0, logoPixels/2)
	for y := 0; y < logoPixels; y += 2 {
		var b strings.Builder
		for x := range logoWidth {
			b.WriteString(halfBlock(art.color(x, y), art.color(x, y+1)))
		}
		lines = append(lines, b.String())
	}
	return lines
}

// halfBlock is the cell that shows top over bottom.
func halfBlock(top, bottom string) string {
	s := lipgloss.NewStyle()
	switch {
	case top == "" && bottom == "":
		return " "
	case top == "":
		return s.Foreground(lipgloss.Color(bottom)).Render("▄")
	case bottom == "":
		return s.Foreground(lipgloss.Color(top)).Render("▀")
	}
	return s.Foreground(lipgloss.Color(top)).Background(lipgloss.Color(bottom)).Render("▀")
}

// besideLogo sets the provider's logo to the left of lines. When a line would
// not fit next to it, the logo goes above them, so no text is cut for it.
func besideLogo(provider string, lines []string, width int) []string {
	logo := drawLogo(provider)
	if logo == nil || width < logoWidth {
		return lines
	}
	for _, l := range lines {
		if logoWidth+logoGap+lipgloss.Width(l) > width {
			return slices.Concat(logo, []string{""}, lines)
		}
	}
	out := make([]string, 0, max(len(logo), len(lines)))
	for i := range max(len(logo), len(lines)) {
		left, right := strings.Repeat(" ", logoWidth), ""
		if i < len(logo) {
			left = logo[i]
		}
		if i < len(lines) {
			right = lines[i]
		}
		out = append(out, left+strings.Repeat(" ", logoGap)+right)
	}
	return out
}
