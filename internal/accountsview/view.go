package accountsview

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/accounts"
	"github.com/tmih06/nightshift/internal/dialog"
	ghtheme "github.com/tmih06/nightshift/internal/ghdash/tui/theme"
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
	provider accounts.Provider
	acc      int // index into flat, or -1
	empty    bool
	header   bool
}

func (m Model) listLines() []listLine {
	var lines []listLine
	i := 0
	for _, p := range accounts.Providers() {
		lines = append(lines, listLine{provider: p, acc: -1, header: true})
		n := len(m.snap.ByProvider(p.ID))
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
		if m.snap.UsageErr != nil {
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
	keys := [][2]string{{"j/k", "move"}, {"a", "add"}, {"⏎", "use"}, {"d", "disable"}, {"x", "remove"}, {"r", "refresh"}, {"p", "publish"}, {"R", "reload"}, {"⇥", "tabs"}, {"q", "quit"}, {"?", "help"}}
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
		accts := m.snap.ByProvider(l.provider.ID)
		req := 0
		for i := range accts {
			if accts[i].Usage != nil {
				req += accts[i].Usage.Requests
			}
		}
		head := s.head.Render(l.provider.Name) + s.faint.Render(fmt.Sprintf("  %d", len(accts)))
		if req > 0 {
			head += s.faint.Render(fmt.Sprintf("  ·  %d requests", req))
		}
		return pad(head, width)
	case l.empty:
		return pad("   "+s.faint.Render("none — press a to sign one in"), width)
	}
	a := m.flat[l.acc]
	return m.accountRow(a, l.acc == m.cursor, width)
}

func (m Model) accountRow(a accounts.Account, selected bool, width int) string {
	s := m.st
	now := m.now()
	mark := s.faint.Render("○")
	if a.Active {
		mark = s.ok.Render("●")
	}
	state := a.State(now)
	stateText := s.faint.Render(state.String())
	switch state {
	case accounts.StateOK:
		stateText = s.ok.Render("ok")
	case accounts.StateExpiring:
		stateText = s.warn.Render("expiring")
	case accounts.StateFailing, accounts.StateSignIn:
		stateText = s.bad.Render(state.String())
	case accounts.StateDisabled:
		stateText = s.faint.Render("disabled")
	}
	req := ""
	if a.Usage != nil {
		req = fmt.Sprintf("%d req", a.Usage.Requests)
	}
	tail := lipgloss.NewStyle().Width(10).Render(stateText) + s.faint.Render(lipgloss.NewStyle().Width(9).Align(lipgloss.Right).Render(req))
	room := max(8, width-3-lipgloss.Width(tail))
	line := " " + mark + " " + pad(s.text.Render(a.Label()), room) + tail
	line = pad(line, width)
	if selected {
		line = s.selected.Render(line)
	}
	return line
}

func (m Model) detail(a accounts.Account) []string {
	s := m.st
	now := m.now()
	p, _ := accounts.ProviderByID(a.Provider)
	title := s.title.Render(a.Label())
	if a.Active {
		title += "  " + s.ok.Render("in use by sandboxes")
	}
	rows := m.statusRows(a, p)
	rows = append(rows,
		[2]string{"Token", "expires " + until(now, a.Expires)},
		[2]string{"Refreshed", ago(now, a.LastRefresh) + nextCheck(now, a)},
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
	out = append(out, m.usageBlock(a)...)
	return append(out, "", m.stateHint(a))
}

// statusRows are the detail pane's rows about the account's health.
func (m Model) statusRows(a accounts.Account, p accounts.Provider) [][2]string {
	s := m.st
	state := a.State(m.now())
	text := state.String()
	switch state {
	case accounts.StateOK:
		text = s.ok.Render(text)
	case accounts.StateExpiring:
		text = s.warn.Render(text)
	case accounts.StateDisabled:
		text = s.faint.Render(text)
	case accounts.StateFailing, accounts.StateSignIn:
		text = s.bad.Render(text)
	}
	rows := [][2]string{{"Status", text}}
	if a.StatusNote != "" {
		rows = append(rows, [2]string{"", s.faint.Render(a.StatusNote)})
	}
	if a.RefreshError != "" {
		rows = append(rows, [2]string{"Problem", s.bad.Render(a.RefreshError)})
	}
	if a.NeedsSignIn {
		rows = append(rows, [2]string{"", s.warn.Render("Sign in again: press a, pick " + p.Name + ", then remove the old entry.")})
	}
	if a.QuotaBlocked {
		rows = append(rows, [2]string{"Quota", s.warn.Render("blocked by the provider")})
	}
	return rows
}

// nextCheck says when CLIProxyAPI will next look at the account's token.
func nextCheck(now time.Time, a accounts.Account) string {
	if a.NextRefresh.IsZero() || !a.NextRefresh.After(now) {
		return ""
	}
	return "  ·  next check " + until(now, a.NextRefresh)
}

func (m Model) usageBlock(a accounts.Account) []string {
	s := m.st
	if m.snap.UsageErr != nil {
		return []string{s.head.Render("Usage"), s.warn.Render("unavailable: " + m.snap.UsageErr.Error())}
	}
	u := a.Usage
	if u == nil {
		note := "No request has carried this account's token since the proxy last started."
		if !a.Active {
			note = "Not in use, so no requests. Press enter to use it for sandboxes."
		}
		return []string{s.head.Render("Usage"), s.faint.Render(note)}
	}
	now := m.now()
	out := []string{s.head.Render("Usage") + s.faint.Render("  through the egress proxy, since it last started")}
	out = append(out, fmt.Sprintf("%s requests   %s ok", s.title.Render(strconv.Itoa(u.Requests)), s.ok.Render(strconv.Itoa(u.OK))))
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
	if len(bad) > 0 {
		out = append(out, strings.Join(bad, s.faint.Render("  ·  ")))
	}
	out = append(out, s.faint.Render("last request "+ago(now, u.Last)))
	width := min(48, max(12, m.width-m.leftWidth()-8))
	out = append(out, "", s.faint.Render("last 24 hours"), s.ok.Render(spark(u.Recent, width)))
	return out
}

func (m Model) stateHint(a accounts.Account) string {
	switch {
	case a.Disabled:
		return m.st.faint.Render("d enables it again, x removes it")
	case !a.Active:
		return m.st.faint.Render("enter makes sandboxes use this account")
	default:
		return m.st.faint.Render("this account is published to new sandboxes")
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
	for i, p := range accounts.Providers() {
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
	p, _ := accounts.ProviderByID(m.modal.login.Provider)
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
		row("enter, s", "use this account for its agent's new sandboxes"),
		row("d", "disable or enable the account"),
		row("x", "remove the account and its tokens"),
		row("r", "refresh the account's token now"),
		row("p", "publish to sandboxes now instead of within five minutes"),
		row("R", "read everything again"),
		row("j/k, g/G", "move"), "",
		s.faint.Render("Usage counts requests through the egress proxy and starts over"),
		s.faint.Render("when the proxy restarts. Running sandboxes keep the login they"),
		s.faint.Render("booted with; new ones get the account in use."),
	})
}
