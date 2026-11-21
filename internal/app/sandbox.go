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

// picker asks which pool's template a pull request's, issue's, or repository's
// sandbox starts from (the choice of agent), and for an issue or repository
// also asks for the branch name to check out. The sandbox starts cold on its
// volume, as a running pod cannot take a new volume.
type picker struct {
	repo   string
	number int
	url    string
	branch string // PR head branch; issues and repos edit branchInput instead
	kind   dashboard.CheckoutKind
	pools  []cluster.Pool
	cursor int

	branchInput textinput.Model
	focusBranch bool
	branchErr   string
}

func (p picker) hasBranchInput() bool {
	return p.kind == dashboard.CheckoutIssue || p.kind == dashboard.CheckoutRepo
}

// prepareRepoPools validates owner/name and returns available warm pools, or
// an error notice string.
func (m Model) prepareRepoPools(repo, url string) ([]cluster.Pool, string) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		target := url
		if target == "" {
			target = repo
		}
		return nil, fmt.Sprintf("cannot check out %q", target)
	}
	pools := m.sandboxes.WarmPools()
	if len(pools) == 0 {
		return nil, "no warm pools to claim from: is the cluster up?"
	}
	return pools, ""
}

// preparePicker validates the target repository/number and returns available
// warm pools, or an error notice string.
func (m Model) preparePicker(repo string, number int, url string) ([]cluster.Pool, string) {
	if number <= 0 {
		return nil, fmt.Sprintf("cannot check out %q", url)
	}
	return m.prepareRepoPools(repo, url)
}

func (m Model) newBranchInput(defaultBranch string) textinput.Model {
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
	return ti
}

// openPicker answers S on a pull request with the pools the fleet shows.
func (m Model) openPicker(msg ghtui.SandboxPullRequestMsg) (Model, tea.Cmd) {
	pools, errMsg := m.preparePicker(msg.Repo, msg.Number, msg.URL)
	if errMsg != "" {
		return m.noticeErr(errMsg)
	}
	m.picker = &picker{
		repo:   msg.Repo,
		number: msg.Number,
		url:    msg.URL,
		branch: msg.Branch,
		kind:   dashboard.CheckoutPR,
		pools:  pools,
	}
	return m, nil
}

// openIssuePicker answers S on an issue with a branch name input and the pools
// the fleet shows.
func (m Model) openIssuePicker(msg ghtui.SandboxIssueMsg) (Model, tea.Cmd) {
	pools, errMsg := m.preparePicker(msg.Repo, msg.Number, msg.URL)
	if errMsg != "" {
		return m.noticeErr(errMsg)
	}
	defaultBranch := fmt.Sprintf("issue-%d", msg.Number)
	m.picker = &picker{
		repo:        msg.Repo,
		number:      msg.Number,
		url:         msg.URL,
		kind:        dashboard.CheckoutIssue,
		pools:       pools,
		branchInput: m.newBranchInput(defaultBranch),
		focusBranch: true,
	}
	return m, nil
}

// openRepoPicker answers S on a repository or branch with a branch name input
// and the pools the fleet shows.
func (m Model) openRepoPicker(repo, branch, url string) (Model, tea.Cmd) {
	pools, errMsg := m.prepareRepoPools(repo, url)
	if errMsg != "" {
		return m.noticeErr(errMsg)
	}
	defaultBranch := strings.TrimSpace(branch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	m.picker = &picker{
		repo:        repo,
		url:         url,
		kind:        dashboard.CheckoutRepo,
		pools:       pools,
		branchInput: m.newBranchInput(defaultBranch),
		focusBranch: true,
	}
	return m, nil
}

func stepCursor(cursor, delta, n int) int {
	if n <= 0 {
		return 0
	}
	return ((cursor+delta)%n + n) % n
}

func (m Model) updatePicker(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
	if p.hasBranchInput() && p.focusBranch {
		return m.updatePickerBranch(msg)
	}
	switch s := msg.String(); s {
	case "esc", "q", "ctrl+c":
		m.picker = nil
		return m, nil
	case "tab", "shift+tab":
		if p.hasBranchInput() {
			p.focusBranch = true
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
		if s == "shift+tab" {
			p.cursor = stepCursor(p.cursor, -1, len(p.pools))
		} else {
			p.cursor = stepCursor(p.cursor, 1, len(p.pools))
		}
	case "up", "k":
		p.cursor = stepCursor(p.cursor, -1, len(p.pools))
	case "down", "j":
		p.cursor = stepCursor(p.cursor, 1, len(p.pools))
	case "enter":
		return m.pickPool(p.cursor)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.pools) {
			return m.pickPool(int(s[0] - '1'))
		}
	}
	m.picker = &p
	return m, nil
}

func (m Model) updatePickerBranch(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
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
		p.cursor = stepCursor(p.cursor, -1, len(p.pools))
		m.picker = &p
		return m, nil
	case "down":
		p.cursor = stepCursor(p.cursor, 1, len(p.pools))
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
	if p.hasBranchInput() && p.focusBranch {
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
	if p.hasBranchInput() {
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
		dashboard.Checkout{Repo: p.repo, Number: p.number, Branch: branch, Kind: p.kind})
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
		if m.picker.hasBranchInput() && placed.OnField(mouse.X, y) {
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

const issuePickerHint = "tab switch · ↑/↓ agent · enter start · esc cancel"

func poolColumnWidths(pools []cluster.Pool) (nameW, tmplW int) {
	for _, pool := range pools {
		nameW = max(nameW, lipgloss.Width(pool.Name))
		tmplW = max(tmplW, lipgloss.Width(pool.Template))
	}
	return nameW, tmplW
}

func (p *picker) title() string {
	if p.kind == dashboard.CheckoutRepo {
		return fmt.Sprintf("Check out %s in a sandbox", p.repo)
	}
	return fmt.Sprintf("Check out %s#%d in a sandbox", p.repo, p.number)
}

func (p *picker) volumeLine() string {
	switch p.kind {
	case dashboard.CheckoutRepo:
		curBranch := strings.TrimSpace(p.branchInput.Value())
		return "volume " + cluster.RepoVolumeName(p.repo, curBranch)
	case dashboard.CheckoutIssue:
		return "volume " + cluster.IssueVolumeName(p.repo, p.number)
	default:
		branch := p.branch
		if branch == "" {
			branch = fmt.Sprintf("pr-%d", p.number)
		}
		return "branch " + branch + " · volume " + cluster.VolumeName(p.repo, p.number)
	}
}

func (m Model) pickerInputWidthFor(p *picker, nameW, tmplW int, title, vol string) int {
	digits := len(fmt.Sprintf("%d", len(p.pools)))
	optW := 1 + digits + 2 + nameW + 2 + tmplW + 1
	inner := max(44, lipgloss.Width(title), lipgloss.Width(vol), lipgloss.Width(issuePickerHint), optW)
	inner = min(inner, max(1, m.width-4))
	return max(1, inner-lipgloss.Width(p.branchInput.Prompt)-1)
}

func (m Model) pickerInputWidth(p *picker) int {
	nameW, tmplW := poolColumnWidths(p.pools)
	return m.pickerInputWidthFor(p, nameW, tmplW, p.title(), p.volumeLine())
}

func (m Model) pickerMenu() dialog.Menu {
	p := m.picker
	nameW, tmplW := poolColumnWidths(p.pools)
	opts := make([]dialog.Option, len(p.pools))
	for i, pool := range p.pools {
		opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s  %-*s ", i+1, nameW, pool.Name, tmplW, pool.Template)}
	}
	title := p.title()
	vol := p.volumeLine()
	if p.hasBranchInput() {
		ti := p.branchInput
		ti.SetWidth(m.pickerInputWidthFor(p, nameW, tmplW, title, vol))
		return dialog.Menu{
			Title:        title,
			Lines:        []string{vol},
			Field:        &dialog.Field{Label: "Branch", View: ti.View(), Focused: p.focusBranch, Err: p.branchErr},
			OptionsLabel: "Pool",
			Options:      opts,
			Cursor:       p.cursor,
			Hint:         issuePickerHint,
		}
	}
	return dialog.Menu{
		Title:   title,
		Lines:   []string{vol},
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
