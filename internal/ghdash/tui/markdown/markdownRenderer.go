package markdown

import (
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	log "charm.land/log/v2"

	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
)

type rendererKey struct {
	style *ansi.StyleConfig
	width int
}

type renderCacheKey struct {
	style *ansi.StyleConfig
	width int
	body  string
}

const (
	maxCachedRenderers = 8
	maxCachedBodies    = 256
)

var (
	markdownMu          sync.Mutex
	markdownStyle       *ansi.StyleConfig
	markdownStyleSource string
	rendererCache       = map[rendererKey]*glamour.TermRenderer{}
	bodyCache           = map[renderCacheKey]string{}
)

func InitializeMarkdownStyle(ctx *context.ProgramContext) {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	initMarkdownStyleLocked(ctx)
}

func initMarkdownStyleLocked(ctx *context.ProgramContext) {
	if markdownStyle != nil && markdownStyleSource == "bubbletea" {
		log.Debugf("InitializeMarkdownStyle: keeping existing bubbletea style")
		return
	}

	hasDarkBackground := true
	backgroundSource := "default"
	if ctx != nil {
		hasDarkBackground = ctx.HasDarkBackground
		backgroundSource = ctx.BackgroundSource
	}

	var nextStyle *ansi.StyleConfig
	if hasDarkBackground {
		nextStyle = &CustomDarkStyleConfig
	} else {
		nextStyle = &styles.LightStyleConfig
	}
	if nextStyle != markdownStyle {
		clear(rendererCache)
		clear(bodyCache)
	}
	markdownStyle = nextStyle
	markdownStyleSource = backgroundSource

	log.Debugf(
		"InitializeMarkdownStyle: assigned ctx.hasDarkBackground=%t, markdownStyleSource=%q",
		hasDarkBackground,
		markdownStyleSource,
	)
}

func getRendererLocked(width int, ctx *context.ProgramContext) *glamour.TermRenderer {
	if markdownStyle == nil {
		initMarkdownStyleLocked(ctx)
	}
	key := rendererKey{style: markdownStyle, width: width}
	if r, ok := rendererCache[key]; ok {
		return r
	}

	markdownRenderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(*markdownStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil || markdownRenderer == nil {
		// Return a fallback renderer that just returns input unchanged
		fallback, _ := glamour.NewTermRenderer()
		if fallback != nil {
			return fallback
		}
		// If even fallback fails, panic with helpful message
		msg := "unknown error"
		if err != nil {
			msg = err.Error()
		}
		panic("failed to create markdown renderer: " + msg)
	}

	if len(rendererCache) >= maxCachedRenderers {
		clear(rendererCache)
	}
	rendererCache[key] = markdownRenderer
	return markdownRenderer
}

// Render renders markdown at width using a cached TermRenderer and body cache.
func Render(body string, width int, ctx *context.ProgramContext) (string, error) {
	markdownMu.Lock()
	defer markdownMu.Unlock()

	if markdownStyle == nil {
		initMarkdownStyleLocked(ctx)
	}
	key := renderCacheKey{style: markdownStyle, width: width, body: body}
	if rendered, ok := bodyCache[key]; ok {
		return rendered, nil
	}

	r := getRendererLocked(width, ctx)
	rendered, err := r.Render(body)
	if err != nil {
		return "", err
	}
	if len(bodyCache) >= maxCachedBodies {
		clear(bodyCache)
	}
	bodyCache[key] = rendered
	return rendered, nil
}

func GetMarkdownRenderer(width int, ctx *context.ProgramContext) glamour.TermRenderer {
	markdownMu.Lock()
	defer markdownMu.Unlock()
	return *getRendererLocked(width, ctx)
}
