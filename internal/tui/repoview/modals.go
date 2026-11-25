package repoview

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/ghdash/config"
	"github.com/tmih06/nightshift/internal/tui/dialog"
)

type paletteCommand struct {
	Category string
	Label    string
	Key      string
	Run      func(*Model) tea.Cmd
}

type modalBounds struct {
	x, y, w, h int
	optTop     int
	optCount   int
}

func (b modalBounds) inside(x, y int) bool {
	return b.w > 0 && b.h > 0 && x >= b.x && x < b.x+b.w && y >= b.y && y < b.y+b.h
}

func (b modalBounds) optionAt(x, y int) (int, bool) {
	if !b.inside(x, y) || b.optCount <= 0 {
		return 0, false
	}
	idx := y - b.optTop
	if idx < 0 || idx >= b.optCount {
		return 0, false
	}
	return idx, true
}

func (m *Model) paletteCommands() []paletteCommand {
	var cmds []paletteCommand

	// Scope-specific commands first so context actions are easiest to reach.
	switch m.activeScope {
	case ScopeBranches:
		cmds = append(cmds, paletteCommand{
			Category: "Branches",
			Label:    "New branch",
			Key:      "n",
			Run:      func(m *Model) tea.Cmd { return m.openNewBranchModal() },
		})
		if row, ok := m.currentListRow(); ok && row.kind == listBranch && row.branch != nil && !row.branch.IsDefault {
			bName := row.branch.Name
			cmds = append(cmds, paletteCommand{
				Category: "Branches",
				Label:    "Delete branch " + bName,
				Key:      "d",
				Run:      func(m *Model) tea.Cmd { return m.promptDeleteBranch(bName) },
			})
		}
	case ScopePRs:
		if row, ok := m.currentListRow(); ok && row.kind == listPR && row.pr != nil {
			pr := *row.pr
			cmds = append(cmds, paletteCommand{
				Category: "Pull Requests",
				Label:    fmt.Sprintf("Open PR #%d in full view", pr.Number),
				Key:      "enter",
				Run:      func(m *Model) tea.Cmd { return m.emitOpenPR(pr) },
			})
		}
	case ScopeIssues:
		cmds = append(cmds, paletteCommand{
			Category: "Issues",
			Label:    "New issue",
			Key:      "n",
			Run:      func(m *Model) tea.Cmd { return m.openNewIssueModal() },
		})
		if row, ok := m.currentListRow(); ok && row.kind == listIssue && row.issue != nil {
			iss := *row.issue
			cmds = append(cmds, paletteCommand{
				Category: "Issues",
				Label:    fmt.Sprintf("Comment on issue #%d", iss.Number),
				Key:      "c",
				Run:      func(m *Model) tea.Cmd { return m.openCommentIssueModal(iss.Number) },
			})
			if strings.EqualFold(iss.State, "OPEN") {
				cmds = append(cmds, paletteCommand{
					Category: "Issues",
					Label:    fmt.Sprintf("Close issue #%d", iss.Number),
					Key:      "x",
					Run:      func(m *Model) tea.Cmd { return m.promptSetIssueState(iss.Number, "closed") },
				})
			} else {
				cmds = append(cmds, paletteCommand{
					Category: "Issues",
					Label:    fmt.Sprintf("Reopen issue #%d", iss.Number),
					Key:      "X",
					Run:      func(m *Model) tea.Cmd { return m.promptSetIssueState(iss.Number, "open") },
				})
			}
		}
	case ScopeActions:
		if row, ok := m.currentListRow(); ok && row.kind == listRun && row.run != nil {
			run := *row.run
			cmds = append(cmds,
				paletteCommand{
					Category: "Actions",
					Label:    fmt.Sprintf("Rerun failed jobs (#%d)", run.RunNumber),
					Key:      "R",
					Run:      func(m *Model) tea.Cmd { return m.promptRerunWorkflow(run, true) },
				},
				paletteCommand{
					Category: "Actions",
					Label:    fmt.Sprintf("Rerun all jobs (#%d)", run.RunNumber),
					Key:      "",
					Run:      func(m *Model) tea.Cmd { return m.promptRerunWorkflow(run, false) },
				},
			)
		}
	case ScopeFiles:
		if m.filePath != "" {
			cmds = append(cmds, paletteCommand{
				Category: "Files",
				Label:    "Go to parent directory",
				Key:      "h",
				Run:      func(m *Model) tea.Cmd { return m.goToParentDir() },
			})
		}
	}

	// Sandbox commands.
	if m.selectedRepo != "" {
		cmds = append(cmds, paletteCommand{
			Category: "Sandbox",
			Label:    "Launch sandbox for selection",
			Key:      "S",
			Run:      func(m *Model) tea.Cmd { return m.launchSandboxForSelection() },
		})
		if pod, ok := m.activeSandboxForSelection(); ok {
			cmds = append(cmds, paletteCommand{
				Category: "Sandbox",
				Label:    fmt.Sprintf("Focus active sandbox (%s)", pod),
				Key:      "b",
				Run: func(m *Model) tea.Cmd {
					return func() tea.Msg { return FocusSandboxMsg{Pod: pod} }
				},
			})
		}
	}

	// Repository commands.
	pinLabel := "Pin repository"
	if m.isPinned(m.selectedRepo) {
		pinLabel = "Unpin repository"
	}
	cmds = append(cmds,
		paletteCommand{
			Category: "Repository",
			Label:    "Add / search repository",
			Key:      "a",
			Run:      func(m *Model) tea.Cmd { return m.openAddRepoModal() },
		},
		paletteCommand{
			Category: "Repository",
			Label:    pinLabel,
			Key:      "f",
			Run:      func(m *Model) tea.Cmd { return m.handleTogglePin() },
		},
		paletteCommand{
			Category: "Repository",
			Label:    "Refresh current view",
			Key:      "r",
			Run:      func(m *Model) tea.Cmd { return m.refreshActive() },
		},
		paletteCommand{
			Category: "Repository",
			Label:    "Open in browser",
			Key:      "o",
			Run:      func(m *Model) tea.Cmd { return m.openSelectionInBrowser() },
		},
		paletteCommand{
			Category: "Repository",
			Label:    "Copy URL / reference",
			Key:      "y",
			Run:      func(m *Model) tea.Cmd { return m.copySelection(false) },
		},
	)

	// Scope navigation commands.
	for _, sc := range allScopes {

		cmds = append(cmds, paletteCommand{
			Category: "Scope",
			Label:    "Switch to " + sc.String(),
			Key:      "",
			Run:      func(m *Model) tea.Cmd { return m.setScope(sc) },
		})
	}

	// Layout commands.
	cmds = append(cmds,
		paletteCommand{
			Category: "Layout",
			Label:    "Toggle Repos pane",
			Key:      "<",
			Run:      func(m *Model) tea.Cmd { m.hideNav = !m.hideNav; return nil },
		},
		paletteCommand{
			Category: "Layout",
			Label:    "Toggle Details pane",
			Key:      ">",
			Run:      func(m *Model) tea.Cmd { m.hideDetails = !m.hideDetails; return nil },
		},
		paletteCommand{
			Category: "Layout",
			Label:    "Toggle Zoom Details",
			Key:      "v",
			Run:      func(m *Model) tea.Cmd { m.zoomDetails = !m.zoomDetails; return nil },
		},
	)

	// GitHub views.
	cmds = append(cmds,
		paletteCommand{
			Category: "Views",
			Label:    "Switch to Notifications",
			Key:      "s",
			Run:      func(m *Model) tea.Cmd { return func() tea.Msg { return SwitchViewMsg{View: config.NotificationsView} } },
		},
		paletteCommand{
			Category: "Views",
			Label:    "Switch to PRs dashboard",
			Key:      "",
			Run:      func(m *Model) tea.Cmd { return func() tea.Msg { return SwitchViewMsg{View: config.PRsView} } },
		},
		paletteCommand{
			Category: "Views",
			Label:    "Switch to Issues dashboard",
			Key:      "",
			Run:      func(m *Model) tea.Cmd { return func() tea.Msg { return SwitchViewMsg{View: config.IssuesView} } },
		},
	)

	return cmds
}

func (m *Model) filteredPaletteCommands() []paletteCommand {
	all := m.paletteCommands()
	q := strings.ToLower(strings.TrimSpace(m.paletteInput.Value()))
	if q == "" {
		return all
	}
	var out []paletteCommand
	for _, c := range all {
		hay := strings.ToLower(c.Category + " " + c.Label + " " + c.Key)
		if strings.Contains(hay, q) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) closeModal() {
	m.modal = ModalNone
	m.confirm = nil
	m.modalErr = ""
	m.paletteInput.Blur()
	m.addRepoInput.Blur()
	m.branchInput.Blur()
	m.issueTitleInput.Blur()
	m.issueBodyInput.Blur()
	m.commentInput.Blur()
}

func (m *Model) openPaletteModal() tea.Cmd {
	m.closeModal()
	m.modal = ModalPalette
	m.paletteInput.SetValue("")
	m.paletteCursor = 0
	return m.paletteInput.Focus()
}

func (m *Model) openAddRepoModal() tea.Cmd {
	m.closeModal()
	m.modal = ModalAddRepo
	m.addRepoInput.SetValue("")
	m.addRepoResults = nil
	m.addRepoCursor = 0
	m.addRepoSearching = false
	return m.addRepoInput.Focus()
}

func (m *Model) openNewBranchModal() tea.Cmd {
	if m.selectedRepo == "" {
		return m.setNotice("Select a repository first", true)
	}
	m.closeModal()
	m.modal = ModalNewBranch
	m.branchBase = m.defaultOrSelectedBranch()
	m.branchInput.SetValue("")
	return m.branchInput.Focus()
}

func (m *Model) openNewIssueModal() tea.Cmd {
	if m.selectedRepo == "" {
		return m.setNotice("Select a repository first", true)
	}
	m.closeModal()
	m.modal = ModalNewIssue
	m.issueTitleInput.SetValue("")
	m.issueBodyInput.SetValue("")
	m.issueFocusBody = false
	return m.issueTitleInput.Focus()
}

func (m *Model) openCommentIssueModal(number int) tea.Cmd {
	if m.selectedRepo == "" || number <= 0 {
		return m.setNotice("Select an issue first", true)
	}
	m.closeModal()
	m.modal = ModalCommentIssue
	m.commentIssueNum = number
	m.commentInput.SetValue("")
	return m.commentInput.Focus()
}

func (m *Model) promptDeleteBranch(branch string) tea.Cmd {
	m.closeModal()
	m.modal = ModalConfirm
	m.confirmCursor = 0
	m.confirm = &confirmAction{
		title:  "Delete branch?",
		detail: fmt.Sprintf("Delete refs/heads/%s in %s", branch, m.selectedRepo),
		kind:   confirmDeleteBranch,
		repo:   m.selectedRepo,
		branch: branch,
		danger: true,
	}
	return nil
}

func (m *Model) promptSetIssueState(number int, nextState string) tea.Cmd {
	m.closeModal()
	m.modal = ModalConfirm
	m.confirmCursor = 0
	verb := "Close"
	danger := true
	if strings.EqualFold(nextState, "open") {
		verb = "Reopen"
		danger = false
	}
	m.confirm = &confirmAction{
		title:     fmt.Sprintf("%s issue #%d?", verb, number),
		detail:    fmt.Sprintf("%s issue #%d in %s", verb, number, m.selectedRepo),
		kind:      confirmSetIssueState,
		repo:      m.selectedRepo,
		issueNum:  number,
		nextState: nextState,
		danger:    danger,
	}
	return nil
}

func (m *Model) promptRerunWorkflow(run WorkflowRun, failedOnly bool) tea.Cmd {
	m.closeModal()
	m.modal = ModalConfirm
	m.confirmCursor = 0
	mode := "all jobs"
	if failedOnly {
		mode = "failed jobs"
	}
	m.confirm = &confirmAction{
		title:      fmt.Sprintf("Rerun %s?", mode),
		detail:     fmt.Sprintf("%s #%d on %s (%s)", run.Name, run.RunNumber, run.Branch, m.selectedRepo),
		kind:       confirmRerunWorkflow,
		repo:       m.selectedRepo,
		runID:      run.ID,
		failedOnly: failedOnly,
		danger:     false,
	}
	return nil
}

func (m *Model) updateModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "esc" || k == "ctrl+c" {
		m.closeModal()
		return m, nil
	}

	switch m.modal {
	case ModalPalette:
		cmds := m.filteredPaletteCommands()
		switch k {
		case "up", "ctrl+p":
			if len(cmds) > 0 {
				m.paletteCursor = (m.paletteCursor - 1 + len(cmds)) % len(cmds)
			}
			return m, nil
		case "down", "ctrl+n":
			if len(cmds) > 0 {
				m.paletteCursor = (m.paletteCursor + 1) % len(cmds)
			}
			return m, nil
		case "enter":
			if len(cmds) > 0 {
				if m.paletteCursor >= len(cmds) {
					m.paletteCursor = 0
				}
				chosen := cmds[m.paletteCursor]
				m.closeModal()
				if chosen.Run != nil {
					return m, chosen.Run(m)
				}
			}
			m.closeModal()
			return m, nil
		}
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		m.paletteCursor = 0
		return m, cmd

	case ModalAddRepo:
		switch k {
		case "up", "ctrl+p":
			if len(m.addRepoResults) > 0 {
				m.addRepoCursor = (m.addRepoCursor - 1 + len(m.addRepoResults)) % len(m.addRepoResults)
			}
			return m, nil
		case "down", "ctrl+n":
			if len(m.addRepoResults) > 0 {
				m.addRepoCursor = (m.addRepoCursor + 1) % len(m.addRepoResults)
			}
			return m, nil
		case "ctrl+s":
			return m, m.triggerRepoSearch()
		case "enter":
			val := strings.TrimSpace(m.addRepoInput.Value())
			if len(m.addRepoResults) > 0 && m.addRepoCursor >= 0 && m.addRepoCursor < len(m.addRepoResults) {
				chosen := m.addRepoResults[m.addRepoCursor]
				m.closeModal()
				return m, m.addAndSelectRepo(chosen)
			}
			if looksLikeOwnerRepo(val) {
				m.closeModal()
				return m, m.addAndSelectRepoName(val)
			}
			if val == "" {
				m.modalErr = "Enter owner/repo or a search query"
				return m, nil
			}
			return m, m.triggerRepoSearch()
		}
		prevVal := m.addRepoInput.Value()
		var cmd tea.Cmd
		m.addRepoInput, cmd = m.addRepoInput.Update(msg)
		if m.addRepoInput.Value() != prevVal {
			m.addRepoResults = nil
			m.addRepoCursor = 0
		}
		m.modalErr = ""
		return m, cmd

	case ModalNewBranch:
		if k == "enter" {
			name := strings.TrimSpace(m.branchInput.Value())
			if name == "" {
				m.modalErr = "Branch name is required"
				return m, nil
			}
			if strings.ContainsAny(name, " \t") {
				m.modalErr = "Branch name cannot contain spaces"
				return m, nil
			}
			repo := m.selectedRepo
			base := m.branchBase
			m.closeModal()
			return m, m.createBranchCmd(repo, name, base)
		}
		var cmd tea.Cmd
		m.branchInput, cmd = m.branchInput.Update(msg)
		m.modalErr = ""
		return m, cmd

	case ModalNewIssue:
		switch k {
		case "tab", "shift+tab":
			m.issueFocusBody = !m.issueFocusBody
			if m.issueFocusBody {
				m.issueTitleInput.Blur()
				return m, m.issueBodyInput.Focus()
			}
			m.issueBodyInput.Blur()
			return m, m.issueTitleInput.Focus()
		case "ctrl+s":
			return m.submitNewIssue()
		case "enter":
			if !m.issueFocusBody {
				return m.submitNewIssue()
			}
			return m.submitNewIssue()
		}
		var cmd tea.Cmd
		if m.issueFocusBody {
			m.issueBodyInput, cmd = m.issueBodyInput.Update(msg)
		} else {
			m.issueTitleInput, cmd = m.issueTitleInput.Update(msg)
		}
		m.modalErr = ""
		return m, cmd

	case ModalCommentIssue:
		if k == "enter" || k == "ctrl+s" {
			body := strings.TrimSpace(m.commentInput.Value())
			if body == "" {
				m.modalErr = "Comment cannot be empty"
				return m, nil
			}
			repo := m.selectedRepo
			num := m.commentIssueNum
			m.closeModal()
			return m, m.commentIssueCmd(repo, num, body)
		}
		var cmd tea.Cmd
		m.commentInput, cmd = m.commentInput.Update(msg)
		m.modalErr = ""
		return m, cmd

	case ModalConfirm:
		switch k {
		case "j", "down", "k", "up", "tab", "shift+tab":
			m.confirmCursor = 1 - m.confirmCursor
			return m, nil
		case "n":
			m.closeModal()
			return m, nil
		case "y":
			return m.executeConfirm()
		case "enter":
			if m.confirmCursor == 0 {
				return m.executeConfirm()
			}
			m.closeModal()
			return m, nil
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) submitNewIssue() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.issueTitleInput.Value())
	if title == "" {
		m.modalErr = "Issue title is required"
		return m, nil
	}
	body := strings.TrimSpace(m.issueBodyInput.Value())
	repo := m.selectedRepo
	m.closeModal()
	return m, m.createIssueCmd(repo, title, body)
}

func (m *Model) executeConfirm() (tea.Model, tea.Cmd) {
	act := m.confirm
	m.closeModal()
	if act == nil {
		return m, nil
	}
	switch act.kind {
	case confirmDeleteBranch:
		return m, m.deleteBranchCmd(act.repo, act.branch)
	case confirmSetIssueState:
		return m, m.setIssueStateCmd(act.repo, act.issueNum, act.nextState)
	case confirmRerunWorkflow:
		return m, m.rerunWorkflowCmd(act.repo, act.runID, act.failedOnly)
	}
	return m, nil
}

func (m *Model) updateModalPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.modal {
	case ModalPalette:
		m.paletteInput, cmd = m.paletteInput.Update(msg)
	case ModalAddRepo:
		m.addRepoInput, cmd = m.addRepoInput.Update(msg)
	case ModalNewBranch:
		m.branchInput, cmd = m.branchInput.Update(msg)
	case ModalNewIssue:
		if m.issueFocusBody {
			m.issueBodyInput, cmd = m.issueBodyInput.Update(msg)
		} else {
			m.issueTitleInput, cmd = m.issueTitleInput.Update(msg)
		}
	case ModalCommentIssue:
		m.commentInput, cmd = m.commentInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleModalMouse(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if !m.modalBounds.inside(msg.X, msg.Y) {
		m.closeModal()
		return m, nil
	}
	idx, ok := m.modalBounds.optionAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	switch m.modal {
	case ModalConfirm:
		if idx == 0 {
			return m.executeConfirm()
		}
		m.closeModal()
		return m, nil
	case ModalPalette:
		cmds := m.filteredPaletteCommands()
		if idx >= 0 && idx < len(cmds) {
			chosen := cmds[idx]
			m.closeModal()
			if chosen.Run != nil {
				return m, chosen.Run(m)
			}
		}
	case ModalAddRepo:
		if idx >= 0 && idx < len(m.addRepoResults) {
			chosen := m.addRepoResults[idx]
			m.closeModal()
			return m, m.addAndSelectRepo(chosen)
		}
	}
	return m, nil
}

func (m *Model) renderModalOverlay(base string) string {
	if m.modal == ModalNone || m.width < 12 || m.height < 6 {
		return base
	}

	if m.modal == ModalConfirm && m.confirm != nil {
		menu := dialog.Menu{
			Title: m.confirm.title,
			Lines: []string{m.confirm.detail},
			Options: []dialog.Option{
				{Text: "Confirm", Danger: m.confirm.danger},
				{Text: "Cancel"},
			},
			Cursor: m.confirmCursor,
			Hint:   "y/enter confirm · n/esc cancel",
		}
		placed := menu.Place(m.theme, m.width, m.height)
		m.modalBounds = modalBounds{
			x:        placed.X,
			y:        placed.Y,
			w:        placed.W,
			h:        placed.H,
			optTop:   placed.OptionY(0),
			optCount: len(menu.Options),
		}
		return dialog.Overlay(base, placed.Box, placed.X, placed.Y, m.height)
	}

	innerW := min(64, max(24, m.width-6))
	plain := lipgloss.NewStyle()
	titleStyle := plain.Foreground(m.theme.PrimaryText).Bold(true)
	faintStyle := plain.Foreground(m.theme.FaintText)
	errStyle := plain.Foreground(m.theme.ErrorText)

	var lines []string
	optStart := -1
	optCount := 0

	switch m.modal {
	case ModalPalette:
		m.paletteInput.SetWidth(max(8, innerW-ansi.StringWidth(m.paletteInput.Prompt)-1))
		lines = append(lines, dialog.Line(titleStyle, "Command Palette", innerW))
		lines = append(lines, dialog.Line(plain, m.paletteInput.View(), innerW))
		lines = append(lines, dialog.Line(plain.Foreground(m.theme.FaintBorder), strings.Repeat("─", innerW), innerW))

		cmds := m.filteredPaletteCommands()
		maxVisible := min(12, max(3, m.height-10))
		if m.paletteCursor >= len(cmds) && len(cmds) > 0 {
			m.paletteCursor = len(cmds) - 1
		}
		if len(cmds) == 0 {
			lines = append(lines, dialog.Line(faintStyle, "No matching commands", innerW))
		} else {
			start := 0
			if m.paletteCursor >= maxVisible {
				start = m.paletteCursor - maxVisible + 1
			}
			end := min(len(cmds), start+maxVisible)
			optStart = len(lines)
			optCount = end - start
			for i := start; i < end; i++ {
				c := cmds[i]
				cat := lipgloss.NewStyle().Foreground(m.theme.FaintText).Render(fmt.Sprintf("%-13s", c.Category))
				lbl := lipgloss.NewStyle().Foreground(m.theme.PrimaryText).Render(c.Label)
				keyStr := ""
				if c.Key != "" {
					keyStr = lipgloss.NewStyle().Foreground(m.theme.SuccessText).Render(c.Key)
				}
				left := cat + " " + lbl
				gap := max(1, innerW-ansi.StringWidth(left)-ansi.StringWidth(keyStr))
				row := left + strings.Repeat(" ", gap) + keyStr
				if i == m.paletteCursor {
					lines = append(lines, m.paintRow(row, innerW, true))
				} else {
					lines = append(lines, padLine(row, innerW))
				}
			}
		}
		lines = append(lines, "", dialog.Line(faintStyle, "↑/↓ move · enter run · esc close", innerW))

	case ModalAddRepo:
		m.addRepoInput.SetWidth(max(8, innerW-ansi.StringWidth(m.addRepoInput.Prompt)-1))
		lines = append(lines, dialog.Line(titleStyle, "Add / Open Repository", innerW))
		lines = append(lines, dialog.Line(faintStyle, "Enter owner/repo to open directly, or search GitHub:", innerW))
		lines = append(lines, dialog.Line(plain, m.addRepoInput.View(), innerW))
		if m.modalErr != "" {
			lines = append(lines, dialog.Line(errStyle, m.modalErr, innerW))
		}
		if m.addRepoSearching {
			lines = append(lines, dialog.Line(faintStyle, "Searching GitHub…", innerW))
		} else if len(m.addRepoResults) > 0 {
			lines = append(lines, dialog.Line(plain.Foreground(m.theme.FaintBorder), strings.Repeat("─", innerW), innerW))
			maxVis := min(8, len(m.addRepoResults))
			optStart = len(lines)
			optCount = maxVis
			for i := 0; i < maxVis; i++ {
				r := m.addRepoResults[i]
				txt := r.NormalizedFullName()
				if r.Description != "" {
					txt += " — " + r.Description
				}
				if i == m.addRepoCursor {
					lines = append(lines, dialog.Line(plain.Foreground(m.theme.PrimaryText).Background(m.theme.SelectedBackground).Bold(true), txt, innerW))
				} else {
					lines = append(lines, dialog.Line(plain.Foreground(m.theme.PrimaryText), txt, innerW))
				}
			}
		}
		lines = append(lines, "", dialog.Line(faintStyle, "enter open/search · ctrl+s search · ↑/↓ pick · esc cancel", innerW))

	case ModalNewBranch:
		m.branchInput.SetWidth(max(8, innerW-ansi.StringWidth(m.branchInput.Prompt)-1))
		lines = append(lines, dialog.Line(titleStyle, "New Branch", innerW))
		lines = append(lines, dialog.Line(faintStyle, fmt.Sprintf("Repository: %s · Base: %s", m.selectedRepo, m.branchBase), innerW))
		lines = append(lines, "", dialog.Line(plain, m.branchInput.View(), innerW))
		if m.modalErr != "" {
			lines = append(lines, dialog.Line(errStyle, m.modalErr, innerW))
		}
		lines = append(lines, "", dialog.Line(faintStyle, "enter create · esc cancel", innerW))

	case ModalNewIssue:
		m.issueTitleInput.SetWidth(max(8, innerW-ansi.StringWidth(m.issueTitleInput.Prompt)-1))
		m.issueBodyInput.SetWidth(max(8, innerW-ansi.StringWidth(m.issueBodyInput.Prompt)-1))
		lines = append(lines, dialog.Line(titleStyle, fmt.Sprintf("New Issue (%s)", m.selectedRepo), innerW))
		titleLbl := faintStyle
		bodyLbl := faintStyle
		if !m.issueFocusBody {
			titleLbl = plain.Foreground(m.theme.SuccessText).Bold(true)
		} else {
			bodyLbl = plain.Foreground(m.theme.SuccessText).Bold(true)
		}
		lines = append(lines, "", dialog.Line(titleLbl, "Title", innerW))
		lines = append(lines, dialog.Line(plain, m.issueTitleInput.View(), innerW))
		lines = append(lines, "", dialog.Line(bodyLbl, "Body (optional)", innerW))
		lines = append(lines, dialog.Line(plain, m.issueBodyInput.View(), innerW))
		if m.modalErr != "" {
			lines = append(lines, dialog.Line(errStyle, m.modalErr, innerW))
		}
		lines = append(lines, "", dialog.Line(faintStyle, "tab switch field · enter/ctrl+s create · esc cancel", innerW))

	case ModalCommentIssue:
		m.commentInput.SetWidth(max(8, innerW-ansi.StringWidth(m.commentInput.Prompt)-1))
		lines = append(lines, dialog.Line(titleStyle, fmt.Sprintf("Comment on %s#%d", m.selectedRepo, m.commentIssueNum), innerW))
		lines = append(lines, "", dialog.Line(plain, m.commentInput.View(), innerW))
		if m.modalErr != "" {
			lines = append(lines, dialog.Line(errStyle, m.modalErr, innerW))
		}
		lines = append(lines, "", dialog.Line(faintStyle, "enter/ctrl+s post comment · esc cancel", innerW))
	}

	box := dialog.Box(m.theme, lines)
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	bx := max(0, (m.width-bw)/2)
	by := max(0, (m.height-bh)/2)
	m.modalBounds = modalBounds{
		x:        bx,
		y:        by,
		w:        bw,
		h:        bh,
		optTop:   by + 1 + optStart,
		optCount: optCount,
	}
	return dialog.Overlay(base, box, bx, by, m.height)
}

func looksLikeOwnerRepo(s string) bool {
	if strings.ContainsAny(s, " \t\n") {
		return false
	}
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}
