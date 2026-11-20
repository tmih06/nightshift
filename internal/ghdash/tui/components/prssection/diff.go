package prssection

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/ghdash/tui/common"
)

func (m Model) diff() tea.Cmd {
	currRowData := m.GetCurrRow()
	if currRowData == nil {
		return nil
	}

	return common.DiffPR(
		currRowData.GetNumber(),
		currRowData.GetRepoNameWithOwner(),
		m.Ctx.Config.GetFullScreenDiffPagerEnv(),
	)
}
