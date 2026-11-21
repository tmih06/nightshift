package tui

import (
	"reflect"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/footer"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/notificationrow"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/section"
	"github.com/tmih06/nightshift/internal/ghdash/tui/components/tabs"
	"github.com/tmih06/nightshift/internal/ghdash/tui/keys"
)

const (
	mouseWheelLines    = 3
	doubleClickWindow  = 400 * time.Millisecond
	minPreviewWidth    = 24
	minMainTableWidth  = 24
	minPreviewHeight   = 6
	minMainTableHeight = 8
)

func (m *Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.ctx == nil || m.ctx.Config == nil {
		return m, nil
	}
	if m.footer.ShowConfirmQuit {
		return m, nil
	}
	currSection := m.getCurrSection()
	if currSection != nil && currSection.IsPromptConfirmationFocused() {
		return m, nil
	}

	mouse := msg.Mouse()
	x, y := mouse.X, mouse.Y

	switch msg.(type) {
	case tea.MouseReleaseMsg:
		m.dragging = false
		return m, nil

	case tea.MouseMotionMsg:
		if m.dragging && mouse.Button == tea.MouseLeft {
			cmd := m.applyDividerDrag(x, y)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		delta := 0
		switch mouse.Button {
		case tea.MouseWheelUp:
			delta = -1
		case tea.MouseWheelDown:
			delta = 1
		default:
			return m, nil
		}
		return m, m.applyMouseWheel(x, y, delta)

	case tea.MouseClickMsg:
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		if m.overDivider(x, y) {
			m.dragging = true
			return m, nil
		}
		// Keep clicks from yanking the row away while typing in a sidebar input box.
		if m.prView.IsTextInputBoxFocused() || m.issueSidebar.IsTextInputBoxFocused() {
			return m, nil
		}
		return m, m.applyMouseClick(x, y)
	}

	return m, nil
}

func (m *Model) bodyTop() int {
	if m.ctx.View == config.RepoView {
		return 1
	}
	return common.TabsHeight
}

func (m *Model) footerY() int {
	top := m.bodyTop()
	if m.sidebar.IsOpen && m.ctx.PreviewPosition == "bottom" {
		return top + m.ctx.MainContentHeight + m.ctx.DynamicPreviewHeight
	}
	return top + m.ctx.MainContentHeight
}

func (m *Model) overDivider(x, y int) bool {
	if !m.sidebar.IsOpen {
		return false
	}
	top := m.bodyTop()
	if m.ctx.PreviewPosition == "bottom" {
		sideTop := top + m.ctx.MainContentHeight
		return y >= sideTop-1 && y <= sideTop+1
	}
	sideX := m.ctx.MainContentWidth
	return y >= top && y < top+m.ctx.MainContentHeight && x >= sideX-1 && x <= sideX+1
}

func (m *Model) inSidebar(x, y int) bool {
	if !m.sidebar.IsOpen {
		return false
	}
	top := m.bodyTop()
	if m.ctx.PreviewPosition == "bottom" {
		sideTop := top + m.ctx.MainContentHeight
		return y >= sideTop && y < sideTop+m.ctx.DynamicPreviewHeight
	}
	return y >= top && y < top+m.ctx.MainContentHeight && x >= m.ctx.MainContentWidth
}

func (m *Model) inSearchBar(x, y int) bool {
	top := m.bodyTop()
	return y >= top && y < top+common.SearchHeight && x >= 0 && x < m.ctx.MainContentWidth
}

func (m *Model) inTableBody(x, y int) bool {
	bodyStart := m.bodyTop() + common.SearchHeight + common.TableHeaderHeight
	bodyEnd := m.bodyTop() + m.ctx.MainContentHeight
	return y >= bodyStart && y < bodyEnd && x >= 0 && x < m.ctx.MainContentWidth
}

func (m *Model) applyDividerDrag(x, y int) tea.Cmd {
	if !m.sidebar.IsOpen {
		return nil
	}
	if m.ctx.PreviewPosition == "bottom" {
		available := m.getBaseContentHeight() - m.ctx.Styles.Sidebar.BorderWidth
		desiredTableH := y - m.bodyTop()
		next := clampPreviewSize(available-desiredTableH, minPreviewHeight, available-minMainTableHeight, available)
		if next == m.ctx.DynamicPreviewHeight {
			return nil
		}
		m.previewHeightOverride = next
	} else {
		desiredPreviewW := m.ctx.ScreenWidth - x
		next := clampPreviewSize(desiredPreviewW, minPreviewWidth, m.ctx.ScreenWidth-minMainTableWidth, m.ctx.ScreenWidth)
		if next == m.ctx.DynamicPreviewWidth {
			return nil
		}
		m.previewWidthOverride = next
	}
	m.syncMainContentDimensions()
	m.syncProgramContext()
	return m.syncSidebar()
}

func clampPreviewSize(val, low, high, limit int) int {
	if limit <= 0 {
		return 0
	}
	if low > limit {
		low = limit
	}
	if high < low {
		high = low
	}
	if high > limit {
		high = limit
	}
	return max(low, min(val, high))
}

func (m *Model) applyMouseWheel(x, y, delta int) tea.Cmd {
	if m.inSidebar(x, y) {
		if delta < 0 {
			m.sidebar.ScrollUp(mouseWheelLines)
		} else {
			m.sidebar.ScrollDown(mouseWheelLines)
		}
		return nil
	}
	if m.prView.IsTextInputBoxFocused() || m.issueSidebar.IsTextInputBoxFocused() {
		return nil
	}
	currSection := m.getCurrSection()
	if currSection == nil {
		return nil
	}
	prevRow := currSection.CurrRow()
	var nextRow int
	if delta > 0 {
		nextRow = currSection.NextRow()
	} else {
		nextRow = currSection.PrevRow()
	}
	return m.finishRowMove(currSection, prevRow, nextRow)
}

func (m *Model) finishRowMove(currSection section.Section, prevRow, nextRow int) tea.Cmd {
	if prevRow == nextRow {
		return nil
	}
	var cmds []tea.Cmd
	if nextRow == currSection.NumRows()-1 && m.ctx.View != config.RepoView {
		cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
	}
	cmds = append(cmds, m.onViewedRowChanged())
	m.syncFooterPager()
	return tea.Batch(cmds...)
}

func (m *Model) applyMouseClick(x, y int) tea.Cmd {
	currSection := m.getCurrSection()

	// If search is focused and the click lands outside the search bar, blur search.
	if currSection != nil && currSection.IsSearchFocused() {
		if m.inSearchBar(x, y) {
			return nil
		}
		currSection.ResetFilters()
		currSection.SetIsSearching(false)
	}

	// Section tabs bar (row 0, hidden in RepoView).
	if m.ctx.View != config.RepoView && y == 0 {
		kind, id := m.tabs.HitTest(x)
		switch kind {
		case tabs.HitNewSection:
			m.addNewSection()
			cmd := m.onViewedRowChanged()
			m.syncFooterPager()
			return cmd
		case tabs.HitPrevOverflow:
			id = m.getPrevSectionId()
		case tabs.HitNextOverflow:
			id = m.getNextSectionId()
		case tabs.HitSection:
		default:
			return nil
		}
		if sec := m.getSectionAt(id); sec != nil && sec.GetId() != m.currSectionId {
			m.setCurrSectionId(sec.GetId())
			cmd := m.onViewedRowChanged()
			m.syncFooterPager()
			return cmd
		}
		return nil
	}

	// Footer view switcher and help button.
	if y == m.footerY() {
		switch m.footer.HitTest(x) {
		case footer.HitNotifications:
			return m.switchToView(config.NotificationsView)
		case footer.HitPRs:
			return m.switchToView(config.PRsView)
		case footer.HitIssues:
			return m.switchToView(config.IssuesView)
		case footer.HitRepos:
			return func() tea.Msg { return OpenRepoViewMsg{} }
		case footer.HitHelp:
			m.footer.ShowAll = !m.footer.ShowAll
			m.syncMainContentDimensions()
			m.syncProgramContext()
			return m.syncSidebar()
		}
		return nil
	}

	// Sidebar clicks (PR preview tabs or loading a notification prompt).
	if m.inSidebar(x, y) {
		return m.applySidebarClick(x, y)
	}

	if currSection == nil {
		return nil
	}

	// Search bar click focuses search.
	if m.inSearchBar(x, y) {
		return currSection.SetIsSearching(true)
	}

	// Table row click selects the row (and double-clicking a PR row opens it).
	if m.inTableBody(x, y) {
		relY := y - (m.bodyTop() + common.SearchHeight + common.TableHeaderHeight)
		rowIdx, ok := currSection.RowAt(relY)
		if !ok {
			return nil
		}
		now := time.Now()
		doubleClick := !m.lastClickAt.IsZero() &&
			now.Sub(m.lastClickAt) <= doubleClickWindow &&
			m.lastClickRow == rowIdx &&
			m.lastClickSection == m.currSectionId &&
			m.lastClickView == m.ctx.View

		if doubleClick {
			m.lastClickAt = time.Time{}
			return m.activateCurrentRow()
		}
		m.lastClickAt = now
		m.lastClickRow = rowIdx
		m.lastClickSection = m.currSectionId
		m.lastClickView = m.ctx.View

		prevRow := currSection.CurrRow()
		nextRow := currSection.SetCurrRow(rowIdx)
		return m.finishRowMove(currSection, prevRow, nextRow)
	}

	return nil
}

func (m *Model) applySidebarClick(x, y int) tea.Cmd {
	var contentX, contentY int
	if m.ctx.PreviewPosition == "bottom" {
		sideTop := m.bodyTop() + m.ctx.MainContentHeight
		contentX = x
		contentY = (y - sideTop - m.ctx.Styles.Sidebar.BorderWidth) + m.sidebar.YOffset()
	} else {
		contentX = x - m.ctx.MainContentWidth - m.ctx.Styles.Sidebar.BorderWidth
		contentY = (y - m.bodyTop()) + m.sidebar.YOffset()
	}
	if contentX < 0 || contentY < 0 {
		return nil
	}

	showingPR := m.ctx.View == config.PRsView ||
		(m.ctx.View == config.NotificationsView && m.notificationView.GetSubjectPR() != nil)
	if showingPR && m.prView.Click(contentX, contentY) {
		return m.syncSidebar()
	}

	if m.ctx.View == config.NotificationsView &&
		m.notificationView.GetSubjectPR() == nil &&
		m.notificationView.GetSubjectIssue() == nil {
		if _, ok := m.getCurrRowData().(*notificationrow.Data); ok {
			return m.loadNotificationContent()
		}
	}

	return nil
}

func (m *Model) activateCurrentRow() tea.Cmd {
	currRowData := m.getCurrRowData()
	if currRowData == nil || reflect.ValueOf(currRowData).IsNil() {
		return nil
	}
	switch m.ctx.View {
	case config.PRsView:
		open := OpenPullRequestMsg{
			Repo:   currRowData.GetRepoNameWithOwner(),
			Number: currRowData.GetNumber(),
			URL:    currRowData.GetUrl(),
		}
		return func() tea.Msg { return open }
	case config.NotificationsView:
		return m.loadNotificationContent()
	}
	return nil
}

func (m *Model) switchToView(view config.ViewType) tea.Cmd {
	if m.ctx.View == view {
		return nil
	}
	if m.ctx.View == config.NotificationsView {
		keys.SetNotificationSubject(keys.NotificationSubjectNone)
		m.notificationView.ClearSubject()
	}
	m.ctx.View = view
	m.syncMainContentDimensions()
	m.setCurrSectionId(0)
	m.syncProgramContext()

	var cmds []tea.Cmd
	if m.isViewSectionsStale() {
		cmds = append(cmds, m.fetchAllViewSections())
	}
	m.updateTabs()
	cmds = append(cmds, m.onViewedRowChanged())
	m.syncFooterPager()
	return tea.Batch(cmds...)
}

func (m *Model) syncFooterPager() {
	currSection := m.getCurrSection()
	if currSection == nil {
		return
	}
	if currSection.IsPromptConfirmationFocused() {
		m.footer.SetLeftSection(currSection.GetPromptConfirmation())
	} else {
		m.footer.SetLeftSection(currSection.GetPagerContent())
	}
}
