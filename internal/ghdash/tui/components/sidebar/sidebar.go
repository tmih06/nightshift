package sidebar

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

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
	lines      []string
	yOffset    int
	vpWidth    int
	vpHeight   int
	ctx        *context.ProgramContext
	emptyState string
	cache      *viewCache
}

func NewModel() Model {
	return Model{
		IsOpen:     false,
		data:       "",
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

func (m *Model) maxYOffset() int {
	return max(0, len(m.lines)-m.vpHeight)
}

func (m *Model) setYOffset(y int) {
	y = max(0, min(y, m.maxYOffset()))
	if m.yOffset != y {
		m.yOffset = y
		m.invalidateCache()
	}
}

func (m Model) scrollPercent() float64 {
	if m.vpHeight >= len(m.lines) {
		return 1.0
	}
	y := float64(m.yOffset)
	h := float64(m.vpHeight)
	t := float64(len(m.lines))
	v := y / (t - h)
	return max(0.0, min(1.0, v))
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Keys.PageDown):
			m.setYOffset(m.yOffset + max(1, m.vpHeight/2))

		case key.Matches(msg, keys.Keys.PageUp):
			m.setYOffset(m.yOffset - max(1, m.vpHeight/2))
		}
	}

	return m, nil
}

func (m Model) renderViewportLines(vpW, vpH int) []string {
	if vpH <= 0 {
		return nil
	}
	out := make([]string, vpH)
	yOff := max(0, min(m.yOffset, max(0, len(m.lines)-vpH)))
	end := min(len(m.lines), yOff+vpH)
	for i := yOff; i < end; i++ {
		line := m.lines[i]
		// Only measure/truncate when byte length could exceed vpW.
		if vpW > 0 && len(line) > vpW && ansi.StringWidth(line) > vpW {
			line = ansi.Truncate(line, vpW, "")
		}
		out[i-yOff] = line
	}
	return out
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
	yOff := m.yOffset
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
			vpW := m.GetSidebarContentWidth()
			vpH := max(0, height-m.ctx.Styles.Sidebar.PagerHeight)
			vpLines := m.renderViewportLines(vpW, vpH)
			pager := m.ctx.Styles.Sidebar.PagerStyle.
				Render(fmt.Sprintf("%d%%", int(m.scrollPercent()*100)))
			rendered = style.Render(strings.Join(vpLines, "\n") + "\n" + pager)
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
				vpW := m.GetSidebarContentWidth()
				vpH := max(0, height-m.ctx.Styles.Sidebar.PagerHeight)
				vpLines := m.renderViewportLines(vpW, vpH)
				pager := m.ctx.Styles.Sidebar.PagerStyle.
					Render(fmt.Sprintf("%d%%", int(m.scrollPercent()*100)))
				rendered = style.Render(strings.Join(vpLines, "\n") + "\n" + pager)
			}
		} else {
			vpW := m.GetSidebarContentWidth()
			vpH := height - m.ctx.Styles.Sidebar.PagerHeight
			vpLines := m.renderViewportLines(vpW, vpH)
			pager := m.ctx.Styles.Sidebar.PagerStyle.
				Width(vpW).
				MaxWidth(vpW).
				Render(fmt.Sprintf("%d%%", int(m.scrollPercent()*100)))
			borderLeft := lipgloss.NewStyle().
				Foreground(m.ctx.Theme.PrimaryBorder).
				Render("│")
			var b strings.Builder
			b.Grow(height * (len(borderLeft) + vpW + 8))
			for _, line := range vpLines {
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
	if data == "" {
		m.lines = nil
	} else {
		m.lines = strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	}
	if m.yOffset > m.maxYOffset() {
		m.yOffset = m.maxYOffset()
	}
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
	m.setYOffset(0)
}

func (m *Model) ScrollToBottom() {
	m.setYOffset(m.maxYOffset())
}

func (m *Model) ScrollUp(n int) {
	m.setYOffset(m.yOffset - n)
}

func (m *Model) ScrollDown(n int) {
	m.setYOffset(m.yOffset + n)
}

func (m *Model) YOffset() int {
	return m.yOffset
}

func (m *Model) ScrollToPercent(percent float64) {
	targetLine := int(float64(len(m.lines)) * percent)
	m.setYOffset(targetLine)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	if ctx == nil {
		return
	}
	m.ctx = ctx
	if m.ctx.PreviewPosition == "bottom" {
		m.vpHeight = max(0, m.ctx.DynamicPreviewHeight-m.ctx.Styles.Sidebar.PagerHeight)
	} else {
		m.vpHeight = max(0, m.ctx.MainContentHeight-m.ctx.Styles.Sidebar.PagerHeight)
	}
	m.vpWidth = m.GetSidebarContentWidth()
	if m.yOffset > m.maxYOffset() {
		m.yOffset = m.maxYOffset()
	}
	m.invalidateCache()
}
