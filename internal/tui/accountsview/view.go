package accountsview

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	v1 "github.com/tmih06/nightshift/api/v1"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
	"github.com/tmih06/nightshift/internal/tui/dialog"
)

type styles struct {
	title, faint, text, ok, warn, bad, head, selected, key lipgloss.Style
}

func newStyles(th ghtheme.Theme) styles {
	s := lipgloss.NewStyle()
	return styles{
		title:    s.Foreground(th.PrimaryText).Bold(true),
		faint:    s.Foreground(th.FaintText),
		text:     s.Foreground(th.PrimaryText),
		ok:       s.Foreground(th.SuccessText),
		warn:     s.Foreground(th.WarningText),
		bad:      s.Foreground(th.ErrorText),
		head:     s.Foreground(th.SecondaryText).Bold(true),
		selected: s.Background(th.SelectedBackground),
		key:      s.Foreground(th.PrimaryText).Bold(true),
	}
}

// pad cuts or pads s to exactly width cells.
func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	if w := lipgloss.Width(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

// listLine is one row of the account list: a heading, a hint under an agent
// with no accounts, or an account.
type listLine struct {
	provider v1.Provider
	acc      int // index into flat, or -1
	empty    bool
	header   bool
}

func (m Model) listLines() []listLine {
	var lines []listLine
	i := 0
	for _, p := range m.snap.Providers {
		lines = append(lines, listLine{provider: p, acc: -1, header: true})
		n := len(byProvider(m.snap, p.ID))
		if n == 0 {
			lines = append(lines, listLine{provider: p, acc: -1, empty: true})
		}
		for j := 0; j < n; j++ {
			lines = append(lines, listLine{provider: p, acc: i})
			i++
		}
	}
	return lines
}

// leftWidth is the account list's width; the detail pane gets the rest.
func (m Model) leftWidth() int {
	if m.width < 90 {
		return m.width
	}
	return min(60, m.width*45/100)
}

func (m Model) bodyHeight() int { return max(3, m.height-2) }

// listHeight is how many lines of the list are visible. On a narrow screen the
// detail pane sits under the list and takes the lower part.
func (m Model) listHeight() int {
	if m.width >= 90 {
		return m.bodyHeight()
	}
	return max(3, m.bodyHeight()*55/100)
}

func (m *Model) keepCursorInView() {
	h := m.listHeight()
	if h <= 0 {
		return
	}
	at := 0
	for i, l := range m.listLines() {
		if l.acc == m.cursor {
			at = i
		}
	}
	if at < m.top {
		m.top = at
	}
	if at >= m.top+h {
		m.top = at - h + 1
	}
	// Show the heading above the first account of an agent when it fits.
	if at == m.top+0 && m.top > 0 {
		m.top--
	}
	m.top = max(0, m.top)
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.modal.kind != modalNone {
		return m, nil
	}
	switch ev := msg.(type) {
	case tea.MouseWheelMsg:
		switch ev.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		default:
		}
	case tea.MouseClickMsg:
		m.click(ev)
	}
	return m, nil
}

// click selects the account under a left click on the list.
func (m *Model) click(ev tea.MouseClickMsg) {
	if ev.Button != tea.MouseLeft || ev.X >= m.leftWidth() || ev.Y < 1 {
		return
	}
	row := ev.Y - 1 + m.top
	lines := m.listLines()
	if row >= 0 && row < len(lines) && lines[row].acc >= 0 {
		m.cursor = lines[row].acc
	}
}

// View draws the tab.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	rows := append([]string{m.titleLine()}, m.body()...)
	for len(rows) < m.height-1 {
		rows = append(rows, "")
	}
	rows = rows[:max(0, m.height-1)]
	rows = append(rows, m.helpLine())
	out := strings.Join(rows, "\n")
	switch {
	case m.modal.kind != modalNone:
		box := m.modalBox()
		x := max(0, (m.width-lipgloss.Width(box))/2)
		y := max(0, (m.height-lipgloss.Height(box))/2)
		out = dialog.Overlay(out, box, x, y, m.height)
	case m.help:
		box := m.helpBox()
		x := max(0, (m.width-lipgloss.Width(box))/2)
		y := max(0, (m.height-lipgloss.Height(box))/2)
		out = dialog.Overlay(out, box, x, y, m.height)
	}
	return out
}

func (m Model) titleLine() string {
	s := m.st
	parts := []string{s.title.Render("Accounts")}
	now := m.now()
	switch {
	case m.err != nil && !m.loaded:
		parts = append(parts, s.bad.Render("● CLIProxyAPI unreachable"))
	case m.err != nil:
		parts = append(parts, s.warn.Render("● showing the last read; refresh failed"))
	case m.loading && !m.loaded:
		parts = append(parts, s.faint.Render("connecting to CLIProxyAPI…"))
	default:
		parts = append(parts, s.ok.Render("● CLIProxyAPI"))
		if !m.snap.PublishedAt.IsZero() {
			parts = append(parts, s.faint.Render("published "+ago(now, m.snap.PublishedAt)))
		} else {
			parts = append(parts, s.warn.Render("nothing published yet"))
		}
		if m.snap.Analytics != nil && m.snap.Analytics.Requests > 0 {
			an := m.snap.Analytics
			summary := fmt.Sprintf("%d req", an.Requests)
			if an.Total > 0 {
				summary += " · " + compactCount(an.Total) + " tok"
			}
			if an.CacheHitPercent > 0 {
				summary += fmt.Sprintf(" · %.0f%% cache", an.CacheHitPercent)
			}
			parts = append(parts, s.faint.Render(summary))
		}
		if m.snap.UsageError != "" {
			parts = append(parts, s.warn.Render("usage unavailable"))
		}
	}
	if m.notice != "" {
		style := s.faint
		if m.noticeBad {
			style = s.bad
		}
		parts = append(parts, style.Render(m.notice))
	}
	return ansi.Truncate(" "+strings.Join(parts, s.faint.Render("  ·  ")), m.width, "…")
}

func (m Model) helpLine() string {
	keys := [][2]string{{"j/k", "move"}, {"K/J", "order"}, {"a", "add"}, {"⏎", "primary"}, {"Q", "quota"}, {"d", "disable"}, {"x", "remove"}, {"r", "refresh"}, {"p", "publish"}, {"R", "reload"}, {"⇥", "tabs"}, {"q", "quit"}, {"?", "help"}}
	var b strings.Builder
	b.WriteString(" ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(m.st.faint.Render(" • "))
		}
		b.WriteString(m.st.key.Render(k[0]) + " " + m.st.faint.Render(k[1]))
	}
	return ansi.Truncate(b.String(), m.width, "")
}

func (m Model) body() []string {
	if !m.loaded {
		return m.unloaded()
	}
	h := m.bodyHeight()
	left := m.listPane(m.leftWidth(), m.listHeight())
	var right []string
	if a, ok := m.selected(); ok {
		right = m.detail(a)
	} else {
		right = m.noSelection()
	}
	if m.width < 90 {
		out := append([]string{}, left...)
		out = append(out, m.st.faint.Render(strings.Repeat("─", m.width)))
		for _, r := range right {
			out = append(out, pad(r, m.width))
		}
		return out
	}
	lw := m.leftWidth()
	rw := m.width - lw - 3
	sep := m.st.faint.Render(" │ ")
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, pad(l, lw)+sep+pad(r, rw))
	}
	return out
}

func (m Model) unloaded() []string {
	s := m.st
	switch {
	case m.err != nil:
		lines := []string{"", "  " + s.bad.Render("Could not read the accounts."), ""}
		for _, l := range wrap(m.err.Error(), max(20, m.width-6)) {
			lines = append(lines, "  "+s.faint.Render(l))
		}
		return append(lines, "",
			"  "+s.text.Render("The login surface is CLIProxyAPI. Install it with k8s/creds-setup.sh."),
			"  "+s.faint.Render("Press R to try again."))
	default:
		return []string{"", "  " + s.faint.Render("Starting a connection to CLIProxyAPI…")}
	}
}

func (m Model) noSelection() []string {
	return []string{
		m.st.title.Render("No accounts yet"),
		"",
		m.st.text.Render("Press a to sign an account in to Claude Code, Codex, or Antigravity."),
		m.st.faint.Render("Sign in as many accounts as you like, then pick the one each"),
		m.st.faint.Render("agent's sandboxes should use with enter."),
	}
}

func (m Model) listPane(width, height int) []string {
	lines := m.listLines()
	out := make([]string, 0, height)
	for i := m.top; i < len(lines) && len(out) < height; i++ {
		out = append(out, m.listRow(lines[i], width))
	}
	return out
}

func (m Model) listRow(l listLine, width int) string {
	s := m.st
	switch {
	case l.header:
		accts := byProvider(m.snap, l.provider.ID)
		req := 0
		var toks int64
		for i := range accts {
			if accts[i].Usage != nil {
				req += accts[i].Usage.Requests
				toks += accts[i].Usage.Total
			}
		}
		head := s.head.Render(l.provider.Name) + s.faint.Render(fmt.Sprintf("  %d", len(accts)))
		if req > 0 {
			head += s.faint.Render(fmt.Sprintf("  ·  %d req", req))
		}
		if toks > 0 {
			head += s.faint.Render(" · " + compactCount(toks) + " tok")
		}
		return pad(head, width)
	case l.empty:
		return pad("   "+s.faint.Render("none — press a to sign one in"), width)
	}
	a := m.flat[l.acc]
	return m.accountRow(a, l.acc == m.cursor, width)
}

func (m Model) accountRow(a v1.Account, selected bool, width int) string {
	s := m.st
	now := m.now()
	cooling := !a.CooldownUntil.IsZero() && a.CooldownUntil.After(now)
	mark := s.faint.Render("○")
	switch {
	case cooling:
		mark = s.warn.Render("◔")
	case a.ActiveNow || a.Active:
		mark = s.ok.Render("●")
	}
	stateText := m.stateText(a.State)
	if cooling {
		stateText = s.warn.Render("cooldown")
	}
	req := ""
	if a.Usage != nil {
		req = fmt.Sprintf("%d req", a.Usage.Requests)
	}
	prefix := ""
	if a.Priority > 0 {
		prefix = s.faint.Render(fmt.Sprintf("#%d ", a.Priority))
	}
	tail := lipgloss.NewStyle().Width(10).Render(stateText) + s.faint.Render(lipgloss.NewStyle().Width(9).Align(lipgloss.Right).Render(req))
	room := max(8, width-3-lipgloss.Width(tail))
	line := " " + mark + " " + pad(prefix+s.text.Render(label(a)), room) + tail
	line = pad(line, width)
	if selected {
		line = s.selected.Render(line)
	}
	return line
}

func (m Model) detail(a v1.Account) []string {
	s := m.st
	now := m.now()
	p := providerByID(m.snap, a.Provider)
	title := s.title.Render(label(a))
	switch {
	case a.Active:
		title += "  " + s.ok.Render("in use by sandboxes")
	case a.ActiveNow:
		title += "  " + s.ok.Render("handling failover")
	case a.Priority > 1:
		title += "  " + s.faint.Render(fmt.Sprintf("fallback #%d", a.Priority))
	}
	rows := m.statusRows(a, p)
	rows = append(rows,
		[2]string{"Token", "expires " + until(now, a.ExpiresAt)},
		[2]string{"Refreshed", ago(now, a.LastRefreshAt) + nextCheck(now, a)},
	)
	if pub, ok := m.snap.Published[a.Provider]; ok && a.Active {
		rows = append(rows, [2]string{"Published", ago(now, m.snap.PublishedAt) + s.faint.Render(" as "+pub.Label)})
	}
	out := []string{title, s.faint.Render(p.Name + " · " + a.Name), ""}
	w := 0
	for _, r := range rows {
		w = max(w, lipgloss.Width(r[0]))
	}
	for _, r := range rows {
		out = append(out, s.faint.Render(pad(r[0], w))+"  "+r[1])
	}
	out = append(out, "")
	out = append(out, m.quotaBlock(a)...)
	out = append(out, "")
	out = append(out, m.usageBlock(a)...)
	return append(out, "", m.stateHint(a))
}

// stateText is a's state by name, in the color that says how it is doing.
func (m Model) stateText(state string) string {
	style := m.st.faint
	switch state {
	case v1.AccountOK:
		style = m.st.ok
	case v1.AccountExpiring:
		style = m.st.warn
	case v1.AccountFailing, v1.AccountSignIn:
		style = m.st.bad
	}
	return style.Render(stateName(state))
}

// statusRows are the detail pane's rows about the account's health.
func (m Model) statusRows(a v1.Account, p v1.Provider) [][2]string {
	s := m.st
	now := m.now()
	rows := [][2]string{{"Status", m.stateText(a.State)}}
	if a.Priority > 0 {
		enabled := 0
		accts := byProvider(m.snap, a.Provider)
		for i := range accts {
			if !accts[i].Disabled {
				enabled++
			}
		}
		chainVal := fmt.Sprintf("#%d of %d", a.Priority, max(enabled, a.Priority))
		if a.ActiveNow && !a.Active {
			chainVal += "  " + s.ok.Render("· handling failover")
		}
		rows = append(rows, [2]string{"Chain", chainVal})
	}
	if !a.CooldownUntil.IsZero() && a.CooldownUntil.After(now) {
		cd := "until " + until(now, a.CooldownUntil)
		if a.CooldownReason != "" {
			cd += " (" + a.CooldownReason + ")"
		}
		rows = append(rows, [2]string{"Cooldown", s.warn.Render(cd)})
	}
	if a.Failovers > 0 {
		rows = append(rows, [2]string{"Failovers", strconv.Itoa(a.Failovers)})
	}
	if a.StatusNote != "" {
		rows = append(rows, [2]string{"", s.faint.Render(a.StatusNote)})
	}
	if a.RefreshError != "" {
		rows = append(rows, [2]string{"Problem", s.bad.Render(a.RefreshError)})
	}
	if a.State == v1.AccountSignIn {
		rows = append(rows, [2]string{"", s.warn.Render("Sign in again: press a, pick " + p.Name + ", then remove the old entry.")})
	}
	if a.QuotaBlocked {
		rows = append(rows, [2]string{"Quota", s.warn.Render("blocked by the provider")})
	}
	return rows
}

// nextCheck says when CLIProxyAPI will next look at the account's token.
func nextCheck(now time.Time, a v1.Account) string {
	if a.NextRefreshAt.IsZero() || !a.NextRefreshAt.After(now) {
		return ""
	}
	return "  ·  next check " + until(now, a.NextRefreshAt)
}

func (m Model) quotaBlock(a v1.Account) []string {
	s := m.st
	q := a.Quota
	if q == nil {
		return []string{s.head.Render("Quota"), s.faint.Render("No quota reading yet — press Q to fetch live quotas.")}
	}
	now := m.now()
	head := s.head.Render("Quota")
	var meta []string
	if q.Plan != "" {
		meta = append(meta, q.Plan)
	}
	if q.CreditsBalance != "" {
		meta = append(meta, "credits "+q.CreditsBalance)
	}
	if !q.FetchedAt.IsZero() {
		src := q.Source
		if src == "" {
			src = "updated"
		}
		meta = append(meta, src+" "+ago(now, q.FetchedAt))
	}
	if len(meta) > 0 {
		head += s.faint.Render("  " + strings.Join(meta, " · "))
	}
	out := []string{head}
	if q.Error != "" {
		out = append(out, s.warn.Render("fetch error: "+q.Error))
	}
	if !q.NextResetAt.IsZero() && q.NextResetAt.After(now) {
		out = append(out, s.faint.Render("next reset "+until(now, q.NextResetAt)+" ("+q.NextResetAt.UTC().Format("15:04Z")+")"))
	}
	for i := range q.Windows {
		out = append(out, m.quotaWindowLine(now, q.Windows[i]))
	}
	return out
}

func (m Model) quotaWindowLine(now time.Time, w v1.QuotaWindow) string {
	s := m.st
	barStyle := s.ok
	switch {
	case w.UsedPercent >= 90:
		barStyle = s.bad
	case w.UsedPercent >= 70:
		barStyle = s.warn
	}
	lbl := w.Label
	if lbl == "" {
		lbl = w.Name
	}
	line := s.faint.Render(pad(lbl, 16)) + " " + barStyle.Render(quotaBar(w.UsedPercent, 10)) + " " + barStyle.Render(fmt.Sprintf("%4.0f%%", w.UsedPercent))
	if w.Limit > 0 {
		line += s.faint.Render(fmt.Sprintf(" (%s/%s)", compactCount(w.Used), compactCount(w.Limit)))
	}
	if !w.ResetsAt.IsZero() && w.ResetsAt.After(now) {
		line += s.faint.Render("  resets " + until(now, w.ResetsAt))
	}
	return line
}

func (m Model) usageBlock(a v1.Account) []string {
	s := m.st
	if m.snap.UsageError != "" {
		return []string{s.head.Render("Usage"), s.warn.Render("unavailable: " + m.snap.UsageError)}
	}
	u := a.Usage
	if u == nil {
		note := "No request has carried this account's token since the proxy last started."
		if !a.Active && a.Priority > 1 {
			note = fmt.Sprintf("Not in use yet (fallback #%d). Press enter to make it primary, or K/J to reorder.", a.Priority)
		} else if !a.Active {
			note = "Not in use, so no requests. Press enter to use it for sandboxes."
		}
		return []string{s.head.Render("Usage"), s.faint.Render(note)}
	}
	now := m.now()
	out := []string{s.head.Render("Usage") + s.faint.Render("  through the egress proxy, since it last started")}
	reqLine := fmt.Sprintf("%s requests   %s ok", s.title.Render(strconv.Itoa(u.Requests)), s.ok.Render(strconv.Itoa(u.OK)))
	if u.AvgLatencyMS > 0 {
		reqLine += s.faint.Render(fmt.Sprintf("   avg %dms", u.AvgLatencyMS))
	}
	out = append(out, reqLine)
	if bad := m.errorSummary(u); bad != "" {
		out = append(out, bad)
	}
	out = append(out, m.tokenLines(u)...)
	out = append(out, m.modelLines(u)...)
	out = append(out, s.faint.Render("last request "+ago(now, u.LastAt)))
	width := min(48, max(12, m.width-m.leftWidth()-8))
	out = append(out, "", s.faint.Render("requests (last 24h)"), s.ok.Render(spark(u.Recent, width)))
	if len(u.RecentVolume) > 0 {
		out = append(out, s.faint.Render("tokens (last 24h)"), s.ok.Render(spark64(u.RecentVolume, width)))
	}
	return out
}

func (m Model) errorSummary(u *v1.AccountUsage) string {
	s := m.st
	var bad []string
	if u.RateLimited > 0 {
		bad = append(bad, s.warn.Render(fmt.Sprintf("%d rate limited", u.RateLimited)))
	}
	if u.ClientError > 0 {
		bad = append(bad, s.warn.Render(fmt.Sprintf("%d client errors", u.ClientError)))
	}
	if u.ServerError > 0 {
		bad = append(bad, s.bad.Render(fmt.Sprintf("%d server errors", u.ServerError)))
	}
	return strings.Join(bad, s.faint.Render("  ·  "))
}

func (m Model) tokenLines(u *v1.AccountUsage) []string {
	if u.Total == 0 && u.Input == 0 && u.Output == 0 {
		return nil
	}
	s := m.st
	out := []string{fmt.Sprintf("%s %s   %s %s   %s %s",
		s.faint.Render("In"), s.text.Render(compactCount(u.Input)),
		s.faint.Render("Out"), s.text.Render(compactCount(u.Output)),
		s.faint.Render("Total"), s.title.Render(compactCount(u.Total)))}
	var extra []string
	if u.CacheRead > 0 || u.CacheWrite > 0 {
		cache := fmt.Sprintf("Cache %s read · %s write", compactCount(u.CacheRead), compactCount(u.CacheWrite))
		if u.CacheHitPercent > 0 {
			cache += fmt.Sprintf(" (%.1f%% hit)", u.CacheHitPercent)
		}
		extra = append(extra, cache)
	}
	if u.Reasoning > 0 {
		extra = append(extra, "Reasoning "+compactCount(u.Reasoning))
	}
	if len(extra) > 0 {
		out = append(out, s.faint.Render(strings.Join(extra, "  ·  ")))
	}
	return out
}

func (m Model) modelLines(u *v1.AccountUsage) []string {
	if len(u.ByModel) == 0 {
		return nil
	}
	type pair struct {
		name string
		u    v1.ModelUsage
	}
	list := make([]pair, 0, len(u.ByModel))
	for k, v := range u.ByModel {
		list = append(list, pair{name: k, u: v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].u.Total != list[j].u.Total {
			return list[i].u.Total > list[j].u.Total
		}
		return list[i].name < list[j].name
	})
	limit := min(4, len(list))
	out := make([]string, 0, limit+1)
	out = append(out, m.st.faint.Render("models"))
	for i := 0; i < limit; i++ {
		p := list[i]
		out = append(out, fmt.Sprintf("  %s  %s",
			m.st.text.Render(pad(p.name, 26)),
			m.st.faint.Render(fmt.Sprintf("%d req · %s in · %s out · %s cache",
				p.u.Requests, compactCount(p.u.Input), compactCount(p.u.Output), compactCount(p.u.CacheRead)))))
	}
	return out
}

func (m Model) stateHint(a v1.Account) string {
	switch {
	case a.Disabled:
		return m.st.faint.Render("d enables it again, x removes it")
	case a.Priority > 1:
		return m.st.faint.Render("enter makes it primary · K/J reorders the fallback chain")
	case !a.Active:
		return m.st.faint.Render("enter makes sandboxes use this account")
	default:
		return m.st.faint.Render("primary account · K/J reorders the fallback chain")
	}
}

func (m Model) modalBox() string {
	switch m.modal.kind {
	case modalProvider:
		return m.providerBox()
	case modalLink:
		return m.linkBox()
	case modalRemove:
		return m.removeBox()
	case modalNone:
	}
	return ""
}

func (m Model) modalInner() int { return min(76, max(40, m.width-6)) - 4 }

func (m Model) providerBox() string {
	s := m.st
	inner := m.modalInner()
	lines := []string{s.title.Render("Sign in an account"), ""}
	for i, p := range m.snap.Providers {
		row := "  " + p.Name
		if i == m.modal.choice {
			row = s.selected.Render(dialog.Line(s.text.Bold(true), "▸ "+p.Name, inner))
		}
		lines = append(lines, row)
	}
	if m.modal.busy {
		lines = append(lines, "", s.faint.Render("asking CLIProxyAPI for a sign-in link…"))
	} else {
		lines = append(lines, "", s.faint.Render("⏎ continue • esc cancel"))
	}
	return dialog.Box(m.theme, lines)
}

func (m Model) linkBox() string {
	s := m.st
	inner := m.modalInner()
	p := providerByID(m.snap, m.modal.login.Provider)
	lines := []string{s.title.Render("Sign in to " + p.Name), "",
		s.text.Render("1. Open this link and approve the sign-in:"), ""}
	for _, l := range wrap(m.modal.login.URL, inner) {
		lines = append(lines, s.ok.Render(l))
	}
	lines = append(lines, "",
		s.text.Render("2. The browser then lands on a localhost address that will"),
		s.text.Render("   not load. Copy that whole address and paste it here:"), "",
		m.modal.input.View(), "")
	switch {
	case m.modal.busy:
		lines = append(lines, s.faint.Render("signing in…"))
	case m.modal.err != "":
		for _, l := range wrap(m.modal.err, inner) {
			lines = append(lines, s.bad.Render(l))
		}
		lines = append(lines, "", s.faint.Render("⏎ try again • ctrl+o open • ctrl+y copy link • esc cancel"))
	default:
		lines = append(lines, s.faint.Render("⏎ finish • ctrl+o open browser • ctrl+y copy link • esc cancel"))
	}
	return dialog.Box(m.theme, lines)
}

func (m Model) removeBox() string {
	s := m.st
	return dialog.Box(m.theme, []string{
		s.title.Render("Remove " + m.modal.label + "?"), "",
		s.text.Render("This deletes its saved tokens. Sandboxes already running keep"),
		s.text.Render("their login until they are replaced."), "",
		s.faint.Render("y remove • n cancel"),
	})
}

func (m Model) helpBox() string {
	s := m.st
	row := func(k, d string) string { return s.key.Render(pad(k, 10)) + s.text.Render(d) }
	return dialog.Box(m.theme, []string{
		s.title.Render("Accounts"), "",
		row("a", "sign an account in (opens a link, paste the address back)"),
		row("enter, s", "promote this account to #1 in its fallback chain"),
		row("K/J, [/]", "move this account up or down in its fallback chain"),
		row("Q", "fetch live provider quotas and reset times now"),
		row("d", "disable or enable the account"),
		row("x", "remove the account and its tokens"),
		row("r", "refresh the account's token now"),
		row("p", "publish to sandboxes now instead of within five minutes"),
		row("R", "read everything again"),
		row("j/k, g/G", "move"), "",
		s.faint.Render("Usage counts requests through the egress proxy and starts over"),
		s.faint.Render("when the proxy restarts. On 429/401/403/529 the proxy fails over"),
		s.faint.Render("down the fallback chain automatically without restarting pods."),
	})
}
