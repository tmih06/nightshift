package syntax

import (
	"hash/fnv"
	"image/color"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Token is one run of code sharing a color. Color is nil where the style sets none.
type Token struct {
	Text  string
	Color color.Color
}

// Syntax colors source code as tokens. Copies share a concurrent cache.
type Syntax struct {
	style *chroma.Style
	cache *sync.Map // uint64 → [][]Token
}

// New builds a colorizer over a Chroma style and reports whether Chroma knows the name; empty counts as known.
// An unknown name still yields a working colorizer with Chroma's fallback style.
func New(name string) (Syntax, bool) {
	_, ok := styles.Registry[name]
	return Syntax{style: styles.Get(name), cache: &sync.Map{}}, ok || name == ""
}

// Lines splits code into lines of colored tokens, choosing the lexer from path.
// Pass a whole file: lexers carry state across lines.
func (s *Syntax) Lines(path, code string) [][]Token {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	// The resolved lexer, not the path, determines the tokens. Identical
	// source in a moved, copied, or generated file can share its colors.
	h := fnv.New64a()
	_, _ = h.Write([]byte(lexer.Config().Name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(code))
	key := h.Sum64()

	if out, ok := s.cache.Load(key); ok {
		return out.([][]Token)
	}

	out := s.tokenise(lexer, code)
	stored, _ := s.cache.LoadOrStore(key, out)
	return stored.([][]Token)
}

func (s *Syntax) tokenise(lexer chroma.Lexer, code string) [][]Token {
	iter, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return plain(code)
	}

	var out [][]Token
	for _, line := range chroma.SplitTokensIntoLines(iter.Tokens()) {
		row := make([]Token, 0, len(line))
		for _, t := range line {
			text := strings.TrimSuffix(t.Value, "\n")
			if text == "" {
				continue
			}
			row = append(row, Token{Text: text, Color: colorOf(s.style.Get(t.Type).Colour)})
		}
		out = append(out, row)
	}
	return out
}

func plain(code string) [][]Token {
	lines := strings.Split(code, "\n")
	out := make([][]Token, len(lines))
	for i, line := range lines {
		out[i] = []Token{{Text: line}}
	}
	return out
}

// Foreground only: a style's own background would paint over the terminal's.
func colorOf(c chroma.Colour) color.Color {
	if !c.IsSet() {
		return nil
	}
	return lipgloss.Color(c.String())
}
