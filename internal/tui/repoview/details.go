package repoview

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
	ghmd "github.com/tmih06/nightshift/internal/ghdash/tui/markdown"
)

const (
	readmePreviewMaxLines = 48
	readmePreviewMaxBytes = 2400
)

func (m *Model) renderDetailsPane(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	focused := m.activePane == PaneDetails
	lines := make([]string, 0, height)
	title := "Details"
	if m.zoomDetails {
		title = "Details (zoomed)"
	}

	bodyLines := m.cachedDetailsLines(width)
	contentH := max(0, height-1)

	maxScroll := max(0, len(bodyLines)-contentH)
	if m.detailsScroll > maxScroll {
		m.detailsScroll = maxScroll
	}
	if m.detailsScroll < 0 {
		m.detailsScroll = 0
	}

	scrollInfo := ""
	if len(bodyLines) > contentH && contentH > 0 {
		pct := 0
		if maxScroll > 0 {
			pct = (m.detailsScroll * 100) / maxScroll
		}
		scrollInfo = fmt.Sprintf("%d%%", pct)
	}

	lines = append(lines, m.renderPaneHeader(3, title, scrollInfo, width, focused))
	if height == 1 {
		return lines
	}

	blank := strings.Repeat(" ", width)
	for i := 0; i < contentH; i++ {
		idx := m.detailsScroll + i
		if idx < len(bodyLines) {
			lines = append(lines, bodyLines[idx])
		} else {
			lines = append(lines, blank)
		}
	}
	return lines
}

func (m *Model) cachedDetailsLines(width int) []string {
	navRow, hasNav := m.currentNavRow()
	showingOverview := m.selectedRepo == "" ||
		(m.activePane == PaneNav && hasNav && navRow.kind == navRepo) ||
		(m.activePane == PaneDetails && m.detailsOverview && hasNav && navRow.kind == navRepo)
	var listRowVal listRow
	var hasListRow bool
	if !showingOverview {
		listRowVal, hasListRow = m.currentListRow()
		if !hasListRow || listRowVal.kind == listParentDir {
			showingOverview = true
		}
	}
	fullReadme := m.activePane == PaneDetails || m.detailsScroll > 0
	minuteBucket := m.now().Unix() / 60

	if m.detailsCache.entries == nil ||
		m.detailsCache.width != width ||
		m.detailsCache.dataRev != m.dataRev ||
		m.detailsCache.sandboxRev != m.sandboxRev ||
		m.detailsCache.minuteBucket != minuteBucket ||
		len(m.detailsCache.entries) >= 64 {
		m.detailsCache = detailsCacheState{
			width:        width,
			dataRev:      m.dataRev,
			sandboxRev:   m.sandboxRev,
			minuteBucket: minuteBucket,
			entries:      make(map[detailsCacheKey]detailsCacheEntry, 16),
		}
	}

	key := detailsCacheKey{
		width:           width,
		selectedRepo:    m.selectedRepo,
		showingOverview: showingOverview,
		activeScope:     m.activeScope,
		listCursor:      m.listCursor,
		filePath:        m.filePath,
		dataRev:         m.dataRev,
		sandboxRev:      m.sandboxRev,
		fullReadme:      fullReadme,
		minuteBucket:    minuteBucket,
	}

	var curReadme string
	var curLoading bool
	if showingOverview && m.selectedRepo != "" {
		curReadme = m.findRepo(m.selectedRepo).Readme
		if rc := m.cache[strings.ToLower(m.selectedRepo)]; rc != nil {
			curLoading = rc.overviewLoading
		}
	}

	if entry, ok := m.detailsCache.entries[key]; ok {
		if !showingOverview || (entry.readme == curReadme && entry.overviewLoading == curLoading) {
			return entry.lines
		}
	}

	var raw []string
	if m.selectedRepo == "" {
		raw = []string{
			"",
			m.ui.faintPadLeft.Render("Select a repository to inspect."),
		}
	} else if showingOverview {
		raw = m.renderRepoOverviewDetails(m.selectedRepo, width, fullReadme)
	} else {
		switch listRowVal.kind {
		case listBranch:
			raw = m.renderBranchDetails(*listRowVal.branch, width)
		case listPR:
			raw = m.renderPRDetails(*listRowVal.pr, width)
		case listIssue:
			raw = m.renderIssueDetails(*listRowVal.issue, width)
		case listRun:
			raw = m.renderRunDetails(*listRowVal.run, width)
		case listFile:
			raw = m.renderFileDetails(*listRowVal.entry, width)
		default:
			raw = m.renderRepoOverviewDetails(m.selectedRepo, width, fullReadme)
		}
	}

	padded := make([]string, len(raw))
	blank := strings.Repeat(" ", width)
	for i, l := range raw {
		if l == "" {
			padded[i] = blank
		} else {
			padded[i] = padLine(l, width)
		}
	}

	m.detailsCache.entries[key] = detailsCacheEntry{
		lines:           padded,
		readme:          curReadme,
		overviewLoading: curLoading,
	}
	return padded
}

func (m *Model) renderMutedPill(text string) string {
	return m.ui.mutedPill.Render(text)
}

func trimReadmePreview(md string) (trimmed string, wasTrimmed bool) {
	if md == "" {
		return md, false
	}
	lines := 0
	pos := 0
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0

	for pos < len(md) {
		nl := strings.IndexByte(md[pos:], '\n')
		var line string
		var nextPos int
		if nl >= 0 {
			line = md[pos : pos+nl]
			nextPos = pos + nl + 1
		} else {
			line = md[pos:]
			nextPos = len(md)
		}

		if lines >= readmePreviewMaxLines || (pos > 0 && pos+len(line) > readmePreviewMaxBytes) {
			out := md[:pos]
			if inFence {
				if len(out) > 0 && out[len(out)-1] != '\n' {
					out += "\n"
				}
				out += strings.Repeat(string(fenceChar), fenceLen) + "\n"
			}
			return out, true
		}

		t := strings.TrimLeft(line, " \t")
		if len(t) >= 3 && (t[0] == '`' || t[0] == '~') {
			ch := t[0]
			n := 0
			for n < len(t) && t[n] == ch {
				n++
			}
			if n >= 3 {
				if !inFence {
					inFence = true
					fenceChar = ch
					fenceLen = n
				} else if ch == fenceChar && n >= fenceLen && strings.TrimSpace(t[n:]) == "" {
					inFence = false
					fenceChar = 0
					fenceLen = 0
				}
			}
		}

		pos = nextPos
		lines++
	}
	return md, false
}

func (m *Model) renderMarkdownLines(md string, width int) []string {
	if strings.TrimSpace(md) == "" {
		return nil
	}
	renderW := max(20, width-2)
	rendered, err := ghmd.Render(md, renderW, nil)
	if err != nil {
		rendered = md
	}
	rendered = strings.TrimRight(rendered, "\n")
	raw := strings.Split(rendered, "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, " "+l)
	}
	return out
}

func (m *Model) renderRepoOverviewDetails(repoName string, width int, fullReadme bool) []string {
	r := m.findRepo(repoName)
	var out []string

	vis := r.Visibility
	if vis == "" {
		if r.IsPrivate {
			vis = "private"
		} else {
			vis = "public"
		}
	}
	headerText := fmt.Sprintf(" %s · %s", r.NormalizedFullName(), vis)
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, headerText))...)
	title := r.Name
	if title == "" {
		title = r.NormalizedFullName()
	}
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, title))...)
	out = append(out, "")

	if r.Description != "" {
		desc := wrapPlain(r.Description, max(12, width-2))
		for _, dl := range desc {
			out = append(out, " "+m.ui.primary.Render(dl))
		}
		out = append(out, "")
	}

	var pills []string
	pills = append(pills, m.renderMutedPill(fmt.Sprintf("★ %d", r.Stars)))
	pills = append(pills, m.renderMutedPill(fmt.Sprintf(" %d", r.Forks)))
	pills = append(pills, m.renderMutedPill(fmt.Sprintf(" %d", r.OpenIssues)))
	if r.DefaultBranch != "" {
		pills = append(pills, m.renderMutedPill(" "+r.DefaultBranch))
	}
	if r.Language != "" {
		pills = append(pills, m.renderMutedPill(r.Language))
	}
	out = append(out, " "+strings.Join(pills, " "))

	if len(r.Topics) > 0 {
		out = append(out, "")
		topicPills := make([]string, 0, len(r.Topics))
		for _, t := range r.Topics {
			topicPills = append(topicPills, m.ui.openPR.Render("#"+t))
		}
		out = append(out, " "+strings.Join(topicPills, " "))
	}

	boxes := sandboxesForRepo(m.sandboxes, repoName)
	if len(boxes) > 0 {
		out = append(out, "")
		out = append(out, " "+m.ui.successBold.Render(fmt.Sprintf("ACTIVE SANDBOXES (%d)", len(boxes))))
		for _, b := range boxes {
			phase := b.Phase
			if phase == "" {
				phase = "Running"
			}
			line := fmt.Sprintf("  %s %s  %s  %s",
				m.ui.dotSuccess,
				m.ui.primaryBold.Render(b.Pod),
				m.ui.faint.Render(b.Volume),
				m.ui.secondary.Render(phase),
			)
			out = append(out, line)
		}
		out = append(out, " "+m.ui.faint.Render("  press b to jump to sandbox · S to launch new"))
	} else {
		out = append(out, "")
		out = append(out, " "+m.ui.faint.Render("S launch sandbox · o open in browser · f pin/unpin"))
	}

	rc := m.cache[strings.ToLower(repoName)]
	if r.Readme != "" {
		out = append(out, "")
		out = append(out, " "+m.ui.faintBold.Render("README.md"))
		out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))
		md := r.Readme
		trimmed := false
		if !fullReadme {
			md, trimmed = trimReadmePreview(r.Readme)
		}
		out = append(out, m.renderMarkdownLines(md, width)...)
		if trimmed {
			out = append(out, " "+m.ui.faint.Render("… focus [3] Details or scroll to read full README"))
		}
	} else if rc != nil && rc.overviewLoading {
		out = append(out, "")
		out = append(out, " "+m.ui.faint.Render("Loading repository overview…"))
	}

	return out
}

func (m *Model) renderBranchDetails(b Branch, width int) []string {
	var out []string
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, fmt.Sprintf(" %s · Branch", m.selectedRepo)))...)
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, b.Name))...)
	out = append(out, "")

	var pills []string
	if b.IsDefault {
		pills = append(pills, m.ui.pillSuccess.Render("default"))
	}
	if b.Protected {
		pills = append(pills, m.ui.pillWarning.Render("protected"))
	}
	if b.SHA != "" {
		short := b.SHA
		if len(short) > 7 {
			short = short[:7]
		}
		pills = append(pills, m.renderMutedPill(short))
	}
	if box, ok := sandboxForBranch(m.sandboxes, m.selectedRepo, b.Name); ok {
		pills = append(pills, m.ui.pillSuccess.Render("● "+box.Pod))
	}
	if len(pills) > 0 {
		out = append(out, " "+strings.Join(pills, " "))
		out = append(out, "")
	}

	if b.LinkedPR != nil {
		pr := b.LinkedPR
		out = append(out, " "+m.ui.openPRBold.Render(
			fmt.Sprintf("Linked PR #%d: %s", pr.Number, pr.Title)))
		out = append(out, "")
	}

	out = append(out, " "+m.ui.faint.Render("S sandbox branch · n new branch · d delete · y copy name"))
	out = append(out, "")
	out = append(out, " "+m.ui.faintBold.Render("RECENT COMMITS"))
	out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	rc := m.cache[strings.ToLower(m.selectedRepo)]
	if rc == nil {
		return out
	}
	commits, loaded := rc.commits[b.Name]
	if !loaded {
		out = append(out, " "+m.ui.faint.Render("Loading commits…"))
		return out
	}
	if len(commits) == 0 {
		out = append(out, " "+m.ui.faint.Render("No commits found."))
		return out
	}
	for _, c := range commits {
		sha := m.ui.openPR.Render(c.ShortSHA)
		msgText := ansi.Truncate(c.Message, max(8, width-11), "…")
		msg := m.ui.primary.Render(msgText)
		meta := m.ui.faint.Render(fmt.Sprintf("%s · %s", c.Author, m.relTime(c.Date)))
		out = append(out, fmt.Sprintf(" %s  %s", sha, msg))
		out = append(out, "          "+meta)
	}
	return out
}

func (m *Model) renderPRDetails(pr PullRequest, width int) []string {
	var out []string
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, fmt.Sprintf("%s · #%d", m.selectedRepo, pr.Number)))...)
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, pr.Title))...)
	out = append(out, "")

	var pills []string
	stateLabel := strings.ToUpper(pr.State)
	stateStyle := m.ui.pillSuccess
	switch stateLabel {
	case "MERGED":
		stateStyle = m.ui.pillMerged
	case "CLOSED":
		stateStyle = m.ui.pillClosed
	default:
		if pr.IsDraft {
			stateLabel = "DRAFT"
			stateStyle = m.ui.pillFaint
		}
	}
	pills = append(pills, stateStyle.Render(stateLabel))
	if pr.Author != "" {
		pills = append(pills, m.renderMutedPill("@"+pr.Author))
	}
	if pr.ChecksState != "" {
		pills = append(pills, m.renderMutedPill("CI: "+strings.ToLower(pr.ChecksState)))
	}
	if box, ok := sandboxForPR(m.sandboxes, m.selectedRepo, pr.Number); ok {
		pills = append(pills, m.ui.pillSuccess.Render("● "+box.Pod))
	}
	out = append(out, " "+strings.Join(pills, " "))

	if pr.BaseRef != "" || pr.HeadRef != "" {
		branchLine := fmt.Sprintf(" %s ← %s",
			m.ui.secondary.Render(pr.BaseRef),
			m.ui.openPRBold.Render(pr.HeadRef),
		)
		out = append(out, branchLine)
	}

	diffStats := fmt.Sprintf(" %s %s · %d files · %d comments",
		m.ui.success.Render(fmt.Sprintf("+%d", pr.Additions)),
		m.ui.errStyle.Render(fmt.Sprintf("-%d", pr.Deletions)),
		pr.ChangedFiles,
		pr.CommentsCount,
	)
	out = append(out, diffStats)

	if len(pr.Labels) > 0 {
		lp := make([]string, 0, len(pr.Labels))
		for _, l := range pr.Labels {
			lp = append(lp, m.renderMutedPill(l))
		}
		out = append(out, " "+strings.Join(lp, " "))
	}

	out = append(out, "")
	out = append(out, " "+m.ui.faint.Render(
		ansi.Truncate("enter open PR · S sandbox · o browser · y copy URL", max(4, width-2), "…")))
	out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	if strings.TrimSpace(pr.Body) != "" {
		out = append(out, m.renderMarkdownLines(pr.Body, width)...)
	} else {
		out = append(out, " "+m.ui.faintItalic.Render("No description provided."))
	}
	return out
}

func (m *Model) renderIssueDetails(iss Issue, width int) []string {
	var out []string
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, fmt.Sprintf("%s · #%d", m.selectedRepo, iss.Number)))...)
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, iss.Title))...)
	out = append(out, "")

	var pills []string
	stateLabel := strings.ToUpper(iss.State)
	stateStyle := m.ui.pillSuccess
	if stateLabel == "CLOSED" {
		stateStyle = m.ui.pillMerged
	}
	pills = append(pills, stateStyle.Render(stateLabel))
	if iss.Author != "" {
		pills = append(pills, m.renderMutedPill("@"+iss.Author))
	}
	if len(iss.Assignees) > 0 {
		pills = append(pills, m.renderMutedPill("→ "+strings.Join(iss.Assignees, ", ")))
	}
	if box, ok := sandboxForIssue(m.sandboxes, m.selectedRepo, iss.Number); ok {
		pills = append(pills, m.ui.pillSuccess.Render("● "+box.Pod))
	}
	out = append(out, " "+strings.Join(pills, " "))

	if len(iss.Labels) > 0 {
		lp := make([]string, 0, len(iss.Labels))
		for _, l := range iss.Labels {
			lp = append(lp, m.renderMutedPill(l))
		}
		out = append(out, " "+strings.Join(lp, " "))
	}

	out = append(out, "")
	toggleHint := "x close"
	if strings.EqualFold(iss.State, "CLOSED") {
		toggleHint = "X reopen"
	}
	out = append(out, " "+m.ui.faint.Render(
		fmt.Sprintf("c comment · %s · S sandbox issue · o browser · y copy URL", toggleHint)))
	out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	if strings.TrimSpace(iss.Body) != "" {
		out = append(out, m.renderMarkdownLines(iss.Body, width)...)
	} else {
		out = append(out, " "+m.ui.faintItalic.Render("No description provided."))
	}

	rc := m.cache[strings.ToLower(m.selectedRepo)]
	if rc != nil {
		comments, loaded := rc.issueComments[iss.Number]
		out = append(out, "")
		out = append(out, " "+m.ui.faintBold.Render(fmt.Sprintf("COMMENTS (%d)", len(comments))))
		out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))
		if !loaded {
			out = append(out, " "+m.ui.faint.Render("Loading comments…"))
		} else if len(comments) == 0 {
			out = append(out, " "+m.ui.faint.Render("No comments yet."))
		} else {
			for _, cm := range comments {
				hdr := fmt.Sprintf(" %s  %s",
					m.ui.primaryBold.Render("@"+cm.Author),
					m.ui.faint.Render(m.relTime(cm.CreatedAt)),
				)
				out = append(out, hdr)
				out = append(out, m.renderMarkdownLines(cm.Body, width)...)
				out = append(out, "")
			}
		}
	}
	return out
}

func (m *Model) renderRunDetails(run WorkflowRun, width int) []string {
	var out []string
	hdr := fmt.Sprintf("%s · %s #%d", m.selectedRepo, run.Name, run.RunNumber)
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, hdr))...)
	title := run.DisplayTitle
	if title == "" {
		title = run.Name
	}
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, title))...)
	out = append(out, "")

	stateLabel := strings.ToUpper(run.Conclusion)
	if stateLabel == "" {
		stateLabel = strings.ToUpper(run.Status)
	}
	stateStyle := m.ui.pillWarning
	switch strings.ToLower(run.Conclusion) {
	case "success":
		stateStyle = m.ui.pillSuccess
	case "failure", "timed_out":
		stateStyle = m.ui.pillError
	case "cancelled", "skipped":
		stateStyle = m.ui.pillFaint
	}

	var pills []string
	pills = append(pills, stateStyle.Render(stateLabel))
	if run.Branch != "" {
		pills = append(pills, m.renderMutedPill(" "+run.Branch))
	}
	if run.Event != "" {
		pills = append(pills, m.renderMutedPill(run.Event))
	}
	if run.HeadSHA != "" {
		short := run.HeadSHA
		if len(short) > 7 {
			short = short[:7]
		}
		pills = append(pills, m.renderMutedPill(short))
	}
	if !run.CreatedAt.IsZero() && !run.UpdatedAt.IsZero() && run.UpdatedAt.After(run.CreatedAt) {
		dur := run.UpdatedAt.Sub(run.CreatedAt).Round(time.Second)
		pills = append(pills, m.renderMutedPill(dur.String()))
	}
	out = append(out, " "+strings.Join(pills, " "))
	out = append(out, "")
	out = append(out, " "+m.ui.faint.Render("R rerun workflow · S sandbox branch · o browser · y copy URL"))
	out = append(out, "")
	out = append(out, " "+m.ui.faintBold.Render("JOBS"))
	out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	rc := m.cache[strings.ToLower(m.selectedRepo)]
	if rc == nil {
		return out
	}
	jobs, loaded := rc.jobs[run.ID]
	if !loaded {
		out = append(out, " "+m.ui.faint.Render("Loading jobs…"))
		return out
	}
	if len(jobs) == 0 {
		out = append(out, " "+m.ui.faint.Render("No jobs reported."))
		return out
	}
	for _, j := range jobs {
		g := m.runStatusGlyph(j.Status, j.Conclusion)
		dur := ""
		if !j.StartedAt.IsZero() && !j.CompletedAt.IsZero() && j.CompletedAt.After(j.StartedAt) {
			dur = " " + m.ui.faint.Render(j.CompletedAt.Sub(j.StartedAt).Round(time.Second).String())
		}
		out = append(out, fmt.Sprintf(" %s %s%s", g, m.ui.primaryBold.Render(j.Name), dur))
		for _, st := range j.Steps {
			sg := m.runStatusGlyph(st.Status, st.Conclusion)
			out = append(out, fmt.Sprintf("   %s %s", sg, m.ui.secondary.Render(st.Name)))
		}
	}
	return out
}

func (m *Model) renderFileDetails(entry TreeEntry, width int) []string {
	var out []string
	ref := m.fileRef()
	if ref == "" {
		ref = "HEAD"
	}
	out = append(out, splitLines(common.RenderPreviewHeader(m.theme, width, fmt.Sprintf("%s · %s", m.selectedRepo, ref)))...)
	out = append(out, splitLines(common.RenderPreviewTitle(m.theme, m.styles.Common, width, entry.Path))...)
	out = append(out, "")

	if entry.Type == "dir" {
		out = append(out, " "+m.renderMutedPill("directory"))
		out = append(out, "")
		out = append(out, " "+m.ui.faint.Render("enter / double-click to open directory · S sandbox repo"))
		return out
	}

	var pills []string
	pills = append(pills, m.renderMutedPill(formatBytes(entry.Size)))
	if ext := strings.TrimPrefix(filepath.Ext(entry.Name), "."); ext != "" {
		pills = append(pills, m.renderMutedPill(ext))
	}
	out = append(out, " "+strings.Join(pills, " "))
	out = append(out, " "+m.ui.faint.Render("S sandbox repo · o browser · y copy path"))
	out = append(out, m.ui.faintBorder.Render(strings.Repeat("─", width)))

	rc := m.cache[strings.ToLower(m.selectedRepo)]
	if rc == nil {
		return out
	}
	key := m.dirCacheKey(m.fileRef(), entry.Path)
	fc, loaded := rc.files[key]
	if !loaded {
		out = append(out, " "+m.ui.faint.Render("Loading file…"))
		return out
	}
	if fc.Binary {
		out = append(out, " "+m.ui.warning.Render("Binary file preview is not supported."))
		return out
	}
	if fc.Truncated {
		out = append(out, " "+m.ui.warning.Render("Preview truncated at 64KB."))
		out = append(out, "")
	}

	ext := strings.ToLower(filepath.Ext(entry.Name))
	if ext == ".md" || ext == ".markdown" {
		out = append(out, m.renderMarkdownLines(fc.Content, width)...)
		return out
	}

	tokenLines := m.syntax.Lines(entry.Path, fc.Content)
	gutterW := len(strconv.Itoa(max(1, len(tokenLines))))

	for i, toks := range tokenLines {
		var b strings.Builder
		b.WriteString(" ")
		b.WriteString(m.ui.faint.Render(fmt.Sprintf("%*d", gutterW, i+1)))
		b.WriteString(m.ui.fileSep)
		for _, tok := range toks {
			txt := strings.ReplaceAll(tok.Text, "\t", "    ")
			if tok.Color != nil {
				b.WriteString(lipgloss.NewStyle().Foreground(tok.Color).Render(txt))
			} else {
				b.WriteString(m.ui.primary.Render(txt))
			}
		}
		out = append(out, b.String())
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func wrapPlain(s string, width int) []string {
	if width <= 0 || ansi.StringWidth(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var out []string
	cur := words[0]
	for _, w := range words[1:] {
		if ansi.StringWidth(cur)+1+ansi.StringWidth(w) <= width {
			cur += " " + w
		} else {
			out = append(out, cur)
			cur = w
		}
	}
	out = append(out, cur)
	return out
}
