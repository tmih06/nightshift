package repoview

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/ghdash/config"
)

// Update processes Bubble Tea messages for repoview.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.invalidateView()
		return m, nil

	case tea.KeyPressMsg:
		m.invalidateView()
		return m.updateKey(msg)

	case tea.PasteMsg:
		m.invalidateView()
		if m.modal != ModalNone {
			return m.updateModalPaste(msg)
		}
		if m.searchMode == SearchNav {
			var cmd tea.Cmd
			m.navSearchInput, cmd = m.navSearchInput.Update(msg)
			m.navFilter = m.navSearchInput.Value()
			m.invalidateNav()
			m.ensureNavCursorValid()
			return m, cmd
		}
		if m.searchMode == SearchList {
			var cmd tea.Cmd
			m.listSearchInput, cmd = m.listSearchInput.Update(msg)
			m.listFilter = m.listSearchInput.Value()
			m.invalidateList()
			m.ensureListCursorValid()
			return m, cmd
		}
		return m, nil

	case tea.MouseClickMsg:
		m.invalidateView()
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		if m.draggingDiv != 0 {
			m.resizeDivider(m.draggingDiv, msg.X)
			m.invalidateView()
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if m.draggingDiv != 0 {
			m.draggingDiv = 0
			m.invalidateView()
		}
		return m, nil

	case tea.MouseWheelMsg:
		m.invalidateView()
		return m.handleMouseWheel(msg)

	// Internal async messages.
	case fetchDebounceMsg:
		if msg.seq != m.fetchSeq {
			return m, nil
		}
		if !msg.at.IsZero() && msg.at.Before(m.fetchDeadline) {
			rem := m.fetchDeadline.Sub(msg.at)
			seq := m.fetchSeq
			return m, tea.Tick(rem, func(t time.Time) tea.Msg {
				return fetchDebounceMsg{seq: seq, at: t}
			})
		}
		m.fetchTimerActive = false
		return m, tea.Batch(
			m.ensureRepoOverviewCmd(m.selectedRepo),
			m.ensureActiveScopeLoadedCmd(),
			m.ensureSelectionDetailsCmd(),
		)

	case viewerLoadedMsg:
		if msg.err == nil && msg.login != "" && msg.login != m.viewer {
			m.viewer = msg.login
			m.invalidateNav()
		}
		return m, nil

	case reposLoadedMsg:
		m.loadingRepos = false
		m.invalidateNav()
		if msg.err != nil {
			return m, m.setNotice("Failed to load repos: "+msg.err.Error(), true)
		}
		for _, r := range msg.repos {
			m.upsertRepo(r)
		}
		if m.selectedRepo == "" {
			rows := m.navRows()
			for _, r := range rows {
				if r.kind == navRepo {
					m.selectedRepo = r.repoFullName
					m.expandedRepos[strings.ToLower(r.repoFullName)] = true
					m.invalidateNav()
					m.invalidateList()
					break
				}
			}
		}
		m.ensureNavCursorValid()
		if m.selectedRepo != "" {
			m.cancelPendingFetch()
			return m, tea.Batch(m.ensureRepoOverviewCmd(m.selectedRepo), m.ensureActiveScopeLoadedCmd())
		}
		return m, nil

	case searchReposLoadedMsg:
		m.addRepoSearching = false
		m.invalidateView()
		if msg.err != nil {
			m.modalErr = msg.err.Error()
			return m, nil
		}
		m.addRepoResults = msg.repos
		m.addRepoCursor = 0
		if len(msg.repos) == 0 {
			m.modalErr = "No repositories found"
		}
		return m, nil

	case repoOverviewLoadedMsg:
		rc := m.getOrCreateCache(msg.repo)
		rc.overviewLoading = false
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Overview error: "+msg.err.Error(), true)
		}
		rc.overviewLoaded = true
		m.upsertRepo(msg.data)
		return m, nil

	case branchesLoadedMsg:
		delete(m.loadingScope, strings.ToLower(msg.repo)+":"+ScopeBranches.String())
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Branches error: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.branches = msg.branches
		rc.branchesLoaded = true
		if strings.EqualFold(msg.repo, m.selectedRepo) && m.activeScope == ScopeBranches {
			m.ensureListCursorValid()
			return m, m.ensureSelectionDetailsCmd()
		}
		return m, nil

	case branchCommitsLoadedMsg:
		rc := m.getOrCreateCache(msg.repo)
		delete(rc.commitsLoading, msg.branch)
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Commits error: "+msg.err.Error(), true)
		}
		rc.commits[msg.branch] = msg.commits
		return m, nil

	case branchCreatedMsg:
		if msg.err != nil {
			return m, m.setNotice("Create branch failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.branchesLoaded = false
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		return m, tea.Batch(
			m.setNotice("Created branch "+msg.branch, false),
			m.ensureActiveScopeLoadedCmd(),
		)

	case branchDeletedMsg:
		if msg.err != nil {
			return m, m.setNotice("Delete branch failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.branchesLoaded = false
		delete(rc.commits, msg.branch)
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		return m, tea.Batch(
			m.setNotice("Deleted branch "+msg.branch, false),
			m.ensureActiveScopeLoadedCmd(),
		)

	case prsLoadedMsg:
		delete(m.loadingScope, strings.ToLower(msg.repo)+":"+ScopePRs.String())
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("PRs error: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.prs = msg.prs
		rc.prsLoaded = true
		if strings.EqualFold(msg.repo, m.selectedRepo) && m.activeScope == ScopePRs {
			m.ensureListCursorValid()
		}
		return m, nil

	case issuesLoadedMsg:
		delete(m.loadingScope, strings.ToLower(msg.repo)+":"+ScopeIssues.String())
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Issues error: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.issues = msg.issues
		rc.issuesLoaded = true
		if strings.EqualFold(msg.repo, m.selectedRepo) && m.activeScope == ScopeIssues {
			m.ensureListCursorValid()
			return m, m.ensureSelectionDetailsCmd()
		}
		return m, nil

	case issueCommentsLoadedMsg:
		rc := m.getOrCreateCache(msg.repo)
		delete(rc.issueCommentsLoading, msg.number)
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Comments error: "+msg.err.Error(), true)
		}
		rc.issueComments[msg.number] = msg.comments
		return m, nil

	case issueCreatedMsg:
		if msg.err != nil {
			return m, m.setNotice("Create issue failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.issues = append([]Issue{msg.issue}, rc.issues...)
		rc.issuesLoaded = true
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		return m, m.setNotice(fmt.Sprintf("Created issue #%d", msg.issue.Number), false)

	case issueCommentedMsg:
		if msg.err != nil {
			return m, m.setNotice("Comment failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.issueComments[msg.number] = append(rc.issueComments[msg.number], msg.comment)
		for i := range rc.issues {
			if rc.issues[i].Number == msg.number {
				rc.issues[i].CommentsCount++
				break
			}
		}
		m.invalidateList()
		m.invalidateData()
		return m, m.setNotice(fmt.Sprintf("Commented on #%d", msg.number), false)

	case issueStateSetMsg:
		if msg.err != nil {
			return m, m.setNotice("Update issue failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		upper := strings.ToUpper(msg.state)
		for i := range rc.issues {
			if rc.issues[i].Number == msg.number {
				rc.issues[i].State = upper
				break
			}
		}
		m.invalidateList()
		m.invalidateData()
		return m, m.setNotice(fmt.Sprintf("Issue #%d marked %s", msg.number, strings.ToLower(msg.state)), false)

	case runsLoadedMsg:
		delete(m.loadingScope, strings.ToLower(msg.repo)+":"+ScopeActions.String())
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Actions error: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.runs = msg.runs
		rc.runsLoaded = true
		if strings.EqualFold(msg.repo, m.selectedRepo) && m.activeScope == ScopeActions {
			m.ensureListCursorValid()
			return m, m.ensureSelectionDetailsCmd()
		}
		return m, nil

	case jobsLoadedMsg:
		rc := m.getOrCreateCache(msg.repo)
		delete(rc.jobsLoading, msg.runID)
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Jobs error: "+msg.err.Error(), true)
		}
		rc.jobs[msg.runID] = msg.jobs
		return m, nil

	case workflowRerunMsg:
		if msg.err != nil {
			return m, m.setNotice("Rerun failed: "+msg.err.Error(), true)
		}
		rc := m.getOrCreateCache(msg.repo)
		rc.runsLoaded = false
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		return m, tea.Batch(
			m.setNotice("Workflow rerun triggered", false),
			m.ensureActiveScopeLoadedCmd(),
		)

	case dirLoadedMsg:
		delete(m.loadingScope, strings.ToLower(msg.repo)+":"+ScopeFiles.String())
		rc := m.getOrCreateCache(msg.repo)
		key := m.dirCacheKey(msg.ref, msg.path)
		delete(rc.dirsLoading, key)
		m.invalidateList()
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("Files error: "+msg.err.Error(), true)
		}
		rc.dirs[key] = msg.entries
		if strings.EqualFold(msg.repo, m.selectedRepo) && m.activeScope == ScopeFiles && msg.path == m.filePath {
			m.ensureListCursorValid()
			return m, m.ensureSelectionDetailsCmd()
		}
		return m, nil

	case fileLoadedMsg:
		rc := m.getOrCreateCache(msg.repo)
		key := m.dirCacheKey(msg.ref, msg.path)
		delete(rc.filesLoading, key)
		m.invalidateData()
		if msg.err != nil {
			return m, m.setNotice("File preview error: "+msg.err.Error(), true)
		}
		rc.files[key] = msg.content
		return m, nil

	case noticeExpiredMsg:
		if msg.at.Equal(m.noticeAt) {
			m.notice = ""
			m.invalidateView()
		}
		return m, nil

	case browserOpenedMsg:
		if msg.err != nil {
			return m, m.setNotice("Open browser failed: "+msg.err.Error(), true)
		}
		return m, m.setNotice("Opened in browser", false)

	case clipboardCopiedMsg:
		if msg.err != nil {
			return m, m.setNotice("Copy failed: "+msg.err.Error(), true)
		}
		return m, m.setNotice("Copied "+msg.label, false)
	}

	return m, nil
}

func (m *Model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.modal != ModalNone {
		return m.updateModalKey(msg)
	}

	if m.searchMode != SearchNone {
		return m.updateSearchKey(msg)
	}

	k := msg.String()

	if m.showHelp {
		if k == "?" || k == "esc" || k == "q" {
			m.showHelp = false
		}
		return m, nil
	}

	// Global keys (tab / shift+tab intentionally left untouched for Nightshift root tabs).
	switch k {
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case ":":
		return m, m.openPaletteModal()
	case "1":
		m.activePane = PaneNav
		m.detailsOverview = false
		return m, nil
	case "2":
		m.activePane = PaneList
		m.detailsOverview = false
		m.cancelPendingFetch()
		return m, m.ensureActiveScopeLoadedCmd()
	case "3":
		if m.activePane == PaneNav {
			if nr, ok := m.currentNavRow(); ok && nr.kind == navRepo {
				m.detailsOverview = true
			} else {
				m.detailsOverview = false
			}
		} else if m.activePane == PaneList {
			m.detailsOverview = false
		}
		m.activePane = PaneDetails
		m.cancelPendingFetch()
		if m.detailsOverview {
			return m, m.ensureRepoOverviewCmd(m.selectedRepo)
		}
		return m, m.ensureSelectionDetailsCmd()
	case "<":
		m.hideNav = !m.hideNav
		if m.hideNav && m.activePane == PaneNav {
			m.activePane = PaneList
		}
		return m, nil
	case ">":
		m.hideDetails = !m.hideDetails
		if m.hideDetails && m.activePane == PaneDetails {
			m.activePane = PaneList
		}
		return m, nil
	case "v":
		m.zoomDetails = !m.zoomDetails
		if m.zoomDetails {
			if m.activePane == PaneNav {
				if nr, ok := m.currentNavRow(); ok && nr.kind == navRepo {
					m.detailsOverview = true
				} else {
					m.detailsOverview = false
				}
			} else if m.activePane == PaneList {
				m.detailsOverview = false
			}
			m.activePane = PaneDetails
		}
		return m, nil
	case "s":
		return m, func() tea.Msg { return SwitchViewMsg{View: config.NotificationsView} }
	case "q", "ctrl+c":
		return m, tea.Quit
	case "a":
		return m, m.openAddRepoModal()
	case "r":
		return m, m.refreshActive()
	case "o":
		return m, m.openSelectionInBrowser()
	case "y":
		return m, m.copySelection(false)
	case "Y":
		return m, m.copySelection(true)
	case "S":
		return m, m.launchSandboxForSelection()
	case "b":
		if pod, ok := m.activeSandboxForSelection(); ok {
			return m, func() tea.Msg { return FocusSandboxMsg{Pod: pod} }
		}
		return m, m.setNotice("No active sandbox for selection", false)
	case "[":
		return m, m.cycleScope(-1)
	case "]":
		return m, m.cycleScope(1)
	}

	switch m.activePane {
	case PaneNav:
		return m.updateNavKey(k)
	case PaneList:
		return m.updateListKey(k)
	case PaneDetails:
		return m.updateDetailsKey(k)
	}

	return m, nil
}

func (m *Model) updateSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc":
		if m.searchMode == SearchNav {
			m.navFilter = ""
			m.navSearchInput.SetValue("")
			m.navSearchInput.Blur()
			m.invalidateNav()
			m.ensureNavCursorValid()
		} else if m.searchMode == SearchList {
			m.listFilter = ""
			m.listSearchInput.SetValue("")
			m.listSearchInput.Blur()
			m.invalidateList()
			m.ensureListCursorValid()
		}
		m.searchMode = SearchNone
		return m, nil
	case "enter":
		if m.searchMode == SearchNav {
			m.navSearchInput.Blur()
			m.syncSelectionFromNav()
		} else if m.searchMode == SearchList {
			m.listSearchInput.Blur()
		}
		m.searchMode = SearchNone
		m.cancelPendingFetch()
		return m, m.ensureSelectionDetailsCmd()
	}

	var cmd tea.Cmd
	if m.searchMode == SearchNav {
		m.navSearchInput, cmd = m.navSearchInput.Update(msg)
		m.navFilter = m.navSearchInput.Value()
		m.invalidateNav()
		m.ensureNavCursorValid()
		m.syncSelectionFromNav()
	} else {
		m.listSearchInput, cmd = m.listSearchInput.Update(msg)
		m.listFilter = m.listSearchInput.Value()
		m.invalidateList()
		m.ensureListCursorValid()
	}
	return m, cmd
}

func (m *Model) updateNavKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "j", "down":
		m.moveNavCursor(1)
		return m, m.scheduleSelectionFetchCmd()
	case "k", "up":
		m.moveNavCursor(-1)
		return m, m.scheduleSelectionFetchCmd()
	case "g":
		m.navFirst()
		return m, m.scheduleSelectionFetchCmd()
	case "G":
		m.navLast()
		return m, m.scheduleSelectionFetchCmd()
	case "/":
		m.searchMode = SearchNav
		m.navSearchInput.SetValue(m.navFilter)
		return m, m.navSearchInput.Focus()
	case "f":
		return m, m.handleTogglePin()
	case "J":
		return m, m.handleReorderPin(1)
	case "K":
		return m, m.handleReorderPin(-1)
	case "enter", "l", "right":
		row, ok := m.currentNavRow()
		if !ok {
			return m, nil
		}
		if row.kind == navRepo {
			key := strings.ToLower(row.repoFullName)
			if k == "enter" {
				m.expandedRepos[key] = !m.expandedRepos[key]
				m.invalidateNav()
			} else {
				if !m.expandedRepos[key] {
					m.expandedRepos[key] = true
					m.invalidateNav()
				} else {
					m.activePane = PaneList
				}
			}
			if !strings.EqualFold(m.selectedRepo, row.repoFullName) {
				m.selectedRepo = row.repoFullName
				m.invalidateList()
			}
			m.cancelPendingFetch()
			return m, tea.Batch(m.ensureRepoOverviewCmd(m.selectedRepo), m.ensureActiveScopeLoadedCmd())
		}
		// navScope: enter focuses PaneList with this scope.
		m.selectedRepo = row.repoFullName
		m.activeScope = row.scope
		m.activePane = PaneList
		m.listCursor = 0
		m.listScroll = 0
		m.detailsScroll = 0
		m.invalidateList()
		m.cancelPendingFetch()
		return m, m.ensureActiveScopeLoadedCmd()
	case "h", "left":
		row, ok := m.currentNavRow()
		if !ok {
			return m, nil
		}
		key := strings.ToLower(row.repoFullName)
		if row.kind == navScope {
			m.selectRepoInNav(row.repoFullName)
			return m, nil
		}
		if m.expandedRepos[key] {
			m.expandedRepos[key] = false
			m.invalidateNav()
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) updateListKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "j", "down":
		m.moveListCursor(1)
		return m, m.scheduleSelectionFetchCmd()
	case "k", "up":
		m.moveListCursor(-1)
		return m, m.scheduleSelectionFetchCmd()
	case "g":
		m.listFirst()
		return m, m.scheduleSelectionFetchCmd()
	case "G":
		m.listLast()
		return m, m.scheduleSelectionFetchCmd()
	case "/":
		m.searchMode = SearchList
		m.listSearchInput.SetValue(m.listFilter)
		return m, m.listSearchInput.Focus()
	case "H":
		return m, m.cycleScope(-1)
	case "L":
		return m, m.cycleScope(1)
	case "h", "left":
		if m.activeScope == ScopeFiles && m.filePath != "" {
			return m, m.goToParentDir()
		}
		m.activePane = PaneNav
		return m, nil
	case "l", "right":
		if m.activeScope == ScopeFiles {
			if row, ok := m.currentListRow(); ok && row.kind == listFile && row.entry.Type == "dir" {
				return m, m.enterDir(row.entry.Path)
			}
		}
		m.activePane = PaneDetails
		m.cancelPendingFetch()
		return m, m.ensureSelectionDetailsCmd()
	case "enter":
		return m, m.activateListItem()
	case "n":
		switch m.activeScope {
		case ScopeBranches:
			return m, m.openNewBranchModal()
		case ScopeIssues:
			return m, m.openNewIssueModal()
		}
	case "d":
		if m.activeScope == ScopeBranches {
			if row, ok := m.currentListRow(); ok && row.kind == listBranch && row.branch != nil {
				if row.branch.IsDefault {
					return m, m.setNotice("Cannot delete default branch", true)
				}
				return m, m.promptDeleteBranch(row.branch.Name)
			}
		}
	case "c":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.openCommentIssueModal(row.issue.Number)
			}
		}
	case "x":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.promptSetIssueState(row.issue.Number, "closed")
			}
		}
	case "X":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.promptSetIssueState(row.issue.Number, "open")
			}
		}
	case "R":
		if m.activeScope == ScopeActions {
			if row, ok := m.currentListRow(); ok && row.kind == listRun && row.run != nil {
				failedOnly := strings.EqualFold(row.run.Conclusion, "failure")
				return m, m.promptRerunWorkflow(*row.run, failedOnly)
			}
		}
	}
	return m, nil
}

func (m *Model) updateDetailsKey(k string) (tea.Model, tea.Cmd) {
	page := max(1, (m.height-4)/2)
	switch k {
	case "j", "down":
		m.detailsScroll++
	case "k", "up":
		if m.detailsScroll > 0 {
			m.detailsScroll--
		}
	case "ctrl+d", "pgdown", " ":
		m.detailsScroll += page
	case "ctrl+u", "pgup":
		m.detailsScroll = max(0, m.detailsScroll-page)
	case "g":
		m.detailsScroll = 0
	case "G":
		m.detailsScroll = 1 << 20 // clamped on render
	case "h", "left", "esc":
		m.zoomDetails = false
		m.detailsOverview = false
		m.activePane = PaneList
	case "enter":
		if m.activeScope == ScopePRs {
			if row, ok := m.currentListRow(); ok && row.kind == listPR && row.pr != nil {
				return m, m.emitOpenPR(*row.pr)
			}
		}
	case "c":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.openCommentIssueModal(row.issue.Number)
			}
		}
	case "x":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.promptSetIssueState(row.issue.Number, "closed")
			}
		}
	case "X":
		if m.activeScope == ScopeIssues {
			if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
				return m, m.promptSetIssueState(row.issue.Number, "open")
			}
		}
	case "R":
		if m.activeScope == ScopeActions {
			if row, ok := m.currentListRow(); ok && row.kind == listRun && row.run != nil {
				failedOnly := strings.EqualFold(row.run.Conclusion, "failure")
				return m, m.promptRerunWorkflow(*row.run, failedOnly)
			}
		}
	}
	return m, nil
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.modal != ModalNone {
		return m.handleModalMouse(msg)
	}
	if msg.Button != tea.MouseLeft {
		return m, nil
	}

	pl := m.computeLayout()

	// Footer row click.
	if msg.Y == pl.bodyH {
		switch m.footerHitTest(msg.X) {
		case footerHitNotifications:
			return m, func() tea.Msg { return SwitchViewMsg{View: config.NotificationsView} }
		case footerHitPRs:
			return m, func() tea.Msg { return SwitchViewMsg{View: config.PRsView} }
		case footerHitIssues:
			return m, func() tea.Msg { return SwitchViewMsg{View: config.IssuesView} }
		case footerHitHelp:
			m.showHelp = !m.showHelp
			return m, nil
		}
		return m, nil
	}

	if msg.Y < 0 || msg.Y >= pl.bodyH {
		return m, nil
	}

	// Divider drag start.
	if div := pl.dividerAt(msg.X); div != 0 {
		m.draggingDiv = div
		return m, nil
	}

	pane, ok := pl.paneAt(msg.X)
	if !ok {
		return m, nil
	}

	// Leave inline search if clicking elsewhere.
	if m.searchMode != SearchNone {
		m.searchMode = SearchNone
		m.navSearchInput.Blur()
		m.listSearchInput.Blur()
	}

	prevPane := m.activePane
	m.activePane = pane
	if pane == PaneDetails {
		if prevPane == PaneNav {
			if nr, ok := m.currentNavRow(); ok && nr.kind == navRepo {
				m.detailsOverview = true
			} else {
				m.detailsOverview = false
			}
		} else if prevPane == PaneList {
			m.detailsOverview = false
		}
	} else {
		m.detailsOverview = false
	}

	switch pane {
	case PaneNav:
		if msg.Y == 1 {
			m.searchMode = SearchNav
			m.navSearchInput.SetValue(m.navFilter)
			return m, m.navSearchInput.Focus()
		}
		const navContentTop = 3
		if msg.Y >= navContentTop {
			rows := m.navRows()
			idx := m.navScroll + (msg.Y - navContentTop)
			if idx >= 0 && idx < len(rows) && rows[idx].kind != navHeading {
				dbl := m.recordClick(PaneNav, idx)
				m.navCursor = idx
				m.syncSelectionFromNav()
				r := rows[idx]
				if dbl {
					if r.kind == navRepo {
						key := strings.ToLower(r.repoFullName)
						m.expandedRepos[key] = !m.expandedRepos[key]
						m.invalidateNav()
					} else if r.kind == navScope {
						m.activePane = PaneList
					}
				}
				m.cancelPendingFetch()
				return m, tea.Batch(m.ensureRepoOverviewCmd(m.selectedRepo), m.ensureActiveScopeLoadedCmd())
			}
		}
		return m, nil

	case PaneList:
		if msg.Y == 1 {
			if sc, hit := m.scopeTabAt(msg.X - pl.listX); hit {
				return m, m.setScope(sc)
			}
			return m, nil
		}
		listTop := m.listHeaderRows()
		if (m.searchMode == SearchList || m.listFilter != "") && msg.Y == 2 {
			m.searchMode = SearchList
			return m, m.listSearchInput.Focus()
		}
		if msg.Y >= listTop {
			rows := m.listRows()
			idx := m.listScroll + (msg.Y - listTop)
			if idx >= 0 && idx < len(rows) && rows[idx].kind != listHeading {
				dbl := m.recordClick(PaneList, idx)
				m.listCursor = idx
				m.detailsScroll = 0
				m.cancelPendingFetch()
				if dbl {
					return m, m.activateListItem()
				}
				return m, m.ensureSelectionDetailsCmd()
			}
		}
		return m, nil

	case PaneDetails:
		return m, nil
	}

	return m, nil
}

func (m *Model) recordClick(pane Pane, row int) bool {
	now := m.now()
	dbl := pane == m.lastClickPane && row == m.lastClickRow && now.Sub(m.lastClickAt) <= doubleClickDelay
	if dbl {
		m.lastClickAt = m.lastClickAt.Add(-2 * doubleClickDelay) // reset so triple-click doesn't re-fire
	} else {
		m.lastClickPane = pane
		m.lastClickRow = row
		m.lastClickAt = now
	}
	return dbl
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.modal != ModalNone {
		return m, nil
	}
	pl := m.computeLayout()
	pane, ok := pl.paneAt(msg.X)
	if !ok {
		return m, nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}

	switch pane {
	case PaneNav:
		m.moveNavCursor(delta)
		return m, m.scheduleSelectionFetchCmd()
	case PaneList:
		m.moveListCursor(delta)
		return m, m.scheduleSelectionFetchCmd()
	case PaneDetails:
		m.detailsScroll = max(0, m.detailsScroll+delta*3)
		return m, nil
	}
	return m, nil
}

// Actions & helpers.

func (m *Model) setScope(sc Scope) tea.Cmd {
	m.cancelPendingFetch()
	if m.activeScope == sc {
		return m.ensureActiveScopeLoadedCmd()
	}
	m.activeScope = sc
	m.listCursor = 0
	m.listScroll = 0
	m.detailsScroll = 0
	m.listFilter = ""
	m.listSearchInput.SetValue("")
	m.invalidateList()
	return m.ensureActiveScopeLoadedCmd()
}

func (m *Model) cycleScope(delta int) tea.Cmd {
	n := len(allScopes)
	idx := 0
	for i, sc := range allScopes {
		if sc == m.activeScope {
			idx = i
			break
		}
	}
	next := allScopes[((idx+delta)%n+n)%n]
	return m.setScope(next)
}

func (m *Model) activateListItem() tea.Cmd {
	row, ok := m.currentListRow()
	if !ok {
		return nil
	}
	switch row.kind {
	case listPR:
		return m.emitOpenPR(*row.pr)
	case listParentDir:
		return m.goToParentDir()
	case listFile:
		if row.entry.Type == "dir" {
			return m.enterDir(row.entry.Path)
		}
		m.activePane = PaneDetails
		return m.ensureSelectionDetailsCmd()
	default:
		m.activePane = PaneDetails
		return m.ensureSelectionDetailsCmd()
	}
}

func (m *Model) emitOpenPR(pr PullRequest) tea.Cmd {
	repo := m.selectedRepo
	url := pr.URL
	if url == "" && repo != "" {
		url = fmt.Sprintf("https://github.com/%s/pull/%d", repo, pr.Number)
	}
	return func() tea.Msg {
		return OpenPRMsg{
			Repo:   repo,
			Number: pr.Number,
			URL:    url,
		}
	}
}

func (m *Model) enterDir(dirPath string) tea.Cmd {
	m.filePath = strings.Trim(dirPath, "/")
	m.listCursor = 0
	m.listScroll = 0
	m.detailsScroll = 0
	m.invalidateList()
	m.cancelPendingFetch()
	return m.ensureActiveScopeLoadedCmd()
}

func (m *Model) goToParentDir() tea.Cmd {
	m.filePath = parentDirPath(m.filePath)
	m.listCursor = 0
	m.listScroll = 0
	m.detailsScroll = 0
	m.invalidateList()
	m.cancelPendingFetch()
	return m.ensureActiveScopeLoadedCmd()
}

func (m *Model) handleTogglePin() tea.Cmd {
	repo := m.selectedRepo
	if repo == "" {
		return nil
	}
	pinned, err := m.togglePin(repo)
	m.selectRepoInNav(repo)
	if err != nil {
		return m.setNotice("Save pins failed: "+err.Error(), true)
	}
	if pinned {
		return m.setNotice("Pinned "+repo, false)
	}
	return m.setNotice("Unpinned "+repo, false)
}

func (m *Model) handleReorderPin(delta int) tea.Cmd {
	repo := m.selectedRepo
	if repo == "" || !m.isPinned(repo) {
		return nil
	}
	moved, err := m.reorderPin(repo, delta)
	if err != nil {
		return m.setNotice("Save pins failed: "+err.Error(), true)
	}
	if moved {
		return m.setNotice("Reordered pinned repos", false)
	}
	return nil
}

func (m *Model) addAndSelectRepo(r Repo) tea.Cmd {
	fn := r.NormalizedFullName()
	if fn == "" {
		return nil
	}
	m.upsertRepo(r)
	m.selectedRepo = fn
	m.expandedRepos[strings.ToLower(fn)] = true
	m.invalidateNav()
	m.invalidateList()
	m.selectRepoInNav(fn)
	m.filePath = ""
	m.listCursor = 0
	m.listScroll = 0
	m.detailsScroll = 0
	m.cancelPendingFetch()
	return tea.Batch(
		m.setNotice("Opened "+fn, false),
		m.ensureRepoOverviewCmd(fn),
		m.ensureActiveScopeLoadedCmd(),
	)
}

func (m *Model) addAndSelectRepoName(fullName string) tea.Cmd {
	owner, name, _ := strings.Cut(fullName, "/")
	return m.addAndSelectRepo(Repo{
		Owner:    owner,
		Name:     name,
		FullName: fullName,
		URL:      "https://github.com/" + fullName,
	})
}

func (m *Model) refreshActive() tea.Cmd {
	m.cancelPendingFetch()
	repo := m.selectedRepo
	if repo == "" {
		m.loadingRepos = true
		m.invalidateNav()
		return m.fetchReposCmd()
	}
	rc := m.getOrCreateCache(repo)
	switch m.activePane {
	case PaneNav:
		m.loadingRepos = true
		rc.overviewLoaded = false
		m.invalidateNav()
		m.invalidateData()
		return tea.Batch(m.fetchReposCmd(), m.ensureRepoOverviewCmd(repo))
	default:
		switch m.activeScope {
		case ScopeBranches:
			rc.branchesLoaded = false
			clear(rc.commits)
		case ScopePRs:
			rc.prsLoaded = false
		case ScopeIssues:
			rc.issuesLoaded = false
			clear(rc.issueComments)
		case ScopeActions:
			rc.runsLoaded = false
			clear(rc.jobs)
		case ScopeFiles:
			key := m.dirCacheKey(m.fileRef(), m.filePath)
			delete(rc.dirs, key)
			clear(rc.files)
		}
		m.invalidateNav()
		m.invalidateList()
		m.invalidateData()
		return m.ensureActiveScopeLoadedCmd()
	}
}

func (m *Model) launchSandboxForSelection() tea.Cmd {
	repo := m.selectedRepo
	if repo == "" {
		return m.setNotice("Select a repository first", true)
	}
	r := m.findRepo(repo)
	repoURL := r.URL
	if repoURL == "" {
		repoURL = "https://github.com/" + repo
	}

	// If in PaneNav on a repo row, launch repo sandbox on default branch.
	if m.activePane == PaneNav {
		if nr, ok := m.currentNavRow(); ok && nr.kind == navRepo {
			branch := r.DefaultBranch
			return func() tea.Msg {
				return SandboxRepoMsg{Repo: repo, Branch: branch, URL: repoURL}
			}
		}
	}

	row, ok := m.currentListRow()
	if !ok {
		return func() tea.Msg {
			return SandboxRepoMsg{Repo: repo, Branch: r.DefaultBranch, URL: repoURL}
		}
	}

	switch row.kind {
	case listPR:
		pr := *row.pr
		prURL := pr.URL
		if prURL == "" {
			prURL = fmt.Sprintf("https://github.com/%s/pull/%d", repo, pr.Number)
		}
		return func() tea.Msg {
			return SandboxPRMsg{Repo: repo, Number: pr.Number, URL: prURL, Branch: pr.HeadRef}
		}
	case listIssue:
		iss := *row.issue
		issURL := iss.URL
		if issURL == "" {
			issURL = fmt.Sprintf("https://github.com/%s/issues/%d", repo, iss.Number)
		}
		return func() tea.Msg {
			return SandboxIssueMsg{Repo: repo, Number: iss.Number, URL: issURL, Title: iss.Title}
		}
	case listBranch:
		b := *row.branch
		return func() tea.Msg {
			return SandboxRepoMsg{Repo: repo, Branch: b.Name, URL: repoURL}
		}
	case listRun:
		run := *row.run
		branch := run.Branch
		if branch == "" {
			branch = r.DefaultBranch
		}
		return func() tea.Msg {
			return SandboxRepoMsg{Repo: repo, Branch: branch, URL: repoURL}
		}
	default:
		return func() tea.Msg {
			return SandboxRepoMsg{Repo: repo, Branch: r.DefaultBranch, URL: repoURL}
		}
	}
}

func (m *Model) activeSandboxForSelection() (string, bool) {
	repo := m.selectedRepo
	if repo == "" {
		return "", false
	}
	if m.activePane != PaneNav {
		if row, ok := m.currentListRow(); ok {
			switch row.kind {
			case listPR:
				if box, ok := sandboxForPR(m.sandboxes, repo, row.pr.Number); ok {
					return box.Pod, true
				}
			case listIssue:
				if box, ok := sandboxForIssue(m.sandboxes, repo, row.issue.Number); ok {
					return box.Pod, true
				}
			case listBranch:
				if box, ok := sandboxForBranch(m.sandboxes, repo, row.branch.Name); ok {
					return box.Pod, true
				}
			}
		}
	}
	if boxes := sandboxesForRepo(m.sandboxes, repo); len(boxes) > 0 {
		return boxes[0].Pod, true
	}
	return "", false
}

func (m *Model) selectionURL() string {
	repo := m.selectedRepo
	if repo == "" {
		return ""
	}
	base := "https://github.com/" + repo
	if m.activePane == PaneNav {
		return base
	}
	row, ok := m.currentListRow()
	if !ok {
		return base
	}
	switch row.kind {
	case listBranch:
		return base + "/tree/" + row.branch.Name
	case listPR:
		if row.pr.URL != "" {
			return row.pr.URL
		}
		return fmt.Sprintf("%s/pull/%d", base, row.pr.Number)
	case listIssue:
		if row.issue.URL != "" {
			return row.issue.URL
		}
		return fmt.Sprintf("%s/issues/%d", base, row.issue.Number)
	case listRun:
		if row.run.URL != "" {
			return row.run.URL
		}
		return fmt.Sprintf("%s/actions/runs/%d", base, row.run.ID)
	case listFile:
		if row.entry.URL != "" {
			return row.entry.URL
		}
		ref := m.fileRef()
		if ref == "" {
			ref = "HEAD"
		}
		return fmt.Sprintf("%s/blob/%s/%s", base, ref, row.entry.Path)
	}
	return base
}

func (m *Model) openSelectionInBrowser() tea.Cmd {
	u := m.selectionURL()
	if u == "" {
		return m.setNotice("Nothing to open", true)
	}
	opener := m.openBrowser
	return func() tea.Msg {
		err := opener(u)
		return browserOpenedMsg{url: u, err: err}
	}
}

func (m *Model) copySelection(alt bool) tea.Cmd {
	text := ""
	label := "URL"
	if alt {
		// Y copies reference / identifier (repo name, branch name, #number, file path).
		text = m.selectedRepo
		label = "repo"
		if m.activePane != PaneNav {
			if row, ok := m.currentListRow(); ok {
				switch row.kind {
				case listBranch:
					text = row.branch.Name
					label = "branch"
				case listPR:
					text = fmt.Sprintf("%s#%d", m.selectedRepo, row.pr.Number)
					label = "PR ref"
				case listIssue:
					text = fmt.Sprintf("%s#%d", m.selectedRepo, row.issue.Number)
					label = "issue ref"
				case listRun:
					text = strconv.FormatInt(row.run.ID, 10)
					label = "run ID"
				case listFile:
					text = row.entry.Path
					label = "path"
				}
			}
		}
	} else {
		text = m.selectionURL()
	}
	if text == "" {
		return m.setNotice("Nothing to copy", true)
	}
	copier := m.copyClipboard
	return func() tea.Msg {
		err := copier(text)
		return clipboardCopiedMsg{label: label, err: err}
	}
}
