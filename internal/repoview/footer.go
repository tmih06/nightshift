package repoview

import (
	"strings"

	bbHelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

const footerViewSeparator = " │ "

type footerHit int

const (
	footerHitNone footerHit = iota
	footerHitNotifications
	footerHitPRs
	footerHitIssues
	footerHitRepos
	footerHitHelp
)

type repoHelpKeys struct{}

func (repoHelpKeys) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (repoHelpKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			key.NewBinding(key.WithKeys("1", "2", "3"), key.WithHelp("1/2/3", "focus pane")),
			key.NewBinding(key.WithKeys("j", "k"), key.WithHelp("j/k", "move / scroll")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "first / last")),
			key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "collapse / expand")),
			key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[ / ]", "prev / next scope")),
		},
		{
			key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "pin / unpin repo")),
			key.NewBinding(key.WithKeys("J", "K"), key.WithHelp("J/K", "reorder pinned")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add / search repo")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter pane")),
			key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command palette")),
		},
		{
			key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sandbox checkout")),
			key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "focus active sandbox")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open PR / folder")),
			key.NewBinding(key.WithKeys("n", "d"), key.WithHelp("n / d", "new / delete")),
			key.NewBinding(key.WithKeys("c", "x", "R"), key.WithHelp("c/x/R", "comment/close/rerun")),
		},
		{
			key.NewBinding(key.WithKeys("<", ">", "v"), key.WithHelp("</>/v", "panes / zoom")),
			key.NewBinding(key.WithKeys("o", "y"), key.WithHelp("o / y", "browser / copy")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "switch gh view")),
			key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
		},
	}
}

func (m *Model) helpDrawerView() string {
	if !m.showHelp || m.width <= 0 {
		return ""
	}
	h := bbHelp.New()
	h.ShowAll = true
	h.Styles = m.styles.Help.BubbleStyles
	h.SetWidth(m.width)
	return h.View(repoHelpKeys{})
}

func (m *Model) footerHeight() int {
	if !m.showHelp {
		return 1
	}
	drawer := m.helpDrawerView()
	if drawer == "" {
		return 1
	}
	return 1 + len(strings.Split(drawer, "\n"))
}

func (m *Model) renderFooterBar(width int) string {
	if width <= 0 {
		return ""
	}

	var meta string
	var metaW int
	if m.selectedRepo != "" && m.viewer != "" {
		meta = m.ui.footerMetaPrimary.Render(m.selectedRepo) +
			m.ui.footerMetaDot +
			m.ui.footerMetaSecondary.Render("@"+m.viewer) +
			m.ui.footerMetaSep
		metaW = ansi.StringWidth(m.selectedRepo) + 3 + 1 + ansi.StringWidth(m.viewer) + 3
	} else if m.selectedRepo != "" {
		meta = m.ui.footerMetaPrimary.Render(m.selectedRepo) + m.ui.footerMetaSep
		metaW = ansi.StringWidth(m.selectedRepo) + 3
	} else if m.viewer != "" {
		meta = m.ui.footerMetaSecondary.Render("@"+m.viewer) + m.ui.footerMetaSep
		metaW = 1 + ansi.StringWidth(m.viewer) + 3
	}

	hints := m.paneFooterHints()
	hintStyle := m.ui.footerHintFaint
	if m.notice != "" {
		if m.noticeErr {
			hintStyle = m.ui.footerHintErr
		} else {
			hintStyle = m.ui.footerHintSuccess
		}
		hints = m.notice
	}

	helpInd := m.ui.footerHelpInd
	helpW := m.ui.footerHelpW

	left := m.ui.footerPrefix + meta
	leftW := m.ui.footerPrefixW + metaW

	availHints := width - leftW - helpW - 1
	var mid string
	var midW int
	if availHints > 3 {
		trunc := ansi.Truncate(hints, availHints, "…")
		mid = hintStyle.Render(trunc)
		midW = ansi.StringWidth(trunc)
	}

	combined := left + mid
	usedW := leftW + midW

	if usedW+helpW > width {
		keep := max(0, width-helpW)
		combined = ansi.Truncate(combined, keep, "")
		usedW = ansi.StringWidth(combined)
	}

	gap := max(0, width-usedW-helpW)
	spacing := m.ui.selectedBg.Render(strings.Repeat(" ", gap))

	bar := combined + spacing + helpInd
	totalW := usedW + gap + helpW
	if totalW > width {
		bar = ansi.Truncate(bar, width, "")
	} else if totalW < width {
		bar += strings.Repeat(" ", width-totalW)
	}
	return bar
}

func (m *Model) paneFooterHints() string {
	switch m.activePane {
	case PaneNav:
		return "enter expand · f pin · J/K order · a add · / filter · S sandbox · : cmds"
	case PaneList:
		switch m.activeScope {
		case ScopeBranches:
			return "n new · d del · S sandbox · / filter · : cmds"
		case ScopePRs:
			return "enter open · S sandbox · o browser · / filter · : cmds"
		case ScopeIssues:
			return "n new · c comment · x close · S sandbox · : cmds"
		case ScopeActions:
			return "R rerun · S sandbox · o browser · / filter · : cmds"
		case ScopeFiles:
			return "enter open · h up · S sandbox · y copy · : cmds"
		}
	case PaneDetails:
		return "j/k scroll · v zoom · S sandbox · o browser · y copy · : cmds"
	}
	return ": commands · ? help"
}

func (m *Model) renderFooterLines(width int) []string {
	key := footerCacheKey{
		width:        width,
		selectedRepo: m.selectedRepo,
		viewer:       m.viewer,
		activePane:   m.activePane,
		activeScope:  m.activeScope,
		notice:       m.notice,
		noticeErr:    m.noticeErr,
		showHelp:     m.showHelp,
	}
	if m.footerCache.valid && m.footerCache.key == key {
		return m.footerCache.lines
	}

	lines := []string{m.renderFooterBar(width)}
	if m.showHelp {
		drawer := m.helpDrawerView()
		if drawer != "" {
			for _, l := range strings.Split(drawer, "\n") {
				lines = append(lines, padLine(l, width))
			}
		}
	}
	m.footerCache = footerCacheState{
		valid: true,
		key:   key,
		lines: lines,
	}
	return lines
}

func (m *Model) footerHitTest(x int) footerHit {
	if x < 0 || x >= m.width {
		return footerHitNone
	}
	if x >= m.width-m.ui.footerHelpW {
		return footerHitHelp
	}
	pos := 0
	if x < pos+m.ui.footerNotifW {
		return footerHitNotifications
	}
	pos += m.ui.footerNotifW + m.ui.footerSepW

	if x >= pos && x < pos+m.ui.footerPRsW {
		return footerHitPRs
	}
	pos += m.ui.footerPRsW + m.ui.footerSepW

	if x >= pos && x < pos+m.ui.footerIssuesW {
		return footerHitIssues
	}
	pos += m.ui.footerIssuesW + m.ui.footerSepW

	if x >= pos && x < pos+m.ui.footerReposW {
		return footerHitRepos
	}
	return footerHitNone
}
