package app

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/tui/context"
)

// barHeight is the view switcher's one row.
const barHeight = 1

const (
	tabSeparator = " │ "
	brand        = "NIGHTSHIFT"
)

// Nerd Font glyphs, as gh-dash's own view switcher uses.
var tabIcons = [...]string{
	TabSandboxes: "\uf1b2", // fa-cube
	TabGitHub:    "\uf09b", // fa-github: gh-dash covers PRs, issues, and notifications
	TabAccounts:  "\uf007", // fa-user: the coding-agent accounts
}

type span struct{ from, to int }

func truncate(s string, width int) string { return ansi.Truncate(s, width, "") }

// counts are the numbers shown beside the tabs that have one.
type counts struct{ sandboxes, accounts int }

func tabText(i int, c counts) string {
	text := tabIcons[i] + " " + tabLabels[i]
	switch {
	case i == int(TabSandboxes) && c.sandboxes > 0:
		text += " (" + strconv.Itoa(c.sandboxes) + ")"
	case i == int(TabAccounts) && c.accounts > 0:
		text += " (" + strconv.Itoa(c.accounts) + ")"
	}
	return text
}

func (m Model) counts() counts {
	return counts{sandboxes: m.sandboxes.Count(), accounts: m.accounts.Count()}
}

// tabSpans gives each tab's columns, left to right.
func tabSpans(count counts) []span {
	spans := make([]span, 0, len(tabLabels))
	x := 1
	for i := range tabLabels {
		w := lipgloss.Width(tabText(i, count))
		spans = append(spans, span{from: x, to: x + w})
		x += w + lipgloss.Width(tabSeparator)
	}
	return spans
}

func (m Model) tabAt(x int) (Tab, bool) {
	for i, s := range tabSpans(m.counts()) {
		if x >= s.from && x < s.to && s.to <= m.width {
			return Tab(i), true
		}
	}
	return 0, false
}

// renderBar draws the tabs the way gh-dash draws its view switcher: a filled
// bar, the active view bold with a green icon, the others faint, and the
// name of the program in the corner like gh-dash's DASH logo.
func (m Model) renderBar() string {
	s := m.styles
	bar := s.Common.FooterStyle
	var b strings.Builder
	b.WriteString(s.ViewSwitcher.ViewsSeparator.Render(" "))
	used := 1
	count := m.counts()
	for i := range tabLabels {
		if i > 0 {
			b.WriteString(s.ViewSwitcher.ViewsSeparator.Render(tabSeparator))
			used += lipgloss.Width(tabSeparator)
		}
		text := tabText(i, count)
		if Tab(i) == m.active {
			active := lipgloss.NewStyle().Background(s.ViewSwitcher.ActiveView.GetBackground()).Bold(true)
			b.WriteString(active.Foreground(m.theme.SuccessText).Render(tabIcons[i]) +
				active.Foreground(m.theme.PrimaryText).Render(strings.TrimPrefix(text, tabIcons[i])))
		} else {
			b.WriteString(s.ViewSwitcher.InactiveView.Render(text))
		}
		used += lipgloss.Width(text)
	}
	b.WriteString(s.ViewSwitcher.ViewsSeparator.Render(" "))
	used++

	logo := lipgloss.NewStyle().
		Background(lipgloss.Darken(context.LogoColor, 0.7)).
		Foreground(context.LogoColor).
		Bold(true).
		Padding(0, 1).
		Render(brand)
	text, bad := m.notice, m.noticeBad
	if text == "" && m.active != TabSandboxes {
		text = m.sandboxes.CheckoutLine() // a checkout's progress, while its tab is hidden
	}
	if text != "" {
		fg := m.theme.FaintText
		if bad {
			fg = m.theme.ErrorText
		}
		room := max(0, m.width-used-lipgloss.Width(logo)-2)
		notice := bar.Foreground(fg).Render(truncate(text, room) + " ")
		logo = notice + logo
	}
	rest := m.width - used - lipgloss.Width(logo)
	if rest < 1 {
		return truncate(b.String()+bar.Render(strings.Repeat(" ", max(0, m.width-used))), m.width)
	}
	return b.String() + bar.Render(strings.Repeat(" ", rest)) + logo
}
