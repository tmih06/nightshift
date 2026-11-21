package sidebar

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/tui/keys"
)

type viewCache struct {
	valid    bool
	data     string
	yOffset  int
	width    int
	height   int
	position string
	rendered string
}

type Model struct {
	IsOpen     bool
	data       string
	viewport   viewport.Model
	ctx        *context.ProgramContext
	emptyState string
	cache      *viewCache
}

func NewModel() Model {
	vp := viewport.New(
		viewport.WithWidth(0),
		viewport.WithHeight(0),
	)

	return Model{
		IsOpen:     false,
		data:       "",
		viewport:   vp,
		ctx:        nil,
		emptyState: "Nothing selected...",
		cache:      &viewCache{},
	}
}

func (m *Model) invalidateCache() {
	if m.cache != nil {
		m.cache.valid = false
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Keys.PageDown):
			m.viewport.HalfPageDown()
			m.invalidateCache()

		case key.Matches(msg, keys.Keys.PageUp):
			m.viewport.HalfPageUp()
			m.invalidateCache()
		}
	}

	return m, nil
}

func (m Model) View() string {
	if !m.IsOpen {
		return ""
	}

	pos := m.ctx.PreviewPosition
	width := m.ctx.DynamicPreviewWidth
	height := m.ctx.MainContentHeight
	if pos == "bottom" {
		height = m.ctx.DynamicPreviewHeight
	}
	yOff := m.viewport.YOffset()
	if m.cache != nil && m.cache.valid &&
		m.cache.data == m.data &&
		m.cache.yOffset == yOff &&
		m.cache.width == width &&
		m.cache.height == height &&
		m.cache.position == pos {
		return m.cache.rendered
	}

	var rendered string
	if pos == "bottom" {
		style := m.ctx.Styles.Sidebar.BottomRoot.
			Height(height).
			Width(width)

		if m.data == "" {
			rendered = style.Align(lipgloss.Center).Render(
				lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
			)
		} else {
			rendered = style.Render(lipgloss.JoinVertical(
				lipgloss.Top,
				m.viewport.View(),
				m.ctx.Styles.Sidebar.PagerStyle.
					Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
			))
		}
	} else {
		// Right mode
		if m.data == "" || height <= m.ctx.Styles.Sidebar.PagerHeight {
			style := m.ctx.Styles.Sidebar.Root.
				Height(height).
				Width(width)
			if m.data == "" {
				rendered = style.Align(lipgloss.Center).Render(
					lipgloss.PlaceVertical(height, lipgloss.Center, m.emptyState),
				)
			} else {
				rendered = style.Render(lipgloss.JoinVertical(
					lipgloss.Top,
					m.viewport.View(),
					m.ctx.Styles.Sidebar.PagerStyle.
						Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100))),
				))
			}
		} else {
			vpView := m.viewport.View()
			vpW := m.GetSidebarContentWidth()
			pager := m.ctx.Styles.Sidebar.PagerStyle.
				Width(vpW).
				MaxWidth(vpW).
				Render(fmt.Sprintf("%d%%", int(m.viewport.ScrollPercent()*100)))
			borderLeft := lipgloss.NewStyle().
				Foreground(m.ctx.Theme.PrimaryBorder).
				Render("│")
			var b strings.Builder
			b.Grow(len(vpView) + len(pager) + height*(len(borderLeft)+2))
			start := 0
			for start <= len(vpView) {
				nl := strings.IndexByte(vpView[start:], '\n')
				var line string
				if nl < 0 {
					line = vpView[start:]
					start = len(vpView) + 1
				} else {
					line = vpView[start : start+nl]
					start += nl + 1
				}
				b.WriteString(borderLeft)
				b.WriteString(line)
				b.WriteByte('\n')
			}
			b.WriteString(borderLeft)
			b.WriteString(pager)
			rendered = b.String()
		}
	}

	if m.cache != nil {
		m.cache.valid = true
		m.cache.data = m.data
		m.cache.yOffset = yOff
		m.cache.width = width
		m.cache.height = height
		m.cache.position = pos
		m.cache.rendered = rendered
	}
	return rendered
}

func (m *Model) SetContent(data string) {
	if m.data == data {
		return
	}
	m.data = data
	m.viewport.SetContent(data)
	m.invalidateCache()
}

func (m *Model) GetSidebarContentWidth() int {
	if m.ctx == nil || m.ctx.Config == nil {
		return 0
	}
	if m.ctx.PreviewPosition == "bottom" {
		return max(0, m.ctx.DynamicPreviewWidth)
	}
	return max(0, m.ctx.DynamicPreviewWidth-m.ctx.Styles.Sidebar.BorderWidth)
}

func (m *Model) ScrollToTop() {
	m.viewport.GotoTop()
	m.invalidateCache()
}

func (m *Model) ScrollToBottom() {
	m.viewport.GotoBottom()
	m.invalidateCache()
}

func (m *Model) ScrollUp(n int) {
	m.viewport.ScrollUp(n)
	m.invalidateCache()
}

func (m *Model) ScrollDown(n int) {
	m.viewport.ScrollDown(n)
	m.invalidateCache()
}

func (m *Model) YOffset() int {
	return m.viewport.YOffset()
}

func (m *Model) ScrollToPercent(percent float64) {
	totalLines := m.viewport.TotalLineCount()
	targetLine := int(float64(totalLines) * percent)
	m.viewport.SetYOffset(targetLine)
	m.invalidateCache()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	if m.ctx.PreviewPosition == "bottom" {
		m.viewport.SetHeight(m.ctx.DynamicPreviewHeight - m.ctx.Styles.Sidebar.PagerHeight)
	} else {
		m.viewport.SetHeight(m.ctx.MainContentHeight - m.ctx.Styles.Sidebar.PagerHeight)
	}
	m.viewport.SetWidth(m.GetSidebarContentWidth())
	m.invalidateCache()
}
