package table

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/listviewport"
	"github.com/tmih06/nightshift/internal/ghdash/tui/constants"
	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
)

type renderCache struct {
	layoutValid   bool
	layoutWidth   int
	colWidths     []int
	totalColWidth int
	header        string
	separator     string
	rows          []string
	selected      int
	bodyValid     bool
	body          string
	bodyYOffset   int
	bodyWidth     int
	bodyHeight    int
	bodyCurr      int
	bodyRowCount  int
	lastCompact   bool
	lastSeparator bool
	lastTheme     bool
}

type Model struct {
	ctx            context.ProgramContext
	Columns        []Column
	Rows           []Row
	EmptyState     *string
	loadingMessage string
	isLoading      bool
	loadingSpinner spinner.Model
	dimensions     constants.Dimensions
	rowsViewport   listviewport.Model
	ContentHeight  int // Optional: override content height (0 = use default from config)
	cache          *renderCache
}

type Column struct {
	Title         string
	Hidden        *bool
	Width         *int
	ComputedWidth int
	Grow          *bool
	Align         *lipgloss.Position // Optional alignment (defaults to left)
}

type Row []string

func NewModel(
	ctx context.ProgramContext,
	dimensions constants.Dimensions,
	lastUpdated time.Time,
	createdAt time.Time,
	columns []Column,
	rows []Row,
	itemTypeLabel string,
	emptyState *string,
	loadingMessage string,
	isLoading bool,
) Model {
	itemHeight := 1
	if !ctx.Config.Theme.Ui.Table.Compact {
		itemHeight += 1
	}
	if ctx.Config.Theme.Ui.Table.ShowSeparator {
		itemHeight += 1
	}

	loadingSpinner := spinner.New()
	loadingSpinner.Spinner = spinner.Dot
	loadingSpinner.Style = lipgloss.NewStyle().Foreground(ctx.Theme.SecondaryText)

	return Model{
		ctx:            ctx,
		Columns:        columns,
		Rows:           rows,
		EmptyState:     emptyState,
		loadingMessage: loadingMessage,
		isLoading:      isLoading,
		loadingSpinner: loadingSpinner,
		dimensions:     dimensions,
		rowsViewport: listviewport.NewModel(
			ctx,
			dimensions,
			lastUpdated,
			createdAt,
			itemTypeLabel,
			len(rows),
			itemHeight,
		),
		cache: &renderCache{
			selected:      -1,
			lastCompact:   ctx.Config.Theme.Ui.Table.Compact,
			lastSeparator: ctx.Config.Theme.Ui.Table.ShowSeparator,
		},
	}
}

func (m *Model) ensureCache() *renderCache {
	if m.cache == nil {
		m.cache = &renderCache{selected: -1}
	}
	return m.cache
}

func (m *Model) invalidateRows() {
	c := m.ensureCache()
	c.rows = nil
	c.selected = -1
	c.bodyValid = false
}

func (m *Model) invalidateSelection(prev, next int) {
	c := m.ensureCache()
	c.bodyValid = false
	if prev == next {
		return
	}
	if prev >= 0 && prev < len(c.rows) {
		c.rows[prev] = ""
	}
	if next >= 0 && next < len(c.rows) {
		c.rows[next] = ""
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.isLoading {
		m.loadingSpinner, cmd = m.loadingSpinner.Update(msg)
	}
	return m, cmd
}

func (m Model) StartLoadingSpinner() tea.Cmd {
	return m.loadingSpinner.Tick
}

func (m Model) View() string {
	header := m.renderHeader()
	body := m.renderBody()

	return header + "\n" + body
}

func (m *Model) SetIsLoading(isLoading bool) {
	m.isLoading = isLoading
}

func (m *Model) SetDimensions(dimensions constants.Dimensions) {
	if m.dimensions == dimensions && m.ensureCache().layoutValid {
		return
	}
	m.dimensions = dimensions
	m.rowsViewport.SetDimensions(constants.Dimensions{
		Width:  m.dimensions.Width,
		Height: m.dimensions.Height,
	})
	c := m.ensureCache()
	c.layoutValid = false
	m.invalidateRows()
	m.EnsureLayout()
}

func (m *Model) ResetCurrItem() {
	prev := m.rowsViewport.GetCurrItem()
	m.rowsViewport.ResetCurrItem()
	m.invalidateSelection(prev, m.rowsViewport.GetCurrItem())
}

func (m *Model) GetCurrItem() int {
	return m.rowsViewport.GetCurrItem()
}

func (m *Model) RowAt(relY int) (int, bool) {
	if m.isLoading || len(m.Rows) == 0 {
		return 0, false
	}
	return m.rowsViewport.RowAt(relY)
}

func (m *Model) SetCurrItem(idx int) int {
	prev := m.rowsViewport.GetCurrItem()
	currItem := m.rowsViewport.SetCurrItem(idx)
	m.invalidateSelection(prev, currItem)
	return currItem
}

func (m *Model) PrevItem() int {
	prev := m.rowsViewport.GetCurrItem()
	currItem := m.rowsViewport.PrevItem()
	m.invalidateSelection(prev, currItem)
	return currItem
}

func (m *Model) NextItem() int {
	prev := m.rowsViewport.GetCurrItem()
	currItem := m.rowsViewport.NextItem()
	m.invalidateSelection(prev, currItem)
	return currItem
}

func (m *Model) FirstItem() int {
	prev := m.rowsViewport.GetCurrItem()
	currItem := m.rowsViewport.FirstItem()
	m.invalidateSelection(prev, currItem)
	return currItem
}

func (m *Model) LastItem() int {
	prev := m.rowsViewport.GetCurrItem()
	currItem := m.rowsViewport.LastItem()
	m.invalidateSelection(prev, currItem)
	return currItem
}

// EnsureLayout computes and caches visible column widths and the rendered header.
func (m *Model) EnsureLayout() {
	c := m.ensureCache()
	if c.layoutValid && c.layoutWidth == m.dimensions.Width {
		return
	}
	headerColumns := m.renderHeaderColumns()
	colWidths := make([]int, len(headerColumns))
	totalColWidth := 0
	headerColId := 0
	for i := range m.Columns {
		if m.Columns[i].Hidden != nil && *m.Columns[i].Hidden {
			continue
		}
		if headerColId < len(headerColumns) {
			w := lipgloss.Width(headerColumns[headerColId])
			colWidths[headerColId] = w
			totalColWidth += w
			m.Columns[i].ComputedWidth = w
			headerColId++
		}
	}
	header := ansi.Truncate(
		lipgloss.JoinHorizontal(lipgloss.Top, headerColumns...),
		m.dimensions.Width,
		constants.Ellipsis,
	)
	c.header = m.ctx.Styles.Table.HeaderStyle.
		Width(m.dimensions.Width).
		Height(common.TableHeaderHeight).
		Render(header)
	c.separator = lipgloss.NewStyle().
		Foreground(m.ctx.Theme.FaintBorder).
		Render(strings.Repeat(lipgloss.NormalBorder().Bottom, max(0, m.dimensions.Width)))
	c.colWidths = colWidths
	c.totalColWidth = totalColWidth
	c.layoutWidth = m.dimensions.Width
	c.layoutValid = true
}

func (m *Model) SyncViewPortContent() {
	c := m.ensureCache()
	c.layoutValid = false
	m.invalidateRows()
	m.EnsureLayout()
}

func (m *Model) SetRows(rows []Row) {
	m.Rows = rows
	m.rowsViewport.SetNumItems(len(m.Rows))
	m.EnsureLayout()
	m.invalidateRows()
}

// UpdateRow replaces a single row and invalidates only its cached render.
func (m *Model) UpdateRow(idx int, row Row) {
	if idx < 0 || idx >= len(m.Rows) {
		return
	}
	m.Rows[idx] = row
	c := m.ensureCache()
	if idx < len(c.rows) {
		c.rows[idx] = ""
	}
	c.bodyValid = false
}

// SetContentHeight sets a custom content height for rows (use 0 to use default config-based height)
func (m *Model) SetContentHeight(height int) {
	m.ContentHeight = height
	// Recalculate item height for viewport
	itemHeight := height
	if m.ctx.Config.Theme.Ui.Table.ShowSeparator {
		itemHeight += 1
	}
	m.rowsViewport.SetItemHeight(itemHeight)
}

func (m *Model) OnLineDown() {
	m.rowsViewport.NextItem()
}

func (m *Model) OnLineUp() {
	m.rowsViewport.PrevItem()
}

func (m *Model) getShownColumns() []Column {
	shownColumns := make([]Column, 0, len(m.Columns))
	for _, col := range m.Columns {
		if col.Hidden != nil && *col.Hidden {
			continue
		}

		shownColumns = append(shownColumns, col)
	}
	return shownColumns
}

func (m *Model) renderHeaderColumns() []string {
	shownColumns := m.getShownColumns()
	renderedColumns := make([]string, len(shownColumns))
	takenWidth := 0
	numGrowingColumns := 0
	for i, column := range shownColumns {
		if column.Grow != nil && *column.Grow {
			numGrowingColumns += 1
			continue
		}

		if column.Width != nil {
			title := column.Title
			style := m.ctx.Styles.Table.TitleCellStyle
			// Align headers for right-aligned columns
			// Use padding-free style to avoid truncation from PlaceHorizontal + padding
			if column.Align != nil && *column.Align == lipgloss.Right {
				// Center short headers (icons), right-align longer headers (text)
				headerAlign := lipgloss.Right
				if lipgloss.Width(title) <= 2 {
					headerAlign = lipgloss.Center
				}
				title = lipgloss.PlaceHorizontal(*column.Width, headerAlign, title)
				style = lipgloss.NewStyle().Bold(true).Foreground(m.ctx.Theme.PrimaryText)
			}
			renderedColumns[i] = style.
				Width(*column.Width).
				MaxWidth(*column.Width).
				Render(title)
			takenWidth += *column.Width
			continue
		}

		cell := m.ctx.Styles.Table.TitleCellStyle.Render(column.Title)
		renderedColumns[i] = cell
		takenWidth += lipgloss.Width(cell)
	}

	if numGrowingColumns == 0 {
		return renderedColumns
	}

	leftoverWidth := m.dimensions.Width - takenWidth
	growCellWidth := leftoverWidth / numGrowingColumns
	for i, column := range shownColumns {
		if column.Grow == nil || !*column.Grow {
			continue
		}

		renderedColumns[i] = m.ctx.Styles.Table.TitleCellStyle.
			Width(growCellWidth).
			MaxWidth(growCellWidth).
			Render(column.Title)
	}

	return renderedColumns
}

func (m *Model) renderHeader() string {
	m.EnsureLayout()
	return m.ensureCache().header
}

func (m *Model) renderBody() string {
	if m.isLoading {
		return lipgloss.Place(
			m.dimensions.Width,
			m.dimensions.Height,
			lipgloss.Center,
			lipgloss.Center,
			fmt.Sprintf("%s%s", m.loadingSpinner.View(), m.loadingMessage),
		)
	}

	if len(m.Rows) == 0 && m.EmptyState != nil {
		bodyStyle := lipgloss.NewStyle().
			Height(m.dimensions.Height).
			Width(m.dimensions.Width)
		return bodyStyle.Render(*m.EmptyState)
	}

	viewH := m.dimensions.Height
	viewW := m.dimensions.Width
	if viewH <= 0 || viewW <= 0 {
		return ""
	}

	m.EnsureLayout()
	c := m.ensureCache()
	yOff := m.rowsViewport.YOffset()
	currItem := m.rowsViewport.GetCurrItem()
	if c.bodyValid &&
		c.bodyYOffset == yOff &&
		c.bodyWidth == viewW &&
		c.bodyHeight == viewH &&
		c.bodyCurr == currItem &&
		c.bodyRowCount == len(m.Rows) {
		return c.body
	}

	if len(c.rows) != len(m.Rows) {
		c.rows = make([]string, len(m.Rows))
		c.selected = -1
	}
	if c.selected != currItem {
		if c.selected >= 0 && c.selected < len(c.rows) {
			c.rows[c.selected] = ""
		}
		if currItem >= 0 && currItem < len(c.rows) {
			c.rows[currItem] = ""
		}
		c.selected = currItem
	}

	itemH := max(1, m.rowsViewport.ListItemHeight)
	firstRow := yOff / itemH
	lastRow := min(len(m.Rows)-1, (yOff+viewH-1)/itemH)

	var out strings.Builder
	linesWritten := 0
	skipLines := yOff - firstRow*itemH
	blankLine := strings.Repeat(" ", viewW)

	for i := firstRow; i <= lastRow && linesWritten < viewH; i++ {
		if c.rows[i] == "" {
			c.rows[i] = m.renderRow(i, c.colWidths)
		}
		rowStr := c.rows[i]
		start := 0
		for start <= len(rowStr) && linesWritten < viewH {
			nl := strings.IndexByte(rowStr[start:], '\n')
			var line string
			if nl < 0 {
				line = rowStr[start:]
				start = len(rowStr) + 1
			} else {
				line = rowStr[start : start+nl]
				start += nl + 1
			}
			if skipLines > 0 {
				skipLines--
				continue
			}
			if linesWritten > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(line)
			linesWritten++
		}
	}

	for linesWritten < viewH {
		if linesWritten > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(blankLine)
		linesWritten++
	}

	c.body = out.String()
	c.bodyYOffset = yOff
	c.bodyWidth = viewW
	c.bodyHeight = viewH
	c.bodyCurr = currItem
	c.bodyRowCount = len(m.Rows)
	c.bodyValid = true
	return c.body
}

func (m *Model) renderRow(rowId int, colWidths []int) string {
	var style lipgloss.Style

	if m.rowsViewport.GetCurrItem() == rowId {
		style = m.ctx.Styles.Table.SelectedCellStyle
	} else {
		style = m.ctx.Styles.Table.CellStyle
	}

	colHeight := 1
	if m.ContentHeight > 0 {
		colHeight = m.ContentHeight
	} else if !m.ctx.Config.Theme.Ui.Table.Compact {
		colHeight = 2
	}

	// Build each visual row line directly across columns so we avoid
	// lipgloss.JoinHorizontal + RowStyle.Render re-scanning ANSI widths.
	rowLines := make([]strings.Builder, colHeight)
	headerColId := 0

	for i, column := range m.Columns {
		if column.Hidden != nil && *column.Hidden {
			continue
		}

		colWidth := colWidths[headerColId]
		col := m.Rows[rowId][i]
		// Account for cell padding (1 left + 1 right = 2)
		contentWidth := max(colWidth-2, 1)
		if strings.Contains(col, "\n") {
			// For multi-line content, apply cell style to each line individually
			// This ensures background color extends properly despite ANSI resets in content
			lines := strings.Split(col, "\n")
			lineStyle := style.Width(colWidth).MaxWidth(colWidth).Height(1)
			if column.Align != nil {
				lineStyle = lineStyle.Align(*column.Align)
			}
			for j := range colHeight {
				var line string
				if j < len(lines) {
					line = lines[j]
					if len(line) > contentWidth && lipgloss.Width(line) > contentWidth {
						line = ansi.Truncate(line, contentWidth, "")
					}
				}
				rowLines[j].WriteString(lineStyle.Render(line))
			}
		} else {
			cellStyle := style.
				Width(colWidth).
				MaxWidth(colWidth).
				Height(colHeight).
				MaxHeight(colHeight)
			if column.Align != nil {
				cellStyle = cellStyle.Align(*column.Align)
			}
			renderedCol := cellStyle.Render(col)
			if colHeight == 1 {
				rowLines[0].WriteString(renderedCol)
			} else {
				start := 0
				for j := range colHeight {
					nl := strings.IndexByte(renderedCol[start:], '\n')
					if nl < 0 {
						rowLines[j].WriteString(renderedCol[start:])
						start = len(renderedCol)
					} else {
						rowLines[j].WriteString(renderedCol[start : start+nl])
						start += nl + 1
					}
				}
			}
		}

		headerColId++
	}

	c := m.ensureCache()
	rightPad := ""
	if c.totalColWidth < m.dimensions.Width {
		rightPad = strings.Repeat(" ", m.dimensions.Width-c.totalColWidth)
	}

	var out strings.Builder
	for j := range colHeight {
		if j > 0 {
			out.WriteByte('\n')
		}
		line := rowLines[j].String()
		if c.totalColWidth > m.dimensions.Width {
			line = ansi.Truncate(line, m.dimensions.Width, "")
		} else if rightPad != "" {
			line += rightPad
		}
		out.WriteString(line)
	}
	if m.ctx.Config.Theme.Ui.Table.ShowSeparator {
		out.WriteByte('\n')
		out.WriteString(c.separator)
	}
	return out.String()
}

func (m *Model) UpdateProgramContext(ctx *context.ProgramContext) {
	c := m.ensureCache()
	compact := ctx.Config != nil && ctx.Config.Theme != nil && ctx.Config.Theme.Ui.Table.Compact
	sep := ctx.Config != nil && ctx.Config.Theme != nil && ctx.Config.Theme.Ui.Table.ShowSeparator
	changed := !c.lastTheme || c.lastCompact != compact || c.lastSeparator != sep ||
		m.ctx.Theme != ctx.Theme
	m.ctx = *ctx
	m.rowsViewport.UpdateProgramContext(ctx)
	if changed {
		c.lastTheme = true
		c.lastCompact = compact
		c.lastSeparator = sep
		c.layoutValid = false
		m.invalidateRows()
	}
}

func (m *Model) LastUpdated() time.Time {
	return m.rowsViewport.LastUpdated
}

func (m *Model) CreatedAt() time.Time {
	return m.rowsViewport.CreatedAt
}

func (m *Model) SetCreatedAt(t time.Time) {
	m.rowsViewport.CreatedAt = t
}

func (m *Model) SetLastUpdated(t time.Time) {
	m.rowsViewport.LastUpdated = t
}

func (m *Model) UpdateTotalItemsCount(count int) {
	m.rowsViewport.SetTotalItems(count)
}

func (m *Model) IsLoading() bool {
	return m.isLoading
}
