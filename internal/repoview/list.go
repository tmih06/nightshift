package repoview

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/tui/constants"
)

type listRowKind int

const (
	listHeading listRowKind = iota
	listBranch
	listPR
	listIssue
	listRun
	listParentDir
	listFile
)

type listRow struct {
	kind   listRowKind
	label  string
	branch *Branch
	pr     *PullRequest
	issue  *Issue
	run    *WorkflowRun
	entry  *TreeEntry
}

func (m *Model) listRows() []listRow {
	repo := m.selectedRepo
	if repo == "" {
		return nil
	}
	key := listRowsKey{
		rev:          m.listRev,
		selectedRepo: repo,
		activeScope:  m.activeScope,
		listFilter:   m.listFilter,
		filePath:     m.filePath,
	}
	if m.cachedListValid && m.cachedListKey == key {
		return m.cachedListRows
	}

	rc := m.cache[strings.ToLower(repo)]
	var rows []listRow
	if rc != nil {
		filter := strings.ToLower(strings.TrimSpace(m.listFilter))
		switch m.activeScope {
		case ScopeBranches:
			rows = m.branchListRows(rc, filter)
		case ScopePRs:
			rows = m.prListRows(rc, filter)
		case ScopeIssues:
			rows = m.issueListRows(rc, filter)
		case ScopeActions:
			rows = m.runListRows(rc, filter)
		case ScopeFiles:
			rows = m.fileListRows(rc, filter)
		}
	}
	cnt := 0
	for _, r := range rows {
		if r.kind != listHeading && r.kind != listParentDir {
			cnt++
		}
	}
	m.cachedListRows = rows
	m.cachedListItemCount = cnt
	m.cachedListKey = key
	m.cachedListValid = true
	return rows
}

func (m *Model) branchListRows(rc *repoCache, filter string) []listRow {
	var defGroup, prGroup, otherGroup []Branch
	for _, b := range rc.branches {
		if filter != "" && !strings.Contains(strings.ToLower(b.Name), filter) {
			continue
		}
		switch {
		case b.IsDefault:
			defGroup = append(defGroup, b)
		case b.LinkedPR != nil:
			prGroup = append(prGroup, b)
		default:
			otherGroup = append(otherGroup, b)
		}
	}
	var rows []listRow
	appendGroup := func(title string, items []Branch) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("%s (%d)", title, len(items))})
		for i := range items {
			b := items[i]
			rows = append(rows, listRow{kind: listBranch, branch: &b})
		}
	}
	appendGroup("DEFAULT", defGroup)
	appendGroup("WITH PR", prGroup)
	appendGroup("OTHER", otherGroup)
	return rows
}

func (m *Model) prListRows(rc *repoCache, filter string) []listRow {
	var openGroup, draftGroup, closedGroup []PullRequest
	for _, pr := range rc.prs {
		if filter != "" {
			hay := strings.ToLower(fmt.Sprintf("#%d %s %s %s", pr.Number, pr.Title, pr.Author, pr.HeadRef))
			if !strings.Contains(hay, filter) {
				continue
			}
		}
		state := strings.ToUpper(pr.State)
		switch {
		case state == "OPEN" && pr.IsDraft:
			draftGroup = append(draftGroup, pr)
		case state == "OPEN":
			openGroup = append(openGroup, pr)
		default:
			closedGroup = append(closedGroup, pr)
		}
	}
	var rows []listRow
	appendGroup := func(title string, items []PullRequest) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("%s (%d)", title, len(items))})
		for i := range items {
			p := items[i]
			rows = append(rows, listRow{kind: listPR, pr: &p})
		}
	}
	appendGroup("OPEN", openGroup)
	appendGroup("DRAFT", draftGroup)
	appendGroup("CLOSED / MERGED", closedGroup)
	return rows
}

func (m *Model) issueListRows(rc *repoCache, filter string) []listRow {
	var openGroup, closedGroup []Issue
	for _, iss := range rc.issues {
		if filter != "" {
			hay := strings.ToLower(fmt.Sprintf("#%d %s %s %s", iss.Number, iss.Title, iss.Author, strings.Join(iss.Labels, " ")))
			if !strings.Contains(hay, filter) {
				continue
			}
		}
		if strings.EqualFold(iss.State, "OPEN") {
			openGroup = append(openGroup, iss)
		} else {
			closedGroup = append(closedGroup, iss)
		}
	}
	var rows []listRow
	appendGroup := func(title string, items []Issue) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("%s (%d)", title, len(items))})
		for i := range items {
			iss := items[i]
			rows = append(rows, listRow{kind: listIssue, issue: &iss})
		}
	}
	appendGroup("OPEN", openGroup)
	appendGroup("CLOSED", closedGroup)
	return rows
}

func (m *Model) runListRows(rc *repoCache, filter string) []listRow {
	var inProg, failed, passed []WorkflowRun
	for _, r := range rc.runs {
		if filter != "" {
			hay := strings.ToLower(fmt.Sprintf("%s %s %s %s #%d", r.Name, r.DisplayTitle, r.Branch, r.Conclusion, r.RunNumber))
			if !strings.Contains(hay, filter) {
				continue
			}
		}
		st := strings.ToLower(r.Status)
		conc := strings.ToLower(r.Conclusion)
		switch {
		case st != "completed":
			inProg = append(inProg, r)
		case conc == "failure" || conc == "timed_out" || conc == "cancelled" || conc == "action_required":
			failed = append(failed, r)
		default:
			passed = append(passed, r)
		}
	}
	var rows []listRow
	appendGroup := func(title string, items []WorkflowRun) {
		if len(items) == 0 {
			return
		}
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("%s (%d)", title, len(items))})
		for i := range items {
			r := items[i]
			rows = append(rows, listRow{kind: listRun, run: &r})
		}
	}
	appendGroup("IN PROGRESS", inProg)
	appendGroup("FAILED", failed)
	appendGroup("PASSED", passed)
	return rows
}

func (m *Model) fileListRows(rc *repoCache, filter string) []listRow {
	key := m.dirCacheKey(m.fileRef(), m.filePath)
	entries := rc.dirs[key]

	var rows []listRow
	if m.filePath != "" {
		rows = append(rows, listRow{kind: listParentDir, label: ".."})
	}

	var dirs, files []TreeEntry
	for _, e := range entries {
		if filter != "" && !strings.Contains(strings.ToLower(e.Name), filter) {
			continue
		}
		if e.Type == "dir" {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	if len(dirs) > 0 {
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("FOLDERS (%d)", len(dirs))})
		for i := range dirs {
			e := dirs[i]
			rows = append(rows, listRow{kind: listFile, entry: &e})
		}
	}
	if len(files) > 0 {
		rows = append(rows, listRow{kind: listHeading, label: fmt.Sprintf("FILES (%d)", len(files))})
		for i := range files {
			e := files[i]
			rows = append(rows, listRow{kind: listFile, entry: &e})
		}
	}
	return rows
}

func (m *Model) ensureListCursorValid() {
	rows := m.listRows()
	if len(rows) == 0 {
		m.listCursor = 0
		m.listScroll = 0
		return
	}
	if m.listCursor < 0 {
		m.listCursor = 0
	}
	if m.listCursor >= len(rows) {
		m.listCursor = len(rows) - 1
	}
	if rows[m.listCursor].kind == listHeading {
		for i := m.listCursor + 1; i < len(rows); i++ {
			if rows[i].kind != listHeading {
				m.listCursor = i
				return
			}
		}
		for i := m.listCursor - 1; i >= 0; i-- {
			if rows[i].kind != listHeading {
				m.listCursor = i
				return
			}
		}
	}
}

func (m *Model) moveListCursor(delta int) {
	rows := m.listRows()
	if len(rows) == 0 {
		return
	}
	m.ensureListCursorValid()
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	cur := m.listCursor
	for range delta {
		next := cur + step
		for next >= 0 && next < len(rows) && rows[next].kind == listHeading {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		cur = next
	}
	if cur != m.listCursor {
		m.listCursor = cur
		m.detailsScroll = 0
	}
}

func (m *Model) listFirst() {
	rows := m.listRows()
	for i, r := range rows {
		if r.kind != listHeading {
			m.listCursor = i
			m.detailsScroll = 0
			return
		}
	}
}

func (m *Model) listLast() {
	rows := m.listRows()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].kind != listHeading {
			m.listCursor = i
			m.detailsScroll = 0
			return
		}
	}
}

func (m *Model) currentListRow() (listRow, bool) {
	rows := m.listRows()
	m.ensureListCursorValid()
	if m.listCursor < 0 || m.listCursor >= len(rows) {
		return listRow{}, false
	}
	if rows[m.listCursor].kind == listHeading {
		return listRow{}, false
	}
	return rows[m.listCursor], true
}

func (m *Model) listItemCount() int {
	if m.selectedRepo == "" {
		return 0
	}
	_ = m.listRows()
	return m.cachedListItemCount
}

func (m *Model) listHeaderRows() int {
	// Row 0: pane title bar
	// Row 1: scope tab strip
	// Row 2: separator (or search bar + separator when searching/filtering)
	if m.searchMode == SearchList || m.listFilter != "" {
		return 4
	}
	return 3
}

func (m *Model) renderListPane(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	focused := m.activePane == PaneList
	title := m.selectedRepo
	if title == "" {
		title = "No repo"
	} else {
		title = fmt.Sprintf("%s · %s", title, m.activeScope.String())
		if m.activeScope == ScopeFiles && m.filePath != "" {
			title = fmt.Sprintf("%s · /%s", m.selectedRepo, m.filePath)
		}
	}
	rows := m.listRows()
	right := ""
	if cnt := m.cachedListItemCount; cnt > 0 && m.selectedRepo != "" {
		right = fmt.Sprintf("(%d)", cnt)
	}

	lines := make([]string, 0, height)
	lines = append(lines, m.renderPaneHeader(2, title, right, width, focused))
	if height == 1 {
		return lines
	}

	// Row 1: clickable scope tab strip.
	lines = append(lines, m.renderScopeTabs(width))
	if height == 2 {
		return lines
	}

	// Optional search bar.
	if m.searchMode == SearchList || m.listFilter != "" {
		var inputView string
		if m.searchMode == SearchList {
			m.listSearchInput.SetWidth(max(4, width-ansi.StringWidth(m.listSearchInput.Prompt)-2))
			inputView = m.listSearchInput.View()
		}
		lines = append(lines, m.renderSearchBar(width, m.searchMode == SearchList, inputView, m.listFilter, "/ Filter…"))
	}

	if len(lines) < height {
		lines = append(lines, m.ui.faintBorder.Render(strings.Repeat("─", width)))
	}

	contentH := height - len(lines)
	if contentH <= 0 {
		return lines[:height]
	}

	m.ensureListCursorValid()

	blank := strings.Repeat(" ", width)
	if len(rows) == 0 {
		empty := m.listEmptyText()
		lines = append(lines, padLine(m.ui.faintPadLeft.Render(empty), width))
		for len(lines) < height {
			lines = append(lines, blank)
		}
		return lines
	}

	m.listScroll = clampScroll(m.listScroll, m.listCursor, len(rows), contentH)

	minuteBucket := m.now().Unix() / 60
	if m.listRowCache.entries == nil ||
		m.listRowCache.width != width ||
		m.listRowCache.listRev != m.listRev ||
		m.listRowCache.sandboxRev != m.sandboxRev ||
		m.listRowCache.minuteBucket != minuteBucket ||
		len(m.listRowCache.entries) >= 64 {
		m.listRowCache = listRowCacheState{
			width:        width,
			listRev:      m.listRev,
			sandboxRev:   m.sandboxRev,
			minuteBucket: minuteBucket,
			entries:      make(map[listRowCacheKey][]string, 16),
		}
	}
	cacheKey := listRowCacheKey{
		width:        width,
		listRev:      m.listRev,
		sandboxRev:   m.sandboxRev,
		selectedRepo: m.selectedRepo,
		activeScope:  m.activeScope,
		listFilter:   m.listFilter,
		filePath:     m.filePath,
		rowsLen:      len(rows),
		minuteBucket: minuteBucket,
	}
	unselected, ok := m.listRowCache.entries[cacheKey]
	if !ok || len(unselected) != len(rows) {
		unselected = make([]string, len(rows))
		m.listRowCache.entries[cacheKey] = unselected
	}

	for i := 0; i < contentH; i++ {
		idx := m.listScroll + i
		if idx >= len(rows) {
			lines = append(lines, blank)
			continue
		}
		r := rows[idx]
		selected := idx == m.listCursor
		if !selected {
			cached := unselected[idx]
			if cached == "" {
				cached = m.renderListRow(r, width, false, false)
				unselected[idx] = cached
			}
			lines = append(lines, cached)
			continue
		}
		lines = append(lines, m.renderListRow(r, width, true, focused))
	}
	return lines
}

func (m *Model) listEmptyText() string {
	if m.selectedRepo == "" {
		return "Select a repository"
	}
	if m.loadingScope[strings.ToLower(m.selectedRepo)+":"+m.activeScope.String()] {
		return "Loading " + strings.ToLower(m.activeScope.String()) + "…"
	}
	if m.listFilter != "" {
		return "No matches for " + fmt.Sprintf("%q", m.listFilter)
	}
	return "No " + strings.ToLower(m.activeScope.String())
}

type scopeTabSpan struct {
	scope Scope
	start int
	end   int
}

func (m *Model) scopeTabSpans() ([]scopeTabSpan, string) {
	return m.ui.scopeTabSpans[m.activeScope], m.ui.scopeTabStrip[m.activeScope]
}

func (m *Model) renderScopeTabs(width int) string {
	return padLine(m.ui.scopeTabStrip[m.activeScope], width)
}

func (m *Model) scopeTabAt(relX int) (Scope, bool) {
	for _, sp := range m.ui.scopeTabSpans[m.activeScope] {
		if relX >= sp.start && relX < sp.end {
			return sp.scope, true
		}
	}
	return ScopeBranches, false
}

func (m *Model) renderListRow(r listRow, width int, selected, paneFocused bool) string {
	switch r.kind {
	case listHeading:
		s := m.ui.faintBold.Render(" " + r.label)
		return padLine(s, width)

	case listBranch:
		b := r.branch
		icon := m.ui.branchIcon
		if b.IsDefault {
			icon = m.ui.branchDefaultIcon
		}
		var meta []string
		if _, ok := sandboxForBranch(m.sandboxes, m.selectedRepo, b.Name); ok {
			meta = append(meta, m.ui.dotSuccess)
		}
		if b.Protected {
			meta = append(meta, m.ui.lockWarning)
		}
		if b.LinkedPR != nil {
			meta = append(meta, m.ui.openPR.Render(fmt.Sprintf("#%d", b.LinkedPR.Number)))
		}
		if !b.UpdatedAt.IsZero() {
			meta = append(meta, m.ui.faint.Render(m.relTime(b.UpdatedAt)))
		}
		return m.renderTwoColRow(" "+icon+" ", b.Name, strings.Join(meta, " "), width, selected, paneFocused)

	case listPR:
		pr := r.pr
		glyph := m.prStateGlyph(*pr)
		num := m.ui.faint.Render(fmt.Sprintf("#%d", pr.Number))
		var meta []string
		if _, ok := sandboxForPR(m.sandboxes, m.selectedRepo, pr.Number); ok {
			meta = append(meta, m.ui.dotSuccess)
		}
		if ci := m.checksGlyph(pr.ChecksState); ci != "" {
			meta = append(meta, ci)
		}
		if !pr.UpdatedAt.IsZero() {
			meta = append(meta, m.ui.faint.Render(m.relTime(pr.UpdatedAt)))
		}
		return m.renderTwoColRow(" "+glyph+" "+num+" ", pr.Title, strings.Join(meta, " "), width, selected, paneFocused)

	case listIssue:
		iss := r.issue
		glyph := m.issueStateGlyph(*iss)
		num := m.ui.faint.Render(fmt.Sprintf("#%d", iss.Number))
		var meta []string
		if _, ok := sandboxForIssue(m.sandboxes, m.selectedRepo, iss.Number); ok {
			meta = append(meta, m.ui.dotSuccess)
		}
		if iss.CommentsCount > 0 {
			meta = append(meta, m.ui.faint.Render(fmt.Sprintf("%s%d", constants.CommentIcon, iss.CommentsCount)))
		}
		if !iss.UpdatedAt.IsZero() {
			meta = append(meta, m.ui.faint.Render(m.relTime(iss.UpdatedAt)))
		}
		return m.renderTwoColRow(" "+glyph+" "+num+" ", iss.Title, strings.Join(meta, " "), width, selected, paneFocused)

	case listRun:
		run := r.run
		glyph := m.runStatusGlyph(run.Status, run.Conclusion)
		title := run.DisplayTitle
		if title == "" {
			title = run.Name
		}
		var meta []string
		if run.Branch != "" {
			meta = append(meta, m.ui.faint.Render(ansi.Truncate(run.Branch, 12, "…")))
		}
		if !run.UpdatedAt.IsZero() {
			meta = append(meta, m.ui.faint.Render(m.relTime(run.UpdatedAt)))
		}
		return m.renderTwoColRow(" "+glyph+" ", title, strings.Join(meta, " "), width, selected, paneFocused)

	case listParentDir:
		return m.renderTwoColRow(" "+m.ui.parentDirIcon+" ", "..", m.ui.parentMeta, width, selected, paneFocused)

	case listFile:
		e := r.entry
		icon := m.ui.fileIcon
		meta := ""
		if e.Type == "dir" {
			icon = m.ui.dirIcon
			meta = m.ui.dirMeta
		} else if e.Size > 0 {
			meta = m.ui.faint.Render(formatBytes(e.Size))
		}
		return m.renderTwoColRow(" "+icon+" ", e.Name, meta, width, selected, paneFocused)
	}
	return strings.Repeat(" ", width)
}

func (m *Model) renderTwoColRow(prefix, mainText, rightMeta string, width int, selected, paneFocused bool) string {
	mainStyle := m.ui.primary
	if selected && paneFocused {
		mainStyle = m.ui.primaryBold
	}
	suffix := ""
	suffixW := 0
	if rightMeta != "" {
		suffix = " " + rightMeta + " "
		suffixW = ansi.StringWidth(suffix)
	}
	prefixW := ansi.StringWidth(prefix)
	avail := width - prefixW - suffixW
	if avail < 4 {
		// Drop right meta on very narrow panes so title remains readable.
		suffix = ""
		suffixW = 0
		avail = max(1, width-prefixW-1)
	}
	truncated := mainText
	mainW := ansi.StringWidth(truncated)
	if mainW > avail {
		truncated = ansi.Truncate(mainText, avail, "…")
		mainW = ansi.StringWidth(truncated)
	}
	line := prefix + mainStyle.Render(truncated)
	totalW := prefixW + mainW + suffixW
	if totalW <= width {
		return m.paintExactRow(line+strings.Repeat(" ", width-totalW)+suffix, selected)
	}
	return m.paintRow(line+suffix, width, selected)
}

func (m *Model) prStateGlyph(pr PullRequest) string {
	switch strings.ToUpper(pr.State) {
	case "MERGED":
		return m.ui.prMergedIcon
	case "CLOSED":
		return m.ui.prClosedIcon
	default:
		if pr.IsDraft {
			return m.ui.prDraftIcon
		}
		return m.ui.prOpenIcon
	}
}

func (m *Model) issueStateGlyph(iss Issue) string {
	if strings.EqualFold(iss.State, "CLOSED") {
		return m.ui.issueClosedIcon
	}
	return m.ui.issueOpenIcon
}

func (m *Model) checksGlyph(state string) string {
	switch strings.ToUpper(state) {
	case "SUCCESS":
		return m.ui.checksSuccessIcon
	case "FAILURE", "ERROR":
		return m.ui.checksFailureIcon
	case "PENDING", "EXPECTED":
		return m.ui.checksWaitingIcon
	default:
		return ""
	}
}

func (m *Model) runStatusGlyph(status, conclusion string) string {
	if strings.ToLower(status) != "completed" {
		return m.ui.runWaitingIcon
	}
	switch strings.ToLower(conclusion) {
	case "success":
		return m.ui.runSuccessIcon
	case "failure", "timed_out":
		return m.ui.runFailureIcon
	case "cancelled", "skipped":
		return m.ui.runCancelledIcon
	default:
		return m.ui.runActionReqIcon
	}
}

func (m *Model) relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := m.now().Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

func formatBytes(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

func parentDirPath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" || !strings.Contains(p, "/") {
		return ""
	}
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}
