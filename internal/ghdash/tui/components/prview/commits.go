package prview

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	checks "github.com/dlvhdr/x/gh-checks"
	"github.com/tmih06/nightshift/internal/ghdash/tui/constants"
	"github.com/tmih06/nightshift/internal/ghdash/utils"
)

func (m *Model) renderCommits() string {
	main := m.ctx.Styles.Common.MainTextStyle
	faint := m.ctx.Styles.Common.FaintTextStyle
	fainter := lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintBorder)

	if !m.pr.Data.IsEnriched {
		return lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.WaitingGlyph,
			" ",
			faint.Render("Loading..."),
		)
	}

	commits := m.pr.Data.Enriched.AllCommits.Nodes
	heading := m.ctx.Styles.Common.MainTextStyle.MarginBottom(1).Underline(true).Render(
		fmt.Sprintf("%s  %d commits", constants.CommitIcon, len(commits)))

	contentWidth := m.getIndentedContentWidth()
	commitIcon := faint.Render(constants.VerticalCommitIcon)
	pipePrefix := fainter.Render("│ ")
	pipeSep := fainter.Render("│")
	committedWord := faint.Render(" committed ")
	agoWord := faint.Render(" ago")
	dotIcon := faint.Render(constants.SmallDotIcon)

	var out strings.Builder
	out.Grow(len(heading) + len(commits)*160)
	out.WriteString(heading)
	for i, commit := range commits {
		commit := commit.Commit
		name := commit.Author.User.Login
		if name == "" {
			name = commit.Author.Name
		}
		left := commitIcon + " " + main.Render(commit.MessageHeadline)
		right := faint.Render(commit.AbbreviatedOid)
		wright := lipgloss.Width(right)
		left = ansi.Truncate(left, max(0, contentWidth-wright-1), constants.Ellipsis)
		pad := fainter.Render(" " + strings.Repeat(constants.HorizontalLineIcon,
			max(1, contentWidth-lipgloss.Width(left)-wright)-1) + " ")

		title := left + pad + right

		statsStr := ""
		if commit.StatusCheckRollup.Contexts.TotalCount > 0 {
			stats := m.getStatusCheckRollupStats(commit.StatusCheckRollup)
			statsStr = " " + dotIcon + " " +
				m.commitStateSign(commit.StatusCheckRollup.State) + " " +
				faint.Render(fmt.Sprintf("%d/%d", stats.succeeded,
					commit.StatusCheckRollup.Contexts.TotalCount))
		}

		desc := pipePrefix +
			faint.Render("@"+name) +
			committedWord +
			faint.Render(utils.TimeElapsed(commit.CommittedDate)) +
			agoWord +
			statsStr

		out.WriteByte('\n')
		out.WriteString(title)
		out.WriteByte('\n')
		out.WriteString(desc)
		if i < len(commits)-1 {
			out.WriteByte('\n')
			out.WriteString(pipeSep)
		}
	}

	return out.String()
}

func (m *Model) commitStateSign(state checks.CommitState) string {
	switch state {
	case checks.CommitStateError, checks.CommitStateFailure:
		return m.ctx.Styles.Common.FailureGlyph
	case checks.CommitStatePending, checks.CommitStateExpected, checks.CommitStateUnknown:
		return m.ctx.Styles.Common.WaitingGlyph
	case checks.CommitStateSuccess:
		return m.ctx.Styles.Common.SuccessGlyph
	}

	return ""
}
