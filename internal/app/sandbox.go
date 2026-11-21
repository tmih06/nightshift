package app

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/dashboard"
	"github.com/tmih06/nightshift/internal/dialog"
	ghtui "github.com/tmih06/nightshift/internal/ghdash/tui"
)

// picker asks which pool's template a pull request's or issue's sandbox starts
// from (the choice of agent), and for an issue also asks for the branch name
// to check out. The sandbox starts cold on its volume, as a running pod cannot
// take a new volume.
type picker struct {
	repo   string
	number int
	url    string
	branch string // PR head branch; issues edit branchInput instead
	issue  bool
	pools  []cluster.Pool
	cursor int

	branchInput textinput.Model
	focusBranch bool
	branchErr   string
}

// openPicker answers S on a pull request with the pools the fleet shows.
func (m Model) openPicker(msg ghtui.SandboxPullRequestMsg) (Model, tea.Cmd) {
	if _, name, ok := strings.Cut(msg.Repo, "/"); !ok || name == "" || msg.Number <= 0 {
		return m.noticeErr(fmt.Sprintf("cannot check out %q", msg.URL))
	}
	pools := m.sandboxes.WarmPools()
	if len(pools) == 0 {
		return m.noticeErr("no warm pools to claim from: is the cluster up?")
	}
	m.picker = &picker{
		repo:   msg.Repo,
		number: msg.Number,
		url:    msg.URL,
		branch: msg.Branch,
		pools:  pools,
	}
	return m, nil
}

// openIssuePicker answers S on an issue with a branch name input and the pools
// the fleet shows.
func (m Model) openIssuePicker(msg ghtui.SandboxIssueMsg) (Model, tea.Cmd) {
	if _, name, ok := strings.Cut(msg.Repo, "/"); !ok || name == "" || msg.Number <= 0 {
		return m.noticeErr(fmt.Sprintf("cannot check out %q", msg.URL))
	}
	pools := m.sandboxes.WarmPools()
	if len(pools) == 0 {
		return m.noticeErr("no warm pools to claim from: is the cluster up?")
	}
	defaultBranch := fmt.Sprintf("issue-%d", msg.Number)
	ti := textinput.New()
	ti.SetVirtualCursor(true)
	ti.Prompt = "▸ "
	ti.Placeholder = defaultBranch
	ti.CharLimit = 200
	base := lipgloss.NewStyle()
	ti.SetStyles(textinput.Styles{
		Focused: textinput.StyleState{
			Prompt:      base.Foreground(m.theme.SuccessText).Bold(true),
			Text:        base.Foreground(m.theme.PrimaryText),
			Placeholder: base.Foreground(m.theme.FaintText),
		},
		Blurred: textinput.StyleState{
			Prompt:      base.Foreground(m.theme.FaintText),
			Text:        base.Foreground(m.theme.SecondaryText),
			Placeholder: base.Foreground(m.theme.FaintText),
		},
		Cursor: textinput.CursorStyle{
			Color: m.theme.PrimaryText,
			Shape: tea.CursorBar,
			Blink: false,
		},
	})
	ti.SetValue(defaultBranch)
	ti.CursorEnd()
	ti.Focus()
	m.picker = &picker{
		repo:        msg.Repo,
		number:      msg.Number,
		url:         msg.URL,
		issue:       true,
		pools:       pools,
		branchInput: ti,
		focusBranch: true,
	}
	return m, nil
}

func (m Model) updatePicker(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
	if p.issue && p.focusBranch {
		return m.updatePickerBranch(msg)
	}
	menu := m.pickerMenu()
	switch s := msg.String(); s {
	case "esc", "q", "ctrl+c":
		m.picker = nil
		return m, nil
	case "tab", "shift+tab":
		if p.issue {
			p.focusBranch = true
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
		if s == "shift+tab" {
			menu.Move(-1)
		} else {
			menu.Move(1)
		}
	case "up", "k":
		menu.Move(-1)
	case "down", "j":
		menu.Move(1)
	case "enter":
		return m.pickPool(p.cursor)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.pools) {
			return m.pickPool(int(s[0] - '1'))
		}
	}
	p.cursor = menu.Cursor
	m.picker = &p
	return m, nil
}

func (m Model) updatePickerBranch(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
	menu := m.pickerMenu()
	switch msg.String() {
	case "esc", "ctrl+c":
		m.picker = nil
		return m, nil
	case "tab", "shift+tab":
		p.focusBranch = false
		p.branchInput.Blur()
		m.picker = &p
		return m, nil
	case "up":
		menu.Move(-1)
		p.cursor = menu.Cursor
		m.picker = &p
		return m, nil
	case "down":
		menu.Move(1)
		p.cursor = menu.Cursor
		m.picker = &p
		return m, nil
	case "enter":
		return m.pickPool(p.cursor)
	}
	p.branchInput.SetWidth(m.pickerInputWidth(&p))
	p.branchInput, _ = p.branchInput.Update(msg)
	p.branchErr = ""
	m.picker = &p
	return m, nil
}

func (m Model) updatePickerPaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	p := *m.picker
	if p.issue && p.focusBranch {
		p.branchInput.SetWidth(m.pickerInputWidth(&p))
		p.branchInput, _ = p.branchInput.Update(msg)
		p.branchErr = ""
		m.picker = &p
	}
	return m, nil
}

// pickPool starts the checkout from the chosen pool and moves to the
// sandbox tab, which shows its progress.
func (m Model) pickPool(i int) (Model, tea.Cmd) {
	p := *m.picker
	branch := p.branch
	if p.issue {
		branch = strings.TrimSpace(p.branchInput.Value())
		if reason := validateBranchName(branch); reason != "" {
			p.cursor = i
			p.branchErr = reason
			p.focusBranch = true
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
	}
	m.picker = nil
	m.active = TabSandboxes
	m.sandboxes = m.sandboxes.SetHidden(false)
	var cmd tea.Cmd
	m.sandboxes, cmd = m.sandboxes.CheckOut(p.pools[i].Name,
		dashboard.Checkout{Repo: p.repo, Number: p.number, Branch: branch, Issue: p.issue})
	return m, cmd
}

// validateBranchName checks git check-ref-format rules for a branch name and
// returns "" when valid, or a short reason otherwise.
func validateBranchName(name string) string {
	if name == "" {
		return "enter a branch name"
	}
	if name == "@" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") ||
		strings.Contains(name, "..") || strings.Contains(name, "//") ||
		strings.Contains(name, "@{") || strings.Contains(name, "/.") ||
		strings.Contains(name, ".lock/") {
		return "invalid git branch name"
	}
	for _, r := range name {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return "invalid git branch name"
		}
	}
	return ""
}

// pickerMouse picks a pool on a click on its row, focuses the branch field on
// a click there, moves the choice with the wheel, and closes the picker on a
// click outside it.
func (m Model) pickerMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	placed := m.pickerPlaced()
	mouse := msg.Mouse()
	y := mouse.Y - barHeight
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		p := *m.picker
		if msg.Button == tea.MouseWheelUp {
			p.cursor = max(0, p.cursor-1)
		} else if msg.Button == tea.MouseWheelDown {
			p.cursor = min(len(p.pools)-1, p.cursor+1)
		}
		m.picker = &p
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if !placed.Inside(mouse.X, y) {
			m.picker = nil
			return m, nil
		}
		if placed.OnField(mouse.X, y) {
			p := *m.picker
			p.focusBranch = true
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
		if i, ok := placed.OptionAt(mouse.X, y); ok {
			return m.pickPool(i)
		}
	}
	return m, nil
}

func (m Model) pickerInputWidth(p *picker) int {
	nameW, tmplW := 0, 0
	for _, pool := range p.pools {
		nameW = max(nameW, lipgloss.Width(pool.Name))
		tmplW = max(tmplW, lipgloss.Width(pool.Template))
	}
	optW := lipgloss.Width(fmt.Sprintf(" %d  %-*s  %-*s ", len(p.pools), nameW, "", tmplW, ""))
	title := fmt.Sprintf("Check out %s#%d in a sandbox", p.repo, p.number)
	vol := "volume " + cluster.IssueVolumeName(p.repo, p.number)
	hint := "tab switch · ↑/↓ agent · enter start · esc cancel"
	inner := max(44, lipgloss.Width(title), lipgloss.Width(vol), lipgloss.Width(hint), optW)
	inner = min(inner, max(1, m.width-4))
	return max(1, inner-lipgloss.Width(p.branchInput.Prompt)-1)
}

func (m Model) pickerMenu() dialog.Menu {
	p := m.picker
	nameW, tmplW := 0, 0
	for _, pool := range p.pools {
		nameW = max(nameW, lipgloss.Width(pool.Name))
		tmplW = max(tmplW, lipgloss.Width(pool.Template))
	}
	opts := make([]dialog.Option, len(p.pools))
	for i, pool := range p.pools {
		opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s  %-*s ", i+1, nameW, pool.Name, tmplW, pool.Template)}
	}
	title := fmt.Sprintf("Check out %s#%d in a sandbox", p.repo, p.number)
	if p.issue {
		ti := p.branchInput
		ti.SetWidth(m.pickerInputWidth(p))
		return dialog.Menu{
			Title:        title,
			Lines:        []string{"volume " + cluster.IssueVolumeName(p.repo, p.number)},
			Field:        &dialog.Field{Label: "Branch", View: ti.View(), Focused: p.focusBranch, Err: p.branchErr},
			OptionsLabel: "Pool",
			Options:      opts,
			Cursor:       p.cursor,
			Hint:         "tab switch · ↑/↓ agent · enter start · esc cancel",
		}
	}
	branch := p.branch
	if branch == "" {
		branch = fmt.Sprintf("pr-%d", p.number)
	}
	return dialog.Menu{
		Title:   title,
		Lines:   []string{"branch " + branch + " · volume " + cluster.VolumeName(p.repo, p.number)},
		Options: opts,
		Cursor:  p.cursor,
		Hint:    "↑/↓ choose agent · enter start · esc cancel",
	}
}

func (m Model) pickerPlaced() dialog.Placed {
	return m.pickerMenu().Place(m.theme, m.width, max(0, m.height-barHeight))
}

// overPicker draws the picker over the body.
func (m Model) overPicker(body string) string {
	p := m.pickerPlaced()
	return dialog.Overlay(body, p.Box, p.X, p.Y, max(0, m.height-barHeight))
}
