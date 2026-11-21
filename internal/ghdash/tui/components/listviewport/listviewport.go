package listviewport

import (
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/tui/constants"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
	"github.com/tmih06/nightshift/internal/ghdash/utils"
)

type Model struct {
	ctx             context.ProgramContext
	viewport        viewport.Model
	topBoundId      int
	bottomBoundId   int
	currId          int
	yOffset         int
	width           int
	height          int
	ListItemHeight  int
	NumCurrentItems int
	NumTotalItems   int
	LastUpdated     time.Time
	CreatedAt       time.Time
	ItemTypeLabel   string
}

func NewModel(
	ctx context.ProgramContext,
	dimensions constants.Dimensions,
	lastUpdated time.Time,
	createdAt time.Time,
	itemTypeLabel string,
	numItems, listItemHeight int,
) Model {
	w := max(0, dimensions.Width)
	h := max(0, dimensions.Height)
	model := Model{
		ctx:             ctx,
		NumCurrentItems: numItems,
		ListItemHeight:  listItemHeight,
		currId:          0,
		width:           w,
		height:          h,
		viewport: viewport.New(
			viewport.WithWidth(w),
			viewport.WithHeight(h),
		),
		topBoundId:    0,
		ItemTypeLabel: itemTypeLabel,
		LastUpdated:   lastUpdated,
		CreatedAt:     createdAt,
	}
	model.bottomBoundId = utils.Min(
		model.NumCurrentItems-1,
		model.getNumPrsPerPage()-1,
	)
	return model
}

func (m *Model) maxYOffset() int {
	return max(0, m.NumCurrentItems*m.ListItemHeight-m.height)
}

func (m *Model) setYOffset(y int) {
	y = max(0, min(y, m.maxYOffset()))
	m.yOffset = y
	m.viewport.SetYOffset(y)
}

func (m *Model) YOffset() int {
	return m.yOffset
}

func (m *Model) Width() int {
	return m.width
}

func (m *Model) Height() int {
	return m.height
}

func (m *Model) SetNumItems(numItems int) {
	m.NumCurrentItems = numItems
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.getNumPrsPerPage()-1)
	if m.yOffset > m.maxYOffset() {
		m.setYOffset(m.maxYOffset())
	}
}

func (m *Model) SetTotalItems(total int) {
	m.NumTotalItems = total
}

func (m *Model) SetItemHeight(height int) {
	m.ListItemHeight = height
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.getNumPrsPerPage()-1)
	if m.yOffset > m.maxYOffset() {
		m.setYOffset(m.maxYOffset())
	}
}

func (m *Model) SyncViewPort(content string) {
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(m.yOffset)
}

func (m *Model) getNumPrsPerPage() int {
	if m.ListItemHeight == 0 {
		return 0
	}
	return m.height / m.ListItemHeight
}

func (m *Model) ResetCurrItem() {
	m.currId = 0
	m.topBoundId = 0
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.getNumPrsPerPage()-1)
	m.setYOffset(0)
}

func (m *Model) GetCurrItem() int {
	return m.currId
}

// RowAt maps a viewport-relative row relY to the item index drawn there.
// Clicks on the separator row between items or beyond the last item miss.
func (m *Model) RowAt(relY int) (int, bool) {
	if relY < 0 || relY >= m.height || m.ListItemHeight <= 0 || m.NumCurrentItems <= 0 {
		return 0, false
	}
	line := m.yOffset + relY
	idx := line / m.ListItemHeight
	if idx < 0 || idx >= m.NumCurrentItems {
		return 0, false
	}
	if m.ctx.Config != nil && m.ctx.Config.Theme.Ui.Table.ShowSeparator &&
		m.ListItemHeight > 1 && line%m.ListItemHeight == m.ListItemHeight-1 {
		return 0, false
	}
	return idx, true
}

// SetCurrItem selects item idx and keeps it visible inside the viewport.
func (m *Model) SetCurrItem(idx int) int {
	if m.NumCurrentItems <= 0 {
		m.currId = 0
		return 0
	}
	idx = utils.Max(0, utils.Min(idx, m.NumCurrentItems-1))
	m.currId = idx
	if m.ListItemHeight > 0 {
		perPage := utils.Max(1, m.getNumPrsPerPage())
		topIdx := m.yOffset / m.ListItemHeight
		if idx < topIdx {
			topIdx = idx
			m.setYOffset(topIdx * m.ListItemHeight)
		} else if idx >= topIdx+perPage {
			topIdx = idx - perPage + 1
			m.setYOffset(topIdx * m.ListItemHeight)
		}
		m.topBoundId = topIdx
		m.bottomBoundId = utils.Min(m.NumCurrentItems-1, topIdx+perPage-1)
	}
	return m.currId
}

func (m *Model) NextItem() int {
	atBottomOfViewport := m.currId >= m.bottomBoundId
	if atBottomOfViewport && m.currId < m.NumCurrentItems-1 {
		m.topBoundId += 1
		m.bottomBoundId += 1
		m.setYOffset(m.yOffset + m.ListItemHeight)
	}

	newId := utils.Min(m.currId+1, m.NumCurrentItems-1)
	newId = utils.Max(newId, 0)
	m.currId = newId
	return m.currId
}

func (m *Model) PrevItem() int {
	if m.currId > 0 && m.currId <= m.topBoundId {
		m.topBoundId -= 1
		m.bottomBoundId -= 1
		m.setYOffset(m.yOffset - m.ListItemHeight)
	}

	m.currId = utils.Max(m.currId-1, 0)
	return m.currId
}

func (m *Model) FirstItem() int {
	m.currId = 0
	m.topBoundId = 0
	m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.getNumPrsPerPage()-1)
	m.setYOffset(0)
	return m.currId
}

func (m *Model) LastItem() int {
	if m.NumCurrentItems <= 0 {
		m.currId = 0
		m.setYOffset(0)
		return 0
	}
	m.currId = m.NumCurrentItems - 1
	m.setYOffset(m.maxYOffset())
	if m.ListItemHeight > 0 {
		perPage := utils.Max(1, m.getNumPrsPerPage())
		m.topBoundId = m.yOffset / m.ListItemHeight
		m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.topBoundId+perPage-1)
	}
	return m.currId
}

func (m *Model) SetDimensions(dimensions constants.Dimensions) {
	m.height = max(0, dimensions.Height)
	m.width = max(0, dimensions.Width)
	m.viewport.SetHeight(m.height)
	m.viewport.SetWidth(m.width)
	if m.yOffset > m.maxYOffset() {
		m.setYOffset(m.maxYOffset())
	}
	if m.ListItemHeight > 0 {
		perPage := utils.Max(1, m.getNumPrsPerPage())
		m.topBoundId = m.yOffset / m.ListItemHeight
		m.bottomBoundId = utils.Min(m.NumCurrentItems-1, m.topBoundId+perPage-1)
	}
}

func (m *Model) View() string {
	viewport := m.viewport.View()
	return lipgloss.NewStyle().
		Width(m.width).
		MaxWidth(m.width).
		Render(
			viewport,
		)
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	m.ctx = *ctx
}
