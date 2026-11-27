package accountsview

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// section is a heading with a rule to the pane's edge and a note at its end.
func (m Model) section(name, note string, width int) string {
	head := m.st.head.Render(name)
	used := lipgloss.Width(head) + 1
	if note != "" && used+lipgloss.Width(note)+3 <= width {
		rule := width - used - lipgloss.Width(note) - 1
		return head + " " + m.st.faint.Render(strings.Repeat("─", rule)+" "+note)
	}
	out := head + " " + m.st.faint.Render(strings.Repeat("─", max(0, width-used)))
	if note != "" {
		out += " " + m.st.faint.Render(note)
	}
	return out
}

// detailHead names the account; it stays put while the rest scrolls.
func (m Model) detailHead(a v1.Account) []string {
	s := m.st
	p := providerByID(m.snap, a.Provider)
	title := s.title.Render(label(a))
	_, known := inUse(m.snap, a.Provider)
	switch {
	case a.ActiveNow && a.Active:
		title += "  " + s.ok.Render("active on the chain")
	case a.ActiveNow:
		title += "  " + s.ok.Render("active on the chain (failover)")
	case m.pending(a):
		title += "  " + s.warn.Render("first · waiting for the proxy")
	case a.Active && known:
		title += "  " + s.warn.Render("first, but not active")
	case a.Active:
		title += "  " + s.faint.Render("first in the chain")
	case a.Priority > 1:
		title += "  " + s.faint.Render(fmt.Sprintf("fallback #%d", a.Priority))
	}
	return []string{title, s.faint.Render(p.Name + " · " + a.Name), ""}
}

// detailBody is everything about the account under its name.
func (m Model) detailBody(a v1.Account, width int) []string {
	s := m.st
	now := m.now()
	p := providerByID(m.snap, a.Provider)
	rows := m.statusRows(a, p)
	rows = append(rows,
		[2]string{"Token", "expires " + until(now, a.ExpiresAt)},
		[2]string{"Refreshed", ago(now, a.LastRefreshAt) + nextCheck(now, a)},
	)
	if pub, ok := m.snap.Published[a.Provider]; ok && a.Active {
		rows = append(rows, [2]string{"Published", ago(now, m.snap.PublishedAt) + s.faint.Render(" as "+pub.Label)})
	}
	w := 0
	for _, r := range rows {
		w = max(w, lipgloss.Width(r[0]))
	}
	out := make([]string, 0, 64)
	for _, r := range rows {
		out = append(out, s.faint.Render(pad(r[0], w))+"  "+r[1])
	}
	out = append(out, "")
	out = append(out, m.quotaBlock(a, width)...)
	out = append(out, "")
	if models := m.modelsBlock(a, width); len(models) > 0 {
		out = append(append(out, models...), "")
	}
	out = append(out, m.activityBlock(a, width)...)
	out = append(out, "")
	out = append(out, m.usageBlock(a, width)...)
	return append(out, "", m.stateHint(a))
}

// detail is the pane of the selected account, height lines at most. When the
// body is longer, the last line says how much more there is.
func (m Model) detail(a v1.Account, width, height int) []string {
	head, body := m.detailHead(a), m.detailBody(a, width)
	room := max(1, height-len(head))
	if len(body) <= room {
		return append(head, body...)
	}
	room = max(1, room-1)
	top := max(0, min(m.scroll, len(body)-room))
	more := fmt.Sprintf("↑ %d  ↓ %d  ·  ctrl+u/ctrl+d or wheel", top, len(body)-room-top)
	head = append(head, body[top:top+room]...)
	return append(head, m.st.faint.Render(more))
}

// detailSize is the width and height of the detail pane. On a narrow screen it
// sits under the list.
func (m Model) detailSize() (width, height int) {
	if m.width >= 90 {
		return m.width - m.leftWidth() - 3, m.bodyHeight()
	}
	listed := min(m.listHeight(), max(0, len(m.listLines())-m.top))
	return m.width, max(1, m.bodyHeight()-listed-1)
}

// scrollDetail moves the detail pane by delta lines, within what it holds.
func (m *Model) scrollDetail(delta int) {
	a, ok := m.selected()
	if !ok {
		return
	}
	width, height := m.detailSize()
	room := max(1, height-len(m.detailHead(a))-1)
	last := max(0, len(m.detailBody(a, width))-room)
	m.scroll = max(0, min(last, min(m.scroll, last)+delta))
}

// nextCheck says when CLIProxyAPI will next look at the account's token.
func nextCheck(now time.Time, a v1.Account) string {
	if !a.NextRefreshAt.After(now) {
		return ""
	}
	return "  ·  next check " + until(now, a.NextRefreshAt)
}

// stateText is a's state by name, in the color that says how it is doing.
func (m Model) stateText(state string) string { return m.stateStyle(state).Render(stateName(state)) }

func (m Model) stateStyle(state string) lipgloss.Style {
	switch state {
	case v1.AccountOK:
		return m.st.ok
	case v1.AccountExpiring:
		return m.st.warn
	case v1.AccountFailing, v1.AccountSignIn:
		return m.st.bad
	}
	return m.st.faint
}

// statusRows are the detail pane's rows about the account's health.
func (m Model) statusRows(a v1.Account, p v1.Provider) [][2]string {
	s := m.st
	rows := append([][2]string{{"Status", m.stateText(a.State)}}, m.chainRows(a)...)
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

// chainRows are the rows about the account's place in its fallback chain.
func (m Model) chainRows(a v1.Account) [][2]string {
	s := m.st
	now := m.now()
	var rows [][2]string
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
	if a.CooldownUntil.After(now) {
		cd := "until " + until(now, a.CooldownUntil)
		if a.CooldownReason != "" {
			cd += " (" + a.CooldownReason + ")"
		}
		rows = append(rows, [2]string{"Cooldown", s.warn.Render(cd)})
	}
	if a.Failovers > 0 {
		rows = append(rows, [2]string{"Failovers", strconv.Itoa(a.Failovers)})
	}
	return rows
}

func (m Model) stateHint(a v1.Account) string {
	switch {
	case a.Disabled:
		return m.st.faint.Render("d enables it again, x removes it")
	case !a.Active:
		return m.st.faint.Render("u makes this account active · K/J reorders the fallback chain")
	default:
		return m.st.faint.Render("first in the chain · K/J reorders it")
	}
}
