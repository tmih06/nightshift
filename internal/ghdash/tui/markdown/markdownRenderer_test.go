package markdown

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetMarkdownRendererNilContext(t *testing.T) {
	markdownStyle = nil
	markdownStyleSource = ""

	require.NotPanics(t, func() {
		renderer := GetMarkdownRenderer(80, nil)
		rendered, err := renderer.Render("# hello")
		require.NoError(t, err)
		require.NotEmpty(t, rendered)
	})
}

func TestInitializeMarkdownStyleNilContext(t *testing.T) {
	markdownStyle = nil
	markdownStyleSource = ""

	require.NotPanics(t, func() {
		InitializeMarkdownStyle(nil)
	})

	require.NotNil(t, markdownStyle)
}

func TestRenderCachesAndInvalidatesOnStyleChange(t *testing.T) {
	markdownMu.Lock()
	markdownStyle = nil
	markdownStyleSource = ""
	clear(rendererCache)
	clear(bodyCache)
	markdownMu.Unlock()

	darkOut1, err := Render("## Heading\n\nSome **bold** text", 60, nil)
	require.NoError(t, err)
	darkOut2, err := Render("## Heading\n\nSome **bold** text", 60, nil)
	require.NoError(t, err)
	require.Equal(t, darkOut1, darkOut2)

	markdownMu.Lock()
	require.Len(t, bodyCache, 1)
	require.Len(t, rendererCache, 1)
	markdownMu.Unlock()
}
