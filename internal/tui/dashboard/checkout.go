package dashboard

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/tui/dialog"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

// workspaceDir is the sandbox images' working directory, where a volume is
// mounted under the repository's name. It is only shown: the services decide
// where it goes.
const workspaceDir = "/home/node/workspace"

// CheckoutKind distinguishes what kind of GitHub target a Checkout builds.
type CheckoutKind int

// The kinds of target.
const (
	CheckoutPR CheckoutKind = iota
	CheckoutIssue
	CheckoutRepo
)

// Checkout is a pull request, issue, or repository branch to keep on its own
// volume and work on in a sandbox that mounts it.
type Checkout struct {
	Repo    string // owner/name
	Number  int
	Branch  string // head branch for a PR (empty -> pr-<n>), issue branch (empty -> issue-<n>), or repo branch (empty -> main)
	Kind    CheckoutKind
	Harness string // coding agent CLI command to launch (e.g. "claude", "codex", "pi", "opencode"); empty leaves bash open
	Model   string // model ID passed to --model; empty uses the harness default
}

func (c Checkout) String() string {
	if c.Kind == CheckoutRepo {
		return fmt.Sprintf("%s@%s", c.Repo, c.branch())
	}
	return fmt.Sprintf("%s#%d", c.Repo, c.Number)
}

// Request is what the services are asked to do for c, from pool.
func (c Checkout) Request(pool string) v1.CheckoutRequest {
	t := v1.Target{Kind: v1.TargetPullRequest, Repo: c.Repo, Number: c.Number, Branch: c.Branch}
	switch c.Kind {
	case CheckoutIssue:
		t.Kind = v1.TargetIssue
	case CheckoutRepo:
		t.Kind, t.Number = v1.TargetBranch, 0
	case CheckoutPR:
	}
	return v1.CheckoutRequest{Target: t, Pool: pool, Harness: c.Harness, Model: c.Model}
}

// onSandbox is whether b is a sandbox that keeps c.
func (c Checkout) onSandbox(b *fleet.Sandbox) bool {
	t := b.Target
	if t == nil || t.Repo != c.Repo {
		return false
	}
	req := c.Request("").Target
	if t.Kind != req.Kind {
		return false
	}
	if t.Kind == v1.TargetBranch {
		return t.Branch == c.branch()
	}
	return t.Number == c.Number
}

// MountPath is where the volume sits in the sandbox.
func (c Checkout) MountPath() string {
	return workspaceDir + "/" + c.Repo[strings.LastIndex(c.Repo, "/")+1:]
}

func (c Checkout) branch() string {
	if c.Branch != "" {
		return c.Branch
	}
	switch c.Kind {
	case CheckoutIssue:
		return fmt.Sprintf("issue-%d", c.Number)
	case CheckoutRepo:
		return "main"
	case CheckoutPR:
	}
	return fmt.Sprintf("pr-%d", c.Number)
}

func (c Checkout) harnessSummary() string {
	h := strings.TrimSpace(c.Harness)
	if h == "" {
		return ""
	}
	if m := strings.TrimSpace(c.Model); m != "" {
		return h + " (" + m + ")"
	}
	return h
}

// WarmPools is the fleet's pools by name. A pull request's sandbox starts
// from the chosen pool's template.
func (m model) WarmPools() []fleet.Pool {
	pools := make([]fleet.Pool, 0, len(m.pools))
	for name := range m.pools {
		pools = append(pools, m.pools[name])
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools
}

// WithFleet is m showing boxes and pools, as a fetch would leave it.
func (m model) WithFleet(boxes []fleet.Sandbox, pools map[string]fleet.Pool) Model {
	m.boxes, m.pools, m.loaded = boxes, pools, true
	m.dataRev++
	return m
}

// ---- the checkout's steps ------------------------------------------------

type step int

const (
	stepVolume step = iota
	stepClone
	stepSandbox
	stepShell
	stepCount
)

// title is the step's name on the card.
func (s step) title() string {
	switch s {
	case stepClone:
		return "Clone"
	case stepSandbox:
		return "Sandbox"
	case stepShell:
		return "Shell"
	case stepVolume, stepCount:
	}
	return "Volume"
}

// stepOf is the card's step for the contract's step name.
func stepOf(name string) step {
	switch name {
	case v1.StepClone:
		return stepClone
	case v1.StepSandbox:
		return stepSandbox
	case v1.StepShell:
		return stepShell
	}
	return stepVolume
}

// checkoutEvent is the pipeline telling the dashboard where it is: the step
// it works on and what it does there, a step's summary once it is finished,
// the end, or why it stopped.
type checkoutEvent struct {
	step     step
	detail   string
	finished bool
	pod      string
	done     bool
	err      error
}

// eventOf is an event of the services' pipeline as the dashboard shows it.
func eventOf(ev v1.CheckoutEvent) checkoutEvent {
	out := checkoutEvent{step: stepOf(ev.Step), detail: ev.Detail, finished: ev.Finished, pod: ev.Sandbox, done: ev.Done}
	if ev.Error != "" {
		out.err = errText(ev.Error)
	}
	return out
}

type errText string

func (e errText) Error() string { return string(e) }

type checkoutMsg struct {
	ev     checkoutEvent
	events <-chan checkoutEvent
}

// checkoutRun is the checkout in flight, as the card shows it.
type checkoutRun struct {
	co       Checkout
	template string
	step     step
	detail   string
	notes    [stepCount]string
	began    time.Time
	since    time.Time // the current step's start
}

var gitProgress = regexp.MustCompile(`^(.+?):\s+(\d+)% \(\d+/\d+\)(?:,\s*(.*?))?(?:,\s*done\.)?$`)

// parseProgress reads a git progress line: its phase, percent, and the rest
// (size and speed).
func parseProgress(line string) (phase string, percent int, rest string, ok bool) {
	g := gitProgress.FindStringSubmatch(line)
	if g == nil {
		return "", 0, "", false
	}
	pct, err := strconv.Atoi(g[2])
	if err != nil {
		return "", 0, "", false
	}
	rest = strings.TrimSuffix(strings.TrimSpace(g[3]), ", done.")
	if rest == "done." {
		rest = ""
	}
	return g[1], min(pct, 100), rest, true
}

// startCheckoutCmd asks the services for the checkout and follows it. It
// answers with the first event; each event's message asks for the next.
func startCheckoutCmd(api API, c Checkout, pool string) tea.Cmd {
	return func() tea.Msg {
		events := make(chan checkoutEvent, 16)
		go func() {
			defer close(events)
			follow(api, c, pool, func(ev checkoutEvent) { events <- ev })
		}()
		return nextCheckoutEvent(events)()
	}
}

// follow starts the checkout and tells emit about it until it ends. Its last
// event is done or has err set.
func follow(api API, c Checkout, pool string, emit func(checkoutEvent)) {
	ctx := context.Background()
	co, err := call(callTimeout, func(ctx context.Context) (v1.Checkout, error) { return api.Checkout(ctx, c.Request(pool)) })
	if err != nil {
		emit(checkoutEvent{step: stepVolume, err: err})
		return
	}
	last := checkoutEvent{step: stepVolume}
	err = api.FollowCheckout(ctx, co.ID, func(ev v1.CheckoutEvent) error {
		last = eventOf(ev)
		emit(last)
		return nil
	})
	if err != nil && !last.done && last.err == nil {
		emit(checkoutEvent{step: last.step, err: err})
	}
}

func nextCheckoutEvent(events <-chan checkoutEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return checkoutMsg{ev: ev, events: events}
	}
}

// CheckOut puts c on its volume, cloning it there the first time, and starts a
// sandbox from pool's template on it, showing each step in a card. A sandbox
// already on that volume is where the work goes on instead. One checkout runs
// at a time.
func (m model) CheckOut(pool string, c Checkout) (Model, tea.Cmd) {
	m.confirmDestroy = false
	if r := m.checkout; r != nil {
		m.flash("still checking out " + r.co.String() + ", one at a time")
		return m, nil
	}
	for i := range m.boxes {
		b := &m.boxes[i]
		if !c.onSandbox(b) {
			continue
		}
		m.section = sectionAll
		m.selectPod(b.Name)
		if b.Phase != fleet.PodPhaseRunning {
			m.flash(c.String() + " is already starting in " + b.Name)
			return m, nil
		}
		m.flash(c.String() + " is already open in " + b.Name)
		return m, attachCmd(m.api, b.Name)
	}
	p, ok := m.pools[pool]
	if !ok || p.Template == "" {
		m.fail("no template for " + pool)
		return m, nil
	}
	now := time.Now()
	m.checkout = &checkoutRun{co: c, template: p.Template, began: now, since: now, detail: startingLabel}
	m.status, m.statusTTL = "", 0
	return m, startCheckoutCmd(m.api, c, pool)
}

func (m model) checkoutEvent(msg checkoutMsg) (model, tea.Cmd) {
	if m.checkout == nil {
		return m, nil
	}
	run := *m.checkout
	ev := msg.ev
	switch {
	case ev.err != nil:
		m.checkout = nil
		m.fail(strings.ToLower(ev.step.title()) + " failed for " + run.co.String() + ": " + ev.err.Error())
		return m, fetchCmd(m.api)
	case ev.done:
		m.checkout = nil
		co := run.co
		m.pendingPod, m.pendingCheckout = ev.pod, &co
		m.flash(co.String() + " is ready in " + ev.pod)
		return m, fetchCmd(m.api)
	case ev.finished:
		run.notes[ev.step] = ev.detail
	default:
		if ev.step != run.step {
			run.since = time.Now()
		}
		run.step, run.detail = ev.step, ev.detail
	}
	m.checkout = &run
	return m, nextCheckoutEvent(msg.events)
}

// CheckoutLine is the checkout in flight in one line, for a host to show
// while the sandbox tab is out of sight; "" when none runs.
func (m model) CheckoutLine() string {
	r := m.checkout
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%s · %s %d/%d · %s", r.co.String(), strings.ToLower(r.step.title()),
		int(r.step)+1, stepCount, r.detail)
}

// spinnerFrame is the spinner's frame at now.
func spinnerFrame(now time.Time) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[int(now.UnixMilli()/100)%len(frames)]
}

const cardInner = 64

// checkoutCard draws the checkout in flight: each step done, running, or
// still to come, and a bar for git's progress.
func (m model) checkoutCard(now time.Time) string {
	r := m.checkout
	inner := min(cardInner, max(10, m.width-6))
	plain := lipgloss.NewStyle()
	faint := plain.Foreground(th.FaintText)
	sub := "on " + r.template
	if summary := r.co.harnessSummary(); summary != "" {
		sub += " · " + summary
	}
	sub += " · " + elapsed(now.Sub(r.began))
	lines := []string{
		dialog.Line(plain.Foreground(th.PrimaryText).Bold(true), "Checking out "+r.co.String(), inner),
		dialog.Line(faint, sub, inner),
		"",
	}
	const label = 9
	for s := stepVolume; s < stepCount; s++ {
		name := fmt.Sprintf("%-*s", label, s.title())
		switch {
		case s < r.step:
			lines = append(lines, dialog.Line(plain.Foreground(th.SuccessText), "✓ "+name+r.notes[s], inner))
		case s == r.step:
			frame := spinnerFrame(now)
			text := r.detail
			phase, pct, rest, isProgress := parseProgress(text)
			if isProgress {
				text = phase
			}
			lines = append(lines, dialog.Line(plain.Foreground(th.WarningText), frame+" "+name+text+" · "+elapsed(now.Sub(r.since)), inner))
			if isProgress {
				bar := progressBar(pct, 20)
				tail := fmt.Sprintf(" %3d%%", pct)
				if rest != "" {
					tail += " · " + rest
				}
				lines = append(lines, strings.Repeat(" ", label+2)+warmStyle.Render(bar)+
					faint.Render(truncateANSI(tail, inner-label-2-20)))
			}
		default:
			lines = append(lines, dialog.Line(faint, "○ "+name, inner))
		}
	}
	return dialog.Box(th, lines)
}

func progressBar(pct, width int) string {
	full := pct * width / 100
	return strings.Repeat("█", full) + strings.Repeat("░", width-full)
}

func elapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// focusCheckout shows the selected sandbox's shell, in the clone, in the All
// section if its section would hide it.
func (m model) focusCheckout(c Checkout) (Model, tea.Cmd) {
	if m.section == sectionWarm || m.section == sectionInfra {
		m.section = sectionAll
	}
	m.sidebarMode = paneShell
	m.focus = focusShell
	m.prefix = false
	if summary := c.harnessSummary(); summary != "" {
		m.flash(c.String() + " is in " + c.MountPath() + " · " + summary + " — C-b q leaves shell")
	} else {
		m.flash(c.String() + " is in " + c.MountPath() + " — C-b q leaves bash")
	}
	cmd := m.watchShell()
	return m, tea.Batch(fetchCmd(m.api), cmd)
}
