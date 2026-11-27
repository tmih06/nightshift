package accountsview

import (
	"fmt"
	"strings"

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
		m.wheel(ev)
	case tea.MouseClickMsg:
		m.click(ev)
	}
	return m, nil
}

// wheel scrolls the detail pane under the pointer, and moves through the list
// anywhere else.
func (m *Model) wheel(ev tea.MouseWheelMsg) {
	delta := 0
	switch ev.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return
	}
	overDetail := ev.X >= m.leftWidth()
	if m.width < 90 {
		_, height := m.detailSize()
		overDetail = ev.Y >= m.height-1-height
	}
	if overDetail {
		m.scrollDetail(3 * delta)
		return
	}
	m.move(delta)
}

// click selects the account under a left click on the list.
func (m *Model) click(ev tea.MouseClickMsg) {
	if ev.Button != tea.MouseLeft || ev.X >= m.leftWidth() || ev.Y < 1 {
		return
	}
	row := ev.Y - 1 + m.top
	lines := m.listLines()
	if row >= 0 && row < len(lines) && lines[row].acc >= 0 {
		m.cursor, m.scroll = lines[row].acc, 0
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

// trafficSummary is the requests counted in a few words, or "" when there are
// none.
func (m Model) trafficSummary() string {
	an := m.snap.Analytics
	if an == nil || an.Requests <= 0 {
		return ""
	}
	summary := fmt.Sprintf("%d req", an.Requests)
	if an.Total > 0 {
		summary += " · " + compactCount(an.Total) + " tok"
	}
	if an.CacheHitPercent > 0 {
		summary += fmt.Sprintf(" · %.0f%% cache", an.CacheHitPercent)
	}
	return summary
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
		if summary := m.trafficSummary(); summary != "" {
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
	keys := [][2]string{{"j/k", "move"}, {"u", "use"}, {"K/J", "order"}, {"a", "add"}, {"d", "disable"}, {"x", "remove"}, {"r", "refresh"}, {"v", "calendar"}, {"⇥", "tabs"}, {"q", "quit"}, {"?", "help"}}
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
		width, height := m.detailSize()
		right = m.detail(a, width, height)
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
			"  "+s.faint.Render("Press r to try again."))
	default:
		return []string{"", "  " + s.faint.Render("Starting a connection to CLIProxyAPI…")}
	}
}

func (m Model) noSelection() []string {
	return []string{
		m.st.title.Render("No accounts yet"),
		"",
		m.st.text.Render("Press a to sign an account in to Claude Code, Codex, or Antigravity."),
		m.st.faint.Render("Sign in as many accounts as you like, then pick the active"),
		m.st.faint.Render("one for each agent with u."),
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

// accountRow is one account in the list: its health, name, most used quota
// window, state, and request count, as far as width allows.
func (m Model) accountRow(a v1.Account, selected bool, width int) string {
	s := m.st
	// Each part carries the selection's background itself: a style around the
	// whole line would end at the first part's reset.
	on := func(st lipgloss.Style) lipgloss.Style {
		if selected {
			return st.Background(m.theme.SelectedBackground)
		}
		return st
	}
	blank := func(n int) string { return on(s.text).Render(strings.Repeat(" ", max(0, n))) }
	cooling := a.CooldownUntil.After(m.now())
	_, known := inUse(m.snap, a.Provider)
	mark, markStyle := "○", s.faint
	switch {
	case cooling:
		mark, markStyle = "◔", s.warn
	case a.ActiveNow || (a.Active && !known):
		mark, markStyle = "●", s.ok
	}
	state, stateStyle := stateName(a.State), m.stateStyle(a.State)
	switch {
	case cooling:
		state, stateStyle = "cooldown", s.warn
	case a.ActiveNow && a.State == v1.AccountOK:
		state, stateStyle = "active", s.ok
	case m.pending(a):
		state, stateStyle = "pending", s.warn
	}
	tail := m.listQuota(a, width, on) + on(stateStyle).Render(pad(state, 10))
	if width >= 70 {
		req := ""
		if a.Usage != nil {
			req = fmt.Sprintf("%d req", a.Usage.Requests)
		}
		tail += on(s.faint).Render(lipgloss.NewStyle().Width(9).Align(lipgloss.Right).Render(req))
	}
	prefix := ""
	if a.Priority > 0 {
		prefix = fmt.Sprintf("#%d ", a.Priority)
	}
	room := max(8, width-3-lipgloss.Width(tail))
	name := ansi.Truncate(label(a), room-len(prefix), "…")
	line := blank(1) + on(markStyle).Render(mark) + blank(1) + on(s.faint).Render(prefix) + on(s.text).Render(name) +
		blank(room-len(prefix)-lipgloss.Width(name)) + tail
	return pad(line+blank(width-lipgloss.Width(line)), width)
}

// listQuota is the list's small meter of the account's most used window. A
// narrow list leaves it out.
func (m Model) listQuota(a v1.Account, width int, on func(lipgloss.Style) lipgloss.Style) string {
	if width < 46 {
		return ""
	}
	peak, ok := peakQuota(a.Quota)
	if !ok {
		return on(m.st.faint).Render(pad("", 13))
	}
	style := m.severity(peak)
	return m.meter(peak, 6, on(style), -1) + on(style).Render(fmt.Sprintf(" %3.0f%%", peak)) + on(m.st.faint).Render("  ")
}

func (m Model) modalBox() string {
	switch m.modal.kind {
	case modalProvider:
		return m.providerBox()
	case modalLink:
		return m.linkBox()
	case modalConfirm:
		return m.confirmBox()
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

func (m Model) confirmBox() string {
	s := m.st
	text := s.text.Width(m.modalInner())
	lines := make([]string, 0, len(m.modal.body)+4)
	lines = append(lines, s.title.Width(m.modalInner()).Render(m.modal.title), "")
	for _, l := range m.modal.body {
		lines = append(lines, text.Render(l))
	}
	lines = append(lines, "", s.key.Render("y")+s.faint.Render(" "+m.modal.verb+" • ")+s.key.Render("n")+s.faint.Render(" cancel"))
	return dialog.Box(m.theme, lines)
}

func (m Model) helpBox() string {
	s := m.st
	row := func(k, d string) string { return s.key.Render(pad(k, 10)) + s.text.Render(d) }
	return dialog.Box(m.theme, []string{
		s.title.Render("Accounts"), "",
		row("u, enter", "use this account: all of this agent's traffic moves to it"),
		row("K/J", "move this account up or down in its fallback chain"),
		row("a", "sign an account in (opens a link, paste the address back)"),
		row("d", "disable or enable the account"),
		row("x", "remove the account and its tokens"),
		row("r", "read everything again, live quotas included"),
		row("t", "renew the account's token now"),
		row("j/k, g/G", "move"),
		row("ctrl+d/u", "scroll the account's details (or the wheel, pgdn/pgup)"),
		row("v", "calendar of this account, or of all accounts"), "",
		s.faint.Render("Usage counts requests through the egress proxy and starts over"),
		s.faint.Render("when the proxy restarts; the calendar's days (UTC) are kept."),
		s.faint.Render("On 429/401/403/529 the proxy fails over down the fallback chain"),
		s.faint.Render("automatically without restarting pods."),
	})
}
