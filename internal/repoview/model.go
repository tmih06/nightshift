package repoview

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	ghctx "github.com/tmih06/nightshift/internal/ghdash/tui/context"
	ghmd "github.com/tmih06/nightshift/internal/ghdash/tui/markdown"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/zenocto/tui/syntax"
	ztheme "github.com/tmih06/nightshift/internal/zenocto/tui/theme"
)

const (
	apiTimeout           = 25 * time.Second
	noticeDuration       = 5 * time.Second
	doubleClickDelay     = 400 * time.Millisecond
	defaultFetchDebounce = 120 * time.Millisecond
)

type navRowsKey struct {
	rev         uint64
	viewer      string
	navFilter   string
	reposLen    int
	pinsLen     int
	expandedLen int
}

type listRowsKey struct {
	rev          uint64
	selectedRepo string
	activeScope  Scope
	listFilter   string
	filePath     string
}

type navRowCacheKey struct {
	width       int
	navRev      uint64
	sandboxRev  uint64
	viewer      string
	navFilter   string
	reposLen    int
	pinsLen     int
	expandedLen int
	rowsLen     int
}

type navRowCacheState struct {
	valid      bool
	key        navRowCacheKey
	unselected []string
}

type listRowCacheKey struct {
	width        int
	listRev      uint64
	sandboxRev   uint64
	selectedRepo string
	activeScope  Scope
	listFilter   string
	filePath     string
	rowsLen      int
	minuteBucket int64
}

type listRowCacheState struct {
	width        int
	listRev      uint64
	sandboxRev   uint64
	minuteBucket int64
	entries      map[listRowCacheKey][]string
}

type detailsCacheKey struct {
	width           int
	selectedRepo    string
	showingOverview bool
	activeScope     Scope
	listCursor      int
	filePath        string
	dataRev         uint64
	sandboxRev      uint64
	fullReadme      bool
	minuteBucket    int64
}

type detailsCacheEntry struct {
	lines           []string
	readme          string
	overviewLoading bool
}

type detailsCacheState struct {
	width        int
	dataRev      uint64
	sandboxRev   uint64
	minuteBucket int64
	entries      map[detailsCacheKey]detailsCacheEntry
}

type footerCacheKey struct {
	width        int
	selectedRepo string
	viewer       string
	activePane   Pane
	activeScope  Scope
	notice       string
	noticeErr    bool
	showHelp     bool
}

type footerCacheState struct {
	valid bool
	key   footerCacheKey
	lines []string
}

type frameCacheKey struct {
	viewRev         uint64
	dataRev         uint64
	navRev          uint64
	listRev         uint64
	sandboxRev      uint64
	w, h            int
	activePane      Pane
	activeScope     Scope
	selectedRepo    string
	navCursor       int
	navScroll       int
	listCursor      int
	listScroll      int
	detailsScroll   int
	detailsOverview bool
	filePath        string
	navFrac         float64
	listFrac        float64
	hideNav         bool
	hideDetails     bool
	zoomDetails     bool
	draggingDiv     int
	showHelp        bool
	notice          string
	noticeErr       bool
	viewer          string
	navFilter       string
	listFilter      string
	reposLen        int
	expandedLen     int
	pinsLen         int
	minuteBucket    int64
}

type frameCacheState struct {
	valid   bool
	key     frameCacheKey
	content string
}

type repoCache struct {
	overviewLoaded  bool
	overviewLoading bool

	branches       []Branch
	branchesLoaded bool
	commits        map[string][]Commit
	commitsLoading map[string]bool

	prs       []PullRequest
	prsLoaded bool

	issues               []Issue
	issuesLoaded         bool
	issueComments        map[int][]IssueComment
	issueCommentsLoading map[int]bool

	runs        []WorkflowRun
	runsLoaded  bool
	jobs        map[int64][]WorkflowJob
	jobsLoading map[int64]bool

	dirs         map[string][]TreeEntry
	dirsLoading  map[string]bool
	files        map[string]FileContent
	filesLoading map[string]bool
}

func newRepoCache() *repoCache {
	return &repoCache{
		commits:              make(map[string][]Commit),
		commitsLoading:       make(map[string]bool),
		issueComments:        make(map[int][]IssueComment),
		issueCommentsLoading: make(map[int]bool),
		jobs:                 make(map[int64][]WorkflowJob),
		jobsLoading:          make(map[int64]bool),
		dirs:                 make(map[string][]TreeEntry),
		dirsLoading:          make(map[string]bool),
		files:                make(map[string]FileContent),
		filesLoading:         make(map[string]bool),
	}
}

// Model is the 3-pane repository workspace Bubble Tea model.
type Model struct {
	theme  ghtheme.Theme
	styles ghctx.Styles
	ui     uiStyles
	syntax syntax.Syntax
	client Client

	width  int
	height int

	loaded       bool
	loadingRepos bool
	viewer       string
	currentRepo  string

	pinsPath      string
	pins          []string
	repos         []Repo
	repoIndex     map[string]int
	expandedRepos map[string]bool
	cache         map[string]*repoCache
	loadingScope  map[string]bool

	sandboxes []ActiveSandbox

	// Debounced fetch state.
	fetchSeq         uint64
	fetchDebounce    time.Duration
	fetchTimerActive bool
	fetchDeadline    time.Time

	// Revision counters and derived/render caches.
	navRev              uint64
	listRev             uint64
	dataRev             uint64
	sandboxRev          uint64
	viewRev             uint64
	cachedNavRows       []navRow
	cachedNavKey        navRowsKey
	cachedNavValid      bool
	cachedListRows      []listRow
	cachedListItemCount int
	cachedListKey       listRowsKey
	cachedListValid     bool
	navRowCache         navRowCacheState
	listRowCache        listRowCacheState
	detailsCache        detailsCacheState
	footerCache         footerCacheState
	frameCache          frameCacheState

	// Navigation & selection state.
	activePane      Pane
	activeScope     Scope
	selectedRepo    string
	navCursor       int
	navScroll       int
	listCursor      int
	listScroll      int
	detailsScroll   int
	detailsOverview bool
	filePath        string

	// Layout state.
	navFrac     float64
	listFrac    float64
	hideNav     bool
	hideDetails bool
	zoomDetails bool
	draggingDiv int

	// Search state.
	searchMode      SearchMode
	navSearchInput  textinput.Model
	listSearchInput textinput.Model
	navFilter       string
	listFilter      string

	// Modal state.
	modal            ModalKind
	modalBounds      modalBounds
	modalErr         string
	paletteInput     textinput.Model
	paletteCursor    int
	addRepoInput     textinput.Model
	addRepoResults   []Repo
	addRepoCursor    int
	addRepoSearching bool
	branchInput      textinput.Model
	branchBase       string
	issueTitleInput  textinput.Model
	issueBodyInput   textinput.Model
	issueFocusBody   bool
	commentInput     textinput.Model
	commentIssueNum  int
	confirm          *confirmAction
	confirmCursor    int

	// Help & notice state.
	showHelp  bool
	notice    string
	noticeErr bool
	noticeAt  time.Time

	// Double-click tracking.
	lastClickPane Pane
	lastClickRow  int
	lastClickAt   time.Time

	// Injectable helpers for testing.
	openBrowser   func(url string) error
	copyClipboard func(text string) error
	now           func() time.Time
	noticeTTL     time.Duration
}

// New constructs a new repoview Model.
func New(th ghtheme.Theme, styles ghctx.Styles, opts Options) *Model {
	pinsPath := opts.PinsPath
	if pinsPath == "" {
		pinsPath = defaultPinsPath()
	}
	pins, _ := loadPins(pinsPath)

	syn, _ := syntax.New(ztheme.SyntaxDark)

	m := &Model{
		theme:           th,
		styles:          styles,
		ui:              newUIStyles(th, styles),
		syntax:          syn,
		client:          opts.Client,
		currentRepo:     strings.TrimSpace(opts.CurrentRepo),
		pinsPath:        pinsPath,
		pins:            pins,
		repoIndex:       make(map[string]int),
		expandedRepos:   make(map[string]bool),
		cache:           make(map[string]*repoCache),
		loadingScope:    make(map[string]bool),
		fetchDebounce:   defaultFetchDebounce,
		activePane:      PaneNav,
		activeScope:     ScopeBranches,
		navFrac:         defaultNavFrac,
		listFrac:        defaultListFrac,
		navSearchInput:  newStyledInput(th, "/ ", "Filter repos…", 100),
		listSearchInput: newStyledInput(th, "/ ", "Filter…", 100),
		paletteInput:    newStyledInput(th, ": ", "Type a command…", 100),
		addRepoInput:    newStyledInput(th, "▸ ", "owner/repo or search query…", 140),
		branchInput:     newStyledInput(th, "▸ ", "feature/my-branch", 120),
		issueTitleInput: newStyledInput(th, "▸ ", "Issue title…", 200),
		issueBodyInput:  newStyledInput(th, "▸ ", "Description…", 500),
		commentInput:    newStyledInput(th, "▸ ", "Write a comment…", 500),
		openBrowser:     defaultOpenBrowser,
		copyClipboard:   clipboard.WriteAll,
		now:             time.Now,
		noticeTTL:       noticeDuration,
	}

	if m.currentRepo != "" {
		m.selectedRepo = m.currentRepo
		m.expandedRepos[strings.ToLower(m.currentRepo)] = true
		owner, name, _ := strings.Cut(m.currentRepo, "/")
		m.repos = append(m.repos, Repo{
			Owner:    owner,
			Name:     name,
			FullName: m.currentRepo,
			URL:      "https://github.com/" + m.currentRepo,
		})
		m.repoIndex[strings.ToLower(m.currentRepo)] = 0
	} else if len(m.pins) > 0 {
		m.selectedRepo = m.pins[0]
		m.expandedRepos[strings.ToLower(m.pins[0])] = true
	}

	return m
}

func newStyledInput(th ghtheme.Theme, prompt, placeholder string, charLimit int) textinput.Model {
	ti := textinput.New()
	ti.SetVirtualCursor(true)
	ti.Prompt = prompt
	ti.Placeholder = placeholder
	ti.CharLimit = charLimit
	base := lipgloss.NewStyle()
	ti.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{
			Prompt:      base.Foreground(th.SuccessText).Bold(true),
			Text:        base.Foreground(th.PrimaryText),
			Placeholder: base.Foreground(th.FaintText),
		},
		Blurred: textinput.StyleState{
			Prompt:      base.Foreground(th.FaintText),
			Text:        base.Foreground(th.SecondaryText),
			Placeholder: base.Foreground(th.FaintText),
		},
		Cursor: textinput.CursorStyle{
			Color: th.PrimaryText,
			Shape: tea.CursorBar,
			Blink: false,
		},
	})
	return ti
}

// Init returns no initial command so repo loading happens lazily via EnsureLoaded.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Capturing reports whether repoview currently holds the keyboard for a modal,
// search bar, or help overlay, so the host tab bar won't hijack Tab.
func (m *Model) Capturing() bool {
	return m.modal != ModalNone || m.searchMode != SearchNone || m.showHelp
}

// SetSandboxes updates the active cluster sandboxes used to badge items and
// jump directly to live sandboxes.
func (m *Model) SetSandboxes(boxes []ActiveSandbox) {
	if len(boxes) == len(m.sandboxes) {
		same := true
		for i := range boxes {
			if boxes[i] != m.sandboxes[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	cp := make([]ActiveSandbox, len(boxes))
	copy(cp, boxes)
	m.sandboxes = cp
	m.sandboxRev++
	m.viewRev++
}

func (m *Model) invalidateNav() {
	m.navRev++
	m.viewRev++
}

func (m *Model) invalidateList() {
	m.listRev++
	m.viewRev++
}

func (m *Model) invalidateData() {
	m.dataRev++
	m.viewRev++
}

func (m *Model) invalidateView() {
	m.viewRev++
}

func (m *Model) cancelPendingFetch() {
	m.fetchSeq++
	m.fetchTimerActive = false
}

func (m *Model) currentFrameCacheKey() frameCacheKey {
	return frameCacheKey{
		viewRev:         m.viewRev,
		dataRev:         m.dataRev,
		navRev:          m.navRev,
		listRev:         m.listRev,
		sandboxRev:      m.sandboxRev,
		w:               m.width,
		h:               m.height,
		activePane:      m.activePane,
		activeScope:     m.activeScope,
		selectedRepo:    m.selectedRepo,
		navCursor:       m.navCursor,
		navScroll:       m.navScroll,
		listCursor:      m.listCursor,
		listScroll:      m.listScroll,
		detailsScroll:   m.detailsScroll,
		detailsOverview: m.detailsOverview,
		filePath:        m.filePath,
		navFrac:         m.navFrac,
		listFrac:        m.listFrac,
		hideNav:         m.hideNav,
		hideDetails:     m.hideDetails,
		zoomDetails:     m.zoomDetails,
		draggingDiv:     m.draggingDiv,
		showHelp:        m.showHelp,
		notice:          m.notice,
		noticeErr:       m.noticeErr,
		viewer:          m.viewer,
		navFilter:       m.navFilter,
		listFilter:      m.listFilter,
		reposLen:        len(m.repos),
		expandedLen:     len(m.expandedRepos),
		pinsLen:         len(m.pins),
		minuteBucket:    m.now().Unix() / 60,
	}
}

func (m *Model) rebuildRepoIndex() {
	if m.repoIndex == nil {
		m.repoIndex = make(map[string]int, len(m.repos))
	} else {
		clear(m.repoIndex)
	}
	for i := range m.repos {
		fn := m.repos[i].NormalizedFullName()
		if fn != "" {
			m.repoIndex[strings.ToLower(fn)] = i
		}
	}
}

func (m *Model) ensureRepoIndexSynced() {
	if len(m.repoIndex) != len(m.repos) {
		m.rebuildRepoIndex()
	}
}

// SelectedRepo returns the currently selected repository ("owner/name").
func (m *Model) SelectedRepo() string {
	return m.selectedRepo
}

// EnsureLoaded lazily starts the initial viewer and repository list fetch on
// the first call.
func (m *Model) EnsureLoaded() tea.Cmd {
	if m.loaded {
		return nil
	}
	m.loaded = true
	if m.client == nil {
		c, err := NewGitHubClient()
		if err != nil {
			return m.setNotice("GitHub client unavailable: "+err.Error(), true)
		}
		m.client = c
	}
	m.loadingRepos = true
	cmds := []tea.Cmd{
		m.fetchViewerCmd(),
		m.fetchReposCmd(),
	}
	if m.selectedRepo != "" {
		cmds = append(cmds, m.ensureRepoOverviewCmd(m.selectedRepo), m.ensureActiveScopeLoadedCmd())
	}
	return tea.Batch(cmds...)
}

func (m *Model) getOrCreateCache(repo string) *repoCache {
	key := strings.ToLower(repo)
	rc, ok := m.cache[key]
	if !ok {
		rc = newRepoCache()
		m.cache[key] = rc
	}
	return rc
}

func (m *Model) findRepo(fullName string) Repo {
	if fullName != "" {
		m.ensureRepoIndexSynced()
		key := strings.ToLower(fullName)
		if idx, ok := m.repoIndex[key]; ok && idx >= 0 && idx < len(m.repos) {
			if strings.EqualFold(m.repos[idx].NormalizedFullName(), fullName) {
				return m.repos[idx]
			}
			// Slice was mutated out-of-band; rebuild once and retry.
			m.rebuildRepoIndex()
			if idx, ok = m.repoIndex[key]; ok && idx >= 0 && idx < len(m.repos) {
				return m.repos[idx]
			}
		}
	}
	owner, name, _ := strings.Cut(fullName, "/")
	return Repo{
		Owner:    owner,
		Name:     name,
		FullName: fullName,
		URL:      "https://github.com/" + fullName,
	}
}

func (m *Model) upsertRepo(r Repo) {
	fn := r.NormalizedFullName()
	if fn == "" {
		return
	}
	m.ensureRepoIndexSynced()
	key := strings.ToLower(fn)
	if idx, ok := m.repoIndex[key]; ok && idx >= 0 && idx < len(m.repos) && strings.EqualFold(m.repos[idx].NormalizedFullName(), fn) {
		// Preserve existing fields when incoming omits them.
		if r.Readme == "" && m.repos[idx].Readme != "" {
			r.Readme = m.repos[idx].Readme
		}
		m.repos[idx] = r
		m.invalidateNav()
		m.invalidateData()
		return
	}
	m.repoIndex[key] = len(m.repos)
	m.repos = append(m.repos, r)
	m.invalidateNav()
	m.invalidateData()
}

func (m *Model) fileRef() string {
	if m.selectedRepo == "" {
		return ""
	}
	return m.findRepo(m.selectedRepo).DefaultBranch
}

func (m *Model) dirCacheKey(ref, p string) string {
	return ref + ":" + strings.Trim(p, "/")
}

func (m *Model) defaultOrSelectedBranch() string {
	if m.activeScope == ScopeBranches {
		if row, ok := m.currentListRow(); ok && row.kind == listBranch && row.branch != nil {
			return row.branch.Name
		}
	}
	if r := m.findRepo(m.selectedRepo); r.DefaultBranch != "" {
		return r.DefaultBranch
	}
	return "main"
}

func (m *Model) setNotice(text string, isErr bool) tea.Cmd {
	at := m.now()
	m.notice = text
	m.noticeErr = isErr
	m.noticeAt = at
	ttl := m.noticeTTL
	if ttl <= 0 {
		ttl = noticeDuration
	}
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return noticeExpiredMsg{at: at}
	})
}

func defaultOpenBrowser(targetURL string) error {
	if targetURL == "" {
		return fmt.Errorf("nothing to open")
	}
	if _, err := exec.LookPath("gh"); err == nil {
		// Use gh browse only when no full URL; for full URLs use platform opener.
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			return exec.Command("gh", "browse", "-R", targetURL).Start()
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

// Async client commands.

func (m *Model) fetchViewerCmd() tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		login, err := c.Viewer(ctx)
		return viewerLoadedMsg{login: login, err: err}
	}
}

func (m *Model) fetchReposCmd() tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		repos, err := c.ListRepos(ctx, 50)
		return reposLoadedMsg{repos: repos, err: err}
	}
}

func (m *Model) needsRepoOverview(repo string) bool {
	if repo == "" || m.client == nil {
		return false
	}
	rc := m.cache[strings.ToLower(repo)]
	return rc == nil || (!rc.overviewLoaded && !rc.overviewLoading)
}

func (m *Model) needsActiveScopeLoad() bool {
	repo := m.selectedRepo
	if repo == "" || m.client == nil {
		return false
	}
	rc := m.cache[strings.ToLower(repo)]
	scopeKey := strings.ToLower(repo) + ":" + m.activeScope.String()
	switch m.activeScope {
	case ScopeBranches:
		if rc != nil && rc.branchesLoaded {
			return false
		}
		return !m.loadingScope[scopeKey]
	case ScopePRs:
		if rc != nil && rc.prsLoaded {
			return false
		}
		return !m.loadingScope[scopeKey]
	case ScopeIssues:
		if rc != nil && rc.issuesLoaded {
			return false
		}
		return !m.loadingScope[scopeKey]
	case ScopeActions:
		if rc != nil && rc.runsLoaded {
			return false
		}
		return !m.loadingScope[scopeKey]
	case ScopeFiles:
		ref := m.fileRef()
		dirKey := m.dirCacheKey(ref, m.filePath)
		if rc != nil {
			if _, ok := rc.dirs[dirKey]; ok || rc.dirsLoading[dirKey] {
				return false
			}
		}
		return !m.loadingScope[scopeKey]
	}
	return false
}

func (m *Model) needsSelectionDetails() bool {
	repo := m.selectedRepo
	if repo == "" || m.client == nil {
		return false
	}
	row, ok := m.currentListRow()
	if !ok {
		return false
	}
	rc := m.cache[strings.ToLower(repo)]
	switch row.kind {
	case listBranch:
		bName := row.branch.Name
		if rc != nil {
			if _, ok := rc.commits[bName]; ok || rc.commitsLoading[bName] {
				return false
			}
		}
		return true
	case listIssue:
		num := row.issue.Number
		if rc != nil {
			if _, ok := rc.issueComments[num]; ok || rc.issueCommentsLoading[num] {
				return false
			}
		}
		return true
	case listRun:
		id := row.run.ID
		if rc != nil {
			if _, ok := rc.jobs[id]; ok || rc.jobsLoading[id] {
				return false
			}
		}
		return true
	case listFile:
		if row.entry.Type == "dir" {
			return false
		}
		ref := m.fileRef()
		key := m.dirCacheKey(ref, row.entry.Path)
		if rc != nil {
			if _, ok := rc.files[key]; ok || rc.filesLoading[key] {
				return false
			}
		}
		return true
	}
	return false
}

func (m *Model) scheduleSelectionFetchCmd() tea.Cmd {
	if !m.needsRepoOverview(m.selectedRepo) && !m.needsActiveScopeLoad() && !m.needsSelectionDetails() {
		if m.fetchTimerActive {
			m.cancelPendingFetch()
		}
		return nil
	}
	delay := m.fetchDebounce
	if delay <= 0 {
		delay = defaultFetchDebounce
	}
	m.fetchDeadline = time.Now().Add(delay)
	if m.fetchTimerActive {
		return nil
	}
	m.fetchSeq++
	m.fetchTimerActive = true
	seq := m.fetchSeq
	return tea.Tick(delay, func(t time.Time) tea.Msg {
		return fetchDebounceMsg{seq: seq, at: t}
	})
}

func (m *Model) ensureRepoOverviewCmd(repo string) tea.Cmd {
	if repo == "" || m.client == nil {
		return nil
	}
	rc := m.getOrCreateCache(repo)
	if rc.overviewLoaded || rc.overviewLoading {
		return nil
	}
	rc.overviewLoading = true
	m.invalidateData()
	c := m.client
	detailsW := m.computeLayout().detailsW
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		r, err := c.GetRepoOverview(ctx, repo)
		if err == nil && r.Readme != "" && detailsW > 0 {
			preview, _ := trimReadmePreview(r.Readme, readmePreviewMaxLines)
			_, _ = ghmd.Render(preview, max(20, detailsW-2), nil)
		}
		return repoOverviewLoadedMsg{repo: repo, data: r, err: err}
	}
}

func (m *Model) ensureActiveScopeLoadedCmd() tea.Cmd {
	repo := m.selectedRepo
	if repo == "" || m.client == nil {
		return nil
	}
	rc := m.getOrCreateCache(repo)
	scopeKey := strings.ToLower(repo) + ":" + m.activeScope.String()

	switch m.activeScope {
	case ScopeBranches:
		if rc.branchesLoaded || m.loadingScope[scopeKey] {
			return m.ensureSelectionDetailsCmd()
		}
		m.loadingScope[scopeKey] = true
		c := m.client
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			branches, err := c.ListBranches(ctx, repo)
			return branchesLoadedMsg{repo: repo, branches: branches, err: err}
		}

	case ScopePRs:
		if rc.prsLoaded || m.loadingScope[scopeKey] {
			return nil
		}
		m.loadingScope[scopeKey] = true
		c := m.client
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			prs, err := c.ListPRs(ctx, repo, 50)
			return prsLoadedMsg{repo: repo, prs: prs, err: err}
		}

	case ScopeIssues:
		if rc.issuesLoaded || m.loadingScope[scopeKey] {
			return m.ensureSelectionDetailsCmd()
		}
		m.loadingScope[scopeKey] = true
		c := m.client
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			issues, err := c.ListIssues(ctx, repo, 50)
			return issuesLoadedMsg{repo: repo, issues: issues, err: err}
		}

	case ScopeActions:
		if rc.runsLoaded || m.loadingScope[scopeKey] {
			return m.ensureSelectionDetailsCmd()
		}
		m.loadingScope[scopeKey] = true
		c := m.client
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			runs, err := c.ListWorkflowRuns(ctx, repo, 30)
			return runsLoadedMsg{repo: repo, runs: runs, err: err}
		}

	case ScopeFiles:
		ref := m.fileRef()
		dirKey := m.dirCacheKey(ref, m.filePath)
		if _, ok := rc.dirs[dirKey]; ok || rc.dirsLoading[dirKey] {
			return m.ensureSelectionDetailsCmd()
		}
		rc.dirsLoading[dirKey] = true
		m.loadingScope[scopeKey] = true
		c := m.client
		dirPath := m.filePath
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			entries, err := c.ListDir(ctx, repo, ref, dirPath)
			return dirLoadedMsg{repo: repo, ref: ref, path: dirPath, entries: entries, err: err}
		}
	}
	return nil
}

func (m *Model) ensureSelectionDetailsCmd() tea.Cmd {
	repo := m.selectedRepo
	if repo == "" || m.client == nil {
		return nil
	}
	row, ok := m.currentListRow()
	if !ok {
		return nil
	}
	rc := m.getOrCreateCache(repo)
	c := m.client

	switch row.kind {
	case listBranch:
		bName := row.branch.Name
		if _, ok := rc.commits[bName]; ok || rc.commitsLoading[bName] {
			return nil
		}
		rc.commitsLoading[bName] = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			commits, err := c.ListBranchCommits(ctx, repo, bName, 15)
			return branchCommitsLoadedMsg{repo: repo, branch: bName, commits: commits, err: err}
		}

	case listIssue:
		num := row.issue.Number
		if _, ok := rc.issueComments[num]; ok || rc.issueCommentsLoading[num] {
			return nil
		}
		rc.issueCommentsLoading[num] = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			comments, err := c.ListIssueComments(ctx, repo, num)
			return issueCommentsLoadedMsg{repo: repo, number: num, comments: comments, err: err}
		}

	case listRun:
		id := row.run.ID
		if _, ok := rc.jobs[id]; ok || rc.jobsLoading[id] {
			return nil
		}
		rc.jobsLoading[id] = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			jobs, err := c.ListWorkflowJobs(ctx, repo, id)
			return jobsLoadedMsg{repo: repo, runID: id, jobs: jobs, err: err}
		}

	case listFile:
		if row.entry.Type == "dir" {
			return nil
		}
		ref := m.fileRef()
		fPath := row.entry.Path
		key := m.dirCacheKey(ref, fPath)
		if _, ok := rc.files[key]; ok || rc.filesLoading[key] {
			return nil
		}
		rc.filesLoading[key] = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
			defer cancel()
			fc, err := c.GetFile(ctx, repo, ref, fPath)
			return fileLoadedMsg{repo: repo, ref: ref, path: fPath, content: fc, err: err}
		}
	}
	return nil
}

func (m *Model) triggerRepoSearch() tea.Cmd {
	q := strings.TrimSpace(m.addRepoInput.Value())
	if q == "" || m.client == nil {
		return nil
	}
	m.addRepoSearching = true
	m.modalErr = ""
	c := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		repos, err := c.SearchRepos(ctx, q, 15)
		return searchReposLoadedMsg{query: q, repos: repos, err: err}
	}
}

func (m *Model) createBranchCmd(repo, name, base string) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		err := c.CreateBranch(ctx, repo, name, base)
		return branchCreatedMsg{repo: repo, branch: name, err: err}
	}
}

func (m *Model) deleteBranchCmd(repo, name string) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		err := c.DeleteBranch(ctx, repo, name)
		return branchDeletedMsg{repo: repo, branch: name, err: err}
	}
}

func (m *Model) createIssueCmd(repo, title, body string) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		iss, err := c.CreateIssue(ctx, repo, title, body)
		return issueCreatedMsg{repo: repo, issue: iss, err: err}
	}
}

func (m *Model) commentIssueCmd(repo string, number int, body string) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		cm, err := c.CommentIssue(ctx, repo, number, body)
		return issueCommentedMsg{repo: repo, number: number, comment: cm, err: err}
	}
}

func (m *Model) setIssueStateCmd(repo string, number int, state string) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		err := c.SetIssueState(ctx, repo, number, state)
		return issueStateSetMsg{repo: repo, number: number, state: state, err: err}
	}
}

func (m *Model) rerunWorkflowCmd(repo string, runID int64, failedOnly bool) tea.Cmd {
	c := m.client
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), apiTimeout)
		defer cancel()
		err := c.RerunWorkflow(ctx, repo, runID, failedOnly)
		return workflowRerunMsg{repo: repo, runID: runID, err: err}
	}
}
