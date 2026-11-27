package accountsview

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// modelsBlock lists the models the account can be asked for, as its agent's
// CLI offers them: each name, and its id where there is room.
func (m Model) modelsBlock(a v1.Account, width int) []string {
	s := m.st
	var models []v1.AccountModel
	failed := ""
	if q := a.Quota; q != nil {
		models, failed = q.Models, q.ModelsError
	} else if !a.Disabled {
		return nil // the quota block already says to press r
	}
	note := ""
	if len(models) > 0 {
		note = fmt.Sprintf("%d available", len(models))
	}
	out := []string{m.section("Models", note, width)}
	if failed != "" {
		for _, l := range wrap("could not list them: "+failed, width) {
			out = append(out, s.warn.Render(l))
		}
	}
	if len(models) == 0 {
		switch {
		case a.Disabled:
			out = append(out, s.faint.Render("Enable the account to list its models."))
		case failed == "":
			out = append(out, s.faint.Render("No model list yet — press r to fetch it."))
		}
		return out
	}
	names := 0
	for _, mod := range models {
		names = max(names, lipgloss.Width(cmp.Or(mod.Name, mod.ID)))
	}
	for _, mod := range models {
		name := cmp.Or(mod.Name, mod.ID)
		line := s.text.Render(name)
		if names+2+len(mod.ID) <= width {
			line += strings.Repeat(" ", names-lipgloss.Width(name)+2) + s.faint.Render(mod.ID)
		}
		out = append(out, line)
	}
	return out
}
