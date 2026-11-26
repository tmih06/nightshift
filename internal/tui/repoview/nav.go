package repoview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type navRowKind int

const (
	navHeading navRowKind = iota
	navRepo
	navScope
)

type navRow struct {
	kind         navRowKind
	label        string
	repoFullName string
	repo         *Repo
	scope        Scope
	pinned       bool
	expanded     bool
}

func (m *Model) navRows() []navRow {
	key := navRowsKey{
		rev:         m.navRev,
		viewer:      m.viewer,
		navFilter:   m.navFilter,
		reposLen:    len(m.repos),
		pinsLen:     len(m.pins),
		expandedLen: len(m.expandedRepos),
	}
	if m.cachedNavValid && m.cachedNavKey == key {
		return m.cachedNavRows
	}

	m.ensureRepoIndexSynced()
	filter := strings.ToLower(strings.TrimSpace(m.navFilter))

	matchesFilter := func(r Repo) bool {
		if filter == "" {
			return true
		}
		hay := strings.ToLower(r.NormalizedFullName() + " " + r.Description + " " + r.Language)
		return strings.Contains(hay, filter)
	}

	var pinnedSet map[string]struct{}
	if len(m.pins) > 0 {
		pinnedSet = make(map[string]struct{}, len(m.pins))
	}
	var pinnedRepos []Repo
	for _, p := range m.pins {
		pKey := strings.ToLower(p)
		pinnedSet[pKey] = struct{}{}
		var r Repo
		if idx, ok := m.repoIndex[pKey]; ok && idx >= 0 && idx < len(m.repos) {
			r = m.repos[idx]
		} else {
			owner, name, _ := strings.Cut(p, "/")
			r = Repo{Owner: owner, Name: name, FullName: p, URL: "https://github.com/" + p}
		}
		if matchesFilter(r) {
			pinnedRepos = append(pinnedRepos, r)
		}
	}

	otherRepos := make([]Repo, 0, len(m.repos))
	for _, r := range m.repos {
		if len(pinnedSet) > 0 {
			rKey := strings.ToLower(r.NormalizedFullName())
			if _, isPinned := pinnedSet[rKey]; isPinned {
				continue
			}
		}
		if matchesFilter(r) {
			otherRepos = append(otherRepos, r)
		}
	}

	rows := make([]navRow, 0, len(pinnedRepos)+len(otherRepos)+2+len(m.expandedRepos)*len(allScopes))
	appendRepo := func(r Repo, pinned bool) {
		fn := r.NormalizedFullName()
		rCopy := r
		exp := m.expandedRepos[strings.ToLower(fn)]
		rows = append(rows, navRow{
			kind:         navRepo,
			label:        m.displayRepoName(r),
			repoFullName: fn,
			repo:         &rCopy,
			pinned:       pinned,
			expanded:     exp,
		})
		if exp {
			for _, sc := range allScopes {
				rows = append(rows, navRow{
					kind:         navScope,
					label:        sc.String(),
					repoFullName: fn,
					repo:         &rCopy,
					scope:        sc,
					pinned:       pinned,
				})
			}
		}
	}

	if len(pinnedRepos) > 0 {
		rows = append(rows, navRow{kind: navHeading, label: fmt.Sprintf("PINNED (%d)", len(pinnedRepos))})
		for _, r := range pinnedRepos {
			appendRepo(r, true)
		}
	}
	if len(otherRepos) > 0 {
		rows = append(rows, navRow{kind: navHeading, label: fmt.Sprintf("REPOSITORIES (%d)", len(otherRepos))})
		for _, r := range otherRepos {
			appendRepo(r, false)
		}
	}

	m.cachedNavRows = rows
	m.cachedNavKey = key
	m.cachedNavValid = true
	return rows
}

func (m *Model) displayRepoName(r Repo) string {
	fn := r.NormalizedFullName()
	if m.viewer != "" && strings.EqualFold(r.Owner, m.viewer) && r.Name != "" {
		return r.Name
	}
	return fn
}

func (m *Model) ensureNavCursorValid() {
	rows := m.navRows()
	if len(rows) == 0 {
		m.navCursor = 0
		m.navScroll = 0
		return
	}
	if m.navCursor < 0 {
		m.navCursor = 0
	}
	if m.navCursor >= len(rows) {
		m.navCursor = len(rows) - 1
	}
	if rows[m.navCursor].kind == navHeading {
		// Try moving down first, then up.
		for i := m.navCursor + 1; i < len(rows); i++ {
			if rows[i].kind != navHeading {
				m.navCursor = i
				return
			}
		}
		for i := m.navCursor - 1; i >= 0; i-- {
			if rows[i].kind != navHeading {
				m.navCursor = i
				return
			}
		}
	}
}

func (m *Model) moveNavCursor(delta int) {
	rows := m.navRows()
	if len(rows) == 0 {
		return
	}
	m.ensureNavCursorValid()
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	cur := m.navCursor
	for range delta {
		next := cur + step
		for next >= 0 && next < len(rows) && rows[next].kind == navHeading {
			next += step
		}
		if next < 0 || next >= len(rows) {
			break
		}
		cur = next
	}
	m.navCursor = cur
	m.syncSelectionFromNav()
}

func (m *Model) navFirst() {
	rows := m.navRows()
	for i, r := range rows {
		if r.kind != navHeading {
			m.navCursor = i
			m.syncSelectionFromNav()
			return
		}
	}
}

func (m *Model) navLast() {
	rows := m.navRows()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].kind != navHeading {
			m.navCursor = i
			m.syncSelectionFromNav()
			return
		}
	}
}

func (m *Model) currentNavRow() (navRow, bool) {
	rows := m.navRows()
	m.ensureNavCursorValid()
	if m.navCursor < 0 || m.navCursor >= len(rows) {
		return navRow{}, false
	}
	if rows[m.navCursor].kind == navHeading {
		return navRow{}, false
	}
	return rows[m.navCursor], true
}

func (m *Model) syncSelectionFromNav() {
	row, ok := m.currentNavRow()
	if !ok {
		return
	}
	if row.repoFullName != "" && !strings.EqualFold(row.repoFullName, m.selectedRepo) {
		m.selectedRepo = row.repoFullName
		m.filePath = ""
		m.listCursor = 0
		m.listScroll = 0
		m.detailsScroll = 0
		m.invalidateList()
	}
	if row.kind == navScope {
		if m.activeScope != row.scope {
			m.activeScope = row.scope
			m.listCursor = 0
			m.listScroll = 0
			m.detailsScroll = 0
			m.invalidateList()
		}
	}
}

func (m *Model) selectRepoInNav(fullName string) {
	rows := m.navRows()
	for i, r := range rows {
		if r.kind == navRepo && strings.EqualFold(r.repoFullName, fullName) {
			m.navCursor = i
			m.selectedRepo = r.repoFullName
			return
		}
	}
}

func (m *Model) selectRepoScopeInNav(fullName string, sc Scope) {
	rows := m.navRows()
	for i, r := range rows {
		if r.kind == navScope && strings.EqualFold(r.repoFullName, fullName) && r.scope == sc {
			m.navCursor = i
			m.selectedRepo = r.repoFullName
			m.activeScope = sc
			return
		}
	}
}

func (m *Model) isPinned(fullName string) bool {
	for _, p := range m.pins {
		if strings.EqualFold(p, fullName) {
			return true
		}
	}
	return false
}

func (m *Model) togglePin(fullName string) (pinned bool, err error) {
	if fullName == "" {
		return false, nil
	}
	for i, p := range m.pins {
		if strings.EqualFold(p, fullName) {
			m.pins = append(m.pins[:i], m.pins[i+1:]...)
			m.invalidateNav()
			return false, savePins(m.pinsPath, m.pins)
		}
	}
	m.pins = append(m.pins, fullName)
	m.invalidateNav()
	return true, savePins(m.pinsPath, m.pins)
}

func (m *Model) reorderPin(fullName string, delta int) (bool, error) {
	idx := -1
	for i, p := range m.pins {
		if strings.EqualFold(p, fullName) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	target := idx + delta
	if target < 0 || target >= len(m.pins) {
		return false, nil
	}
	m.pins[idx], m.pins[target] = m.pins[target], m.pins[idx]
	m.invalidateNav()
	err := savePins(m.pinsPath, m.pins)
	m.selectRepoInNav(fullName)
	return true, err
}

func repoGlyphIndex(r Repo) int {
	switch {
	case r.IsArchived:
		return 3
	case r.IsFork:
		return 2
	case r.IsPrivate:
		return 1
	default:
		return 0
	}
}

func (m *Model) scopeCountBadge(repo string, sc Scope) string {
	rc := m.cache[strings.ToLower(repo)]
	if rc == nil {
		return ""
	}
	switch sc {
	case ScopeBranches:
		if rc.branchesLoaded {
			return strconv.Itoa(len(rc.branches))
		}
	case ScopePRs:
		if rc.prsLoaded {
			return strconv.Itoa(len(rc.prs))
		}
	case ScopeIssues:
		if rc.issuesLoaded {
			return strconv.Itoa(len(rc.issues))
		}
	case ScopeActions:
		if rc.runsLoaded {
			return strconv.Itoa(len(rc.runs))
		}
	}
	return ""
}

func (m *Model) renderNavPane(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	focused := m.activePane == PaneNav
	lines := make([]string, 0, height)
	lines = append(lines, m.renderPaneHeader(1, "Repos", "", width, focused))

	if height == 1 {
		return lines
	}

	// Row 1: flat search bar.
	var inputView string
	if m.searchMode == SearchNav {
		m.navSearchInput.SetWidth(max(4, width-ansi.StringWidth(m.navSearchInput.Prompt)-2))
		inputView = m.navSearchInput.View()
	}
	lines = append(lines, m.renderSearchBar(width, m.searchMode == SearchNav, inputView, m.navFilter, "/ Filter repos…"))
	if height == 2 {
		return lines
	}

	// Row 2: faint separator rule.
	lines = append(lines, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	contentH := height - len(lines)
	if contentH <= 0 {
		return lines[:height]
	}

	rows := m.navRows()
	m.ensureNavCursorValid()

	blank := strings.Repeat(" ", width)
	if len(rows) == 0 {
		empty := "No repositories"
		if m.loadingRepos {
			empty = "Loading repositories…"
		} else if m.navFilter != "" {
			empty = "No matching repos"
		}
		lines = append(lines, padLine(m.ui.faintPadLeft.Render(empty), width))
		for len(lines) < height {
			lines = append(lines, blank)
		}
		return lines
	}

	m.navScroll = clampScroll(m.navScroll, m.navCursor, len(rows), contentH)

	cacheKey := navRowCacheKey{
		width:       width,
		navRev:      m.navRev,
		sandboxRev:  m.sandboxRev,
		viewer:      m.viewer,
		navFilter:   m.navFilter,
		reposLen:    len(m.repos),
		pinsLen:     len(m.pins),
		expandedLen: len(m.expandedRepos),
		rowsLen:     len(rows),
	}
	if !m.navRowCache.valid || m.navRowCache.key != cacheKey {
		m.navRowCache = navRowCacheState{
			valid:      true,
			key:        cacheKey,
			unselected: make([]string, len(rows)),
		}
	}

	for i := 0; i < contentH; i++ {
		idx := m.navScroll + i
		if idx >= len(rows) {
			lines = append(lines, blank)
			continue
		}
		r := rows[idx]
		selected := idx == m.navCursor
		if !selected && (r.kind != navScope || !(strings.EqualFold(r.repoFullName, m.selectedRepo) && r.scope == m.activeScope)) {
			cached := m.navRowCache.unselected[idx]
			if cached == "" {
				cached = m.renderNavRow(r, width, false, false)
				m.navRowCache.unselected[idx] = cached
			}
			lines = append(lines, cached)
			continue
		}
		lines = append(lines, m.renderNavRow(r, width, selected, focused))
	}

	return lines
}

func (m *Model) renderNavRow(r navRow, width int, selected, paneFocused bool) string {
	switch r.kind {
	case navHeading:
		s := m.ui.faintBold.Render(" " + r.label)
		return padLine(s, width)

	case navRepo:
		chev := m.ui.chevClosedFaint
		if r.expanded {
			chev = m.ui.chevOpenFaint
		}
		glyphIdx := 0
		if r.repo != nil {
			glyphIdx = repoGlyphIndex(*r.repo)
		}
		glyph := m.ui.repoGlyphNorm[glyphIdx]
		nameStyle := m.ui.primary
		if selected {
			nameStyle = m.ui.primaryBold
			if paneFocused {
				if r.expanded {
					chev = m.ui.chevOpenFocus
				} else {
					chev = m.ui.chevClosedFocus
				}
				glyph = m.ui.repoGlyphFocus[glyphIdx]
			}
		}

		prefix := " " + chev + " " + glyph + " "
		prefixW := 1 + m.ui.chevW + 1 + m.ui.repoGlyphW[glyphIdx] + 1
		suffix := ""
		suffixW := 0
		if hasSandboxForRepo(m.sandboxes, r.repoFullName) {
			suffix = " " + m.ui.dotSuccess
			suffixW = 1 + m.ui.dotSuccessW
		}
		avail := max(1, width-prefixW-suffixW-1)
		name := r.label
		nameW := ansi.StringWidth(name)
		if nameW > avail {
			name = ansi.Truncate(name, avail, "…")
			nameW = ansi.StringWidth(name)
		}
		line := prefix + nameStyle.Render(name)
		totalW := prefixW + nameW + suffixW
		if totalW <= width {
			return m.paintExactRow(line+strings.Repeat(" ", width-totalW)+suffix, selected)
		}
		return m.paintRow(line+suffix, width, selected)

	case navScope:
		active := strings.EqualFold(r.repoFullName, m.selectedRepo) && r.scope == m.activeScope
		icon := m.ui.scopeIconFaint[r.scope]
		labelStyle := m.ui.secondary
		if active || selected {
			icon = m.ui.scopeIconSuccess[r.scope]
			labelStyle = m.ui.primaryBold
		}
		prefix := "     " + icon + " "
		prefixW := 5 + m.ui.scopeIconW[r.scope] + 1
		badge := m.scopeCountBadge(r.repoFullName, r.scope)
		suffix := ""
		suffixW := 0
		if badge != "" {
			suffix = " " + m.ui.faint.Render(badge) + " "
			suffixW = 1 + len(badge) + 1
		}
		avail := max(1, width-prefixW-suffixW)
		label := r.label
		labelW := ansi.StringWidth(label)
		if labelW > avail {
			label = ansi.Truncate(label, avail, "…")
			labelW = ansi.StringWidth(label)
		}
		line := prefix + labelStyle.Render(label)
		totalW := prefixW + labelW + suffixW
		if totalW <= width {
			return m.paintExactRow(line+strings.Repeat(" ", width-totalW)+suffix, selected)
		}
		return m.paintRow(line+suffix, width, selected)
	}
	return strings.Repeat(" ", width)
}

func (m *Model) renderSearchBar(width int, active bool, inputView, filterVal, placeholder string) string {
	var text string
	switch {
	case active:
		text = " " + inputView
		return m.paintRow(text, width, true)
	case filterVal != "":
		text = " " + m.ui.slashActive + " " + m.ui.primary.Render(filterVal)
	default:
		text = " " + m.ui.faint.Render(placeholder)
	}
	return padLine(text, width)
}
