package issueview

import (
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/ghdash/data"
	"github.com/tmih06/nightshift/internal/ghdash/tui/markdown"
	"github.com/tmih06/nightshift/internal/ghdash/utils"
)

type RenderedActivity struct {
	UpdatedAt      time.Time
	RenderedString string
}

func (m *Model) renderActivity() string {
	width := m.getIndentedContentWidth() - 2

	var activity []RenderedActivity
	for _, comment := range m.issue.Data.Comments.Nodes {
		renderedComment, err := m.renderComment(comment, width)
		if err != nil {
			continue
		}
		activity = append(activity, RenderedActivity{
			UpdatedAt:      comment.UpdatedAt,
			RenderedString: renderedComment,
		})
	}

	sort.Slice(activity, func(i, j int) bool {
		return activity[i].UpdatedAt.Before(activity[j].UpdatedAt)
	})

	body := ""
	bodyStyle := lipgloss.NewStyle().PaddingLeft(2)
	if len(activity) == 0 {
		body = renderEmptyState()
	} else {
		var renderedActivities []string
		for _, activity := range activity {
			renderedActivities = append(renderedActivities, activity.RenderedString)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, renderedActivities...)
	}

	return lipgloss.JoinVertical(lipgloss.Left, m.renderActivitiesTitle(), bodyStyle.Render(body))
}

func (m Model) renderActivitiesTitle() string {
	return m.ctx.Styles.Common.MainTextStyle.
		MarginBottom(1).
		Underline(true).
		Render(" Comments")
}

func renderEmptyState() string {
	return lipgloss.NewStyle().Italic(true).Render("No comments...")
}

func (m *Model) renderComment(
	comment data.IssueComment,
	width int,
) (string, error) {
	header := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.ctx.Theme.FaintBorder).Render(
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.ctx.Styles.Common.MainTextStyle.Render(comment.Author.Login),
			" ",
			lipgloss.NewStyle().
				Foreground(m.ctx.Theme.FaintText).
				Render(utils.TimeElapsed(comment.UpdatedAt)),
		))

	body := comment.Body
	if strings.Contains(body, "|") {
		body = lineCleanupRegex.ReplaceAllString(body, "")
	}
	body, err := markdown.Render(body, width, m.ctx)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		body,
	), err
}
