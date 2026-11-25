package app

import (
	"fmt"
	"strconv"
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
// also asks for the branch name to check out. Picking a pool forks a cascading
// model submenu to the right so the user can pick the harness model before
// starting the sandbox. The sandbox starts cold on its volume, as a running
// pod cannot take a new volume.
type picker struct {
	repo   string
	number int
	url    string
	branch string // PR head branch; issues and repos edit branchInput instead
	kind   dashboard.CheckoutKind
	pools  []cluster.Pool
	cursor int

	// modelOpen is true while the cascading right-side model submenu is open
	// for pools[cursor].
	modelOpen   bool
	harness     dashboard.Harness
	modelCursor int

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
	ti := dialog.TextInput(m.theme, "▸ ", defaultBranch, 200)
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
	if p.modelOpen {
		return m.updatePickerModel(msg)
	}
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
	case "right", "l", "enter":
		return m.openModelSubmenu(p.cursor)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < len(p.pools) {
			return m.openModelSubmenu(int(s[0] - '1'))
		}
	}
	m.picker = &p
	return m, nil
}

func (m Model) updatePickerModel(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	p := *m.picker
	n := len(p.harness.Models)
	switch s := msg.String(); s {
	case "q", "ctrl+c":
		m.picker = nil
		return m, nil
	case "esc", "left", "h":
		p.modelOpen = false
		m.picker = &p
		return m, nil
	case "up", "k", "shift+tab":
		p.modelCursor = stepCursor(p.modelCursor, -1, n)
	case "down", "j", "tab":
		p.modelCursor = stepCursor(p.modelCursor, 1, n)
	case "enter":
		return m.startCheckout(p.cursor, p.modelCursor)
	default:
		if len(s) == 1 && s[0] >= '1' && int(s[0]-'1') < n {
			return m.startCheckout(p.cursor, int(s[0]-'1'))
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
		return m.openModelSubmenu(p.cursor)
	}
	p.branchInput.SetWidth(m.pickerInputWidth(&p))
	p.branchInput, _ = p.branchInput.Update(msg)
	p.branchErr = ""
	m.picker = &p
	return m, nil
}

func (m Model) updatePickerPaste(msg tea.PasteMsg) (Model, tea.Cmd) {
	p := *m.picker
	if !p.modelOpen && p.hasBranchInput() && p.focusBranch {
		p.branchInput.SetWidth(m.pickerInputWidth(&p))
		p.branchInput, _ = p.branchInput.Update(msg)
		p.branchErr = ""
		m.picker = &p
	}
	return m, nil
}

// openModelSubmenu validates the branch (when applicable) and forks the
// right-side model submenu for pools[i].
func (m Model) openModelSubmenu(i int) (Model, tea.Cmd) {
	p := *m.picker
	if i < 0 || i >= len(p.pools) {
		return m, nil
	}
	if p.hasBranchInput() {
		branch := strings.TrimSpace(p.branchInput.Value())
		if reason := validateBranchName(branch); reason != "" {
			p.cursor = i
			p.branchErr = reason
			p.focusBranch = true
			p.modelOpen = false
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
	}
	p.cursor = i
	p.focusBranch = false
	p.branchInput.Blur()
	p.harness = dashboard.HarnessForPool(p.pools[i])
	p.modelOpen = true
	p.modelCursor = 0
	m.picker = &p
	return m, nil
}

// startCheckout starts the checkout from the chosen pool and harness model and
// moves to the sandbox tab, which shows its progress.
func (m Model) startCheckout(poolIdx, modelIdx int) (Model, tea.Cmd) {
	p := *m.picker
	if poolIdx < 0 || poolIdx >= len(p.pools) {
		return m, nil
	}
	branch := p.branch
	if p.hasBranchInput() {
		branch = strings.TrimSpace(p.branchInput.Value())
		if reason := validateBranchName(branch); reason != "" {
			p.cursor = poolIdx
			p.branchErr = reason
			p.focusBranch = true
			p.modelOpen = false
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
	}
	h := p.harness
	if len(h.Models) == 0 {
		h = dashboard.HarnessForPool(p.pools[poolIdx])
	}
	var modelID string
	if modelIdx >= 0 && modelIdx < len(h.Models) {
		modelID = h.Models[modelIdx].ID
	}
	m.picker = nil
	switched, switchCmd := m.goTo(TabSandboxes)
	m = switched
	var cmd tea.Cmd
	m.sandboxes, cmd = m.sandboxes.CheckOut(p.pools[poolIdx].Name, dashboard.Checkout{
		Repo:    p.repo,
		Number:  p.number,
		Branch:  branch,
		Kind:    p.kind,
		Harness: h.Command,
		Model:   modelID,
	})
	return m, tea.Batch(switchCmd, cmd)
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

// pickerMouse handles clicks and wheel events across the pool dialog and its
// right-side model submenu.
func (m Model) pickerMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	layout := m.pickerLayout()
	mouse := msg.Mouse()
	y := mouse.Y - barHeight
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		p := *m.picker
		delta := 0
		if msg.Button == tea.MouseWheelUp {
			delta = -1
		} else if msg.Button == tea.MouseWheelDown {
			delta = 1
		}
		if delta == 0 {
			return m, nil
		}
		if layout.hasSub && (layout.sub.Inside(mouse.X, y) || !layout.parent.Inside(mouse.X, y)) {
			p.modelCursor = clampIndex(p.modelCursor+delta, len(p.harness.Models))
		} else {
			p.cursor = clampIndex(p.cursor+delta, len(p.pools))
			if p.modelOpen {
				p.harness = dashboard.HarnessForPool(p.pools[p.cursor])
				p.modelCursor = 0
			}
		}
		m.picker = &p
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if layout.hasSub && layout.sub.Inside(mouse.X, y) {
			if i, ok := layout.sub.OptionAt(mouse.X, y); ok {
				return m.startCheckout(m.picker.cursor, i)
			}
			return m, nil
		}
		if !layout.parent.Inside(mouse.X, y) {
			m.picker = nil
			return m, nil
		}
		if m.picker.hasBranchInput() && layout.parent.OnField(mouse.X, y) {
			p := *m.picker
			p.modelOpen = false
			p.focusBranch = true
			p.branchInput.Focus()
			m.picker = &p
			return m, nil
		}
		if i, ok := layout.parent.OptionAt(mouse.X, y); ok {
			return m.openModelSubmenu(i)
		}
		if m.picker.modelOpen {
			p := *m.picker
			p.modelOpen = false
			m.picker = &p
		}
	}
	return m, nil
}

func clampIndex(i, n int) int {
	if n <= 0 {
		return 0
	}
	return max(0, min(n-1, i))
}

const (
	poolPickerHint  = "↑/↓/jk pool · →/l/⏎ models · esc cancel"
	issuePickerHint = "tab switch · ↑/↓ pool · →/l/⏎ models · esc cancel"
	modelPickerHint = "↑/↓/jk model · ←/h back · ⏎ start"
)

func poolColumnWidths(pools []cluster.Pool) (nameW, tmplW int) {
	for _, pool := range pools {
		nameW = max(nameW, lipgloss.Width(pool.Name))
		tmplW = max(tmplW, lipgloss.Width(pool.Template))
	}
	return nameW, tmplW
}

func modelColumnWidths(models []dashboard.ModelOption) (labelW, noteW int) {
	for _, opt := range models {
		labelW = max(labelW, lipgloss.Width(opt.Label))
		noteW = max(noteW, lipgloss.Width(opt.Note))
	}
	return labelW, noteW
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
	digits := len(strconv.Itoa(len(p.pools)))
	optW := 1 + digits + 2 + nameW + 2 + tmplW + 2 + 1 + 1 // trailing "  › "
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
		opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s  %-*s  › ", i+1, nameW, pool.Name, tmplW, pool.Template)}
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
			Dimmed:       p.modelOpen,
		}
	}
	return dialog.Menu{
		Title:   title,
		Lines:   []string{vol},
		Options: opts,
		Cursor:  p.cursor,
		Hint:    poolPickerHint,
		Dimmed:  p.modelOpen,
	}
}

func (m Model) modelSubmenu() dialog.Menu {
	p := m.picker
	h := p.harness
	labelW, noteW := modelColumnWidths(h.Models)
	opts := make([]dialog.Option, len(h.Models))
	for i, opt := range h.Models {
		if noteW > 0 {
			opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s  %-*s ", i+1, labelW, opt.Label, noteW, opt.Note)}
		} else {
			opts[i] = dialog.Option{Text: fmt.Sprintf(" %d  %-*s ", i+1, labelW, opt.Label)}
		}
	}
	sub := p.pools[p.cursor].Name
	if h.Command != "" {
		sub += " · " + h.Command
	}
	return dialog.Menu{
		Title:        h.Label,
		Lines:        []string{sub},
		OptionsLabel: "Model",
		Options:      opts,
		Cursor:       p.modelCursor,
		Hint:         modelPickerHint,
	}
}

type pickerBounds struct {
	parent dialog.Placed
	sub    dialog.Placed
	hasSub bool
	bridge string
	bx, by int
}

func (m Model) pickerLayout() pickerBounds {
	bodyW := m.width
	bodyH := max(0, m.height-barHeight)
	parentRaw := m.pickerMenu().Render(m.theme, bodyW)
	parentX := max(0, (bodyW-parentRaw.W)/2)
	parentY := max(0, (bodyH-parentRaw.H)/2)

	if !m.picker.modelOpen {
		return pickerBounds{parent: parentRaw.At(parentX, parentY)}
	}

	subRaw := m.modelSubmenu().Render(m.theme, bodyW)
	const gap = 1
	var subX int
	switch {
	case parentRaw.W+gap+subRaw.W <= bodyW:
		if parentX+parentRaw.W+gap+subRaw.W > bodyW {
			parentX = max(0, bodyW-parentRaw.W-gap-subRaw.W)
		}
		subX = parentX + parentRaw.W + gap
	case parentRaw.W+subRaw.W <= bodyW:
		parentX = max(0, (bodyW-parentRaw.W-subRaw.W)/2)
		subX = parentX + parentRaw.W
	default:
		// Narrow terminal: cascade the submenu over the right side of the parent
		// while keeping both inside the viewport.
		parentX = 0
		subX = max(0, bodyW-subRaw.W)
	}

	parent := parentRaw.At(parentX, parentY)
	anchorY := parent.OptionY(m.picker.cursor)
	subY := anchorY - 1
	subY = min(subY, max(0, bodyH-subRaw.H))
	subY = max(0, subY)
	sub := subRaw.At(subX, subY)

	res := pickerBounds{
		parent: parent,
		sub:    sub,
		hasSub: true,
	}
	// Draw a crisp fork connector between the selected pool row and the submenu
	// when they sit side-by-side with a 1-column gap.
	if sub.X == parent.X+parent.W+gap && anchorY > parent.Y && anchorY < parent.Y+parent.H-1 {
		leftTee := lipgloss.NewStyle().Foreground(m.theme.PrimaryBorder).Render("├")
		dash := lipgloss.NewStyle().Foreground(m.theme.SecondaryBorder).Render("─")
		if anchorY > sub.Y && anchorY < sub.Y+sub.H-1 {
			rightTee := lipgloss.NewStyle().Foreground(m.theme.SecondaryBorder).Render("┤")
			res.bridge = leftTee + dash + rightTee
		} else if anchorY == sub.Y || anchorY == sub.Y+sub.H-1 {
			res.bridge = leftTee + dash
		}
		if res.bridge != "" {
			res.bx = parent.X + parent.W - 1
			res.by = anchorY
		}
	}
	return res
}

func (m Model) pickerPlaced() dialog.Placed {
	return m.pickerLayout().parent
}

func (m Model) pickerSubPlaced() (dialog.Placed, bool) {
	l := m.pickerLayout()
	return l.sub, l.hasSub
}

// overPicker draws the picker (and its model submenu when open) over the body.
func (m Model) overPicker(body string) string {
	l := m.pickerLayout()
	bodyH := max(0, m.height-barHeight)
	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(l.parent.Box).X(l.parent.X).Y(l.parent.Y).Z(1),
	}
	if l.hasSub {
		layers = append(layers, lipgloss.NewLayer(l.sub.Box).X(l.sub.X).Y(l.sub.Y).Z(2))
		if l.bridge != "" {
			layers = append(layers, lipgloss.NewLayer(l.bridge).X(l.bx).Y(l.by).Z(3))
		}
	}
	return dialog.OverlayLayers(body, bodyH, layers...)
}
