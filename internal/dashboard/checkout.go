package dashboard

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/cluster"
	"github.com/tmih06/nightshift/internal/dialog"
	"github.com/tmih06/nightshift/internal/session"
)

// workspaceDir is the sandbox images' working directory (images/base), where
// a pull request's volume is mounted under the repository's name.
const workspaceDir = "/home/node/workspace"

const (
	cloneTimeout = 10 * time.Minute
	podTimeout   = 3 * time.Minute
)

// CheckoutKind distinguishes what kind of GitHub target a Checkout builds.
type CheckoutKind int

const (
	CheckoutPR CheckoutKind = iota
	CheckoutIssue
	CheckoutRepo
)

// Checkout is a pull request, issue, or repository branch to keep on its own
// volume and work on in a sandbox that mounts it.
type Checkout struct {
	Repo   string // owner/name
	Number int
	Branch string // head branch for a PR (empty -> pr-<n>), issue branch (empty -> issue-<n>), or repo branch (empty -> main)
	Kind   CheckoutKind
}

func (c Checkout) String() string {
	if c.Kind == CheckoutRepo {
		return fmt.Sprintf("%s@%s", c.Repo, c.branch())
	}
	return fmt.Sprintf("%s#%d", c.Repo, c.Number)
}

// Volume is the volume that keeps the pull request, issue, or repository branch.
func (c Checkout) Volume() string {
	switch c.Kind {
	case CheckoutIssue:
		return cluster.IssueVolumeName(c.Repo, c.Number)
	case CheckoutRepo:
		return cluster.RepoVolumeName(c.Repo, c.branch())
	default:
		return cluster.VolumeName(c.Repo, c.Number)
	}
}

func (c Checkout) volumeKind() cluster.VolumeKind {
	switch c.Kind {
	case CheckoutIssue:
		return cluster.VolumeIssue
	case CheckoutRepo:
		return cluster.VolumeRepo
	default:
		return cluster.VolumePR
	}
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
	default:
		return fmt.Sprintf("pr-%d", c.Number)
	}
}

// CloneScript runs at the volume's root in the clone Job. It drops what an
// unfinished clone left, clones over https into cluster.VolumeRepoDir, and
// checks out either the pull request's head ref (which works for forks too and
// needs no gh in the image) or the chosen branch for an issue or repository
// (tracking origin/<branch> if it already exists, or creating it from HEAD).
// It pins HTTP/1.1 with a low-speed timeout and retries transient stalls so a
// hung GitHub stream does not sit decaying for minutes. git's progress is
// carriage returns on one line, which the container log would hold back until
// the line ends, so it is turned into lines, unbuffered, for the dashboard to
// follow.
func (c Checkout) CloneScript() string {
	dir := cluster.VolumeRepoDir
	lines := []string{
		"set -euo pipefail",
		`lines() { if command -v stdbuf >/dev/null; then stdbuf -o0 tr '\r' '\n'; else tr '\r' '\n'; fi; }`,
		"export GIT_CONFIG_COUNT=3",
		"export GIT_CONFIG_KEY_0=http.version GIT_CONFIG_VALUE_0=HTTP/1.1",
		"export GIT_CONFIG_KEY_1=http.lowSpeedLimit GIT_CONFIG_VALUE_1=1024",
		"export GIT_CONFIG_KEY_2=http.lowSpeedTime GIT_CONFIG_VALUE_2=20",
	}
	lines = append(lines, retryScriptLines(
		"clone",
		"rm -rf "+dir,
		"git clone --progress "+shellQuote("https://github.com/"+c.Repo+".git")+" "+dir+" 2>&1 | lines",
	)...)
	lines = append(lines, "cd "+dir)
	if c.Kind == CheckoutIssue || c.Kind == CheckoutRepo {
		b := shellQuote(c.branch())
		ref := shellQuote("refs/remotes/origin/" + c.branch())
		lines = append(lines,
			"if git show-ref --verify --quiet "+ref+"; then git checkout -B "+b+" --track "+ref+"; else git checkout -B "+b+"; fi",
		)
	} else {
		lines = append(lines, retryScriptLines(
			"fetch",
			"",
			fmt.Sprintf("git fetch --progress origin pull/%d/head 2>&1 | lines", c.Number),
		)...)
		lines = append(lines, "git checkout -B "+shellQuote(c.branch())+" FETCH_HEAD")
	}
	return strings.Join(lines, "\n")
}

func retryScriptLines(label, setup, cmd string) []string {
	out := []string{"for i in 1 2 3; do"}
	if setup != "" {
		out = append(out, "  "+setup)
	}
	return append(out,
		"  if "+cmd+"; then break; fi",
		`  [ "$i" -lt 3 ] || exit 1`,
		fmt.Sprintf(`  echo "%s stalled, retrying ($((i+1))/3)..."`, label),
		"  sleep 2",
		"done",
	)
}

// ShellLine is what the sandbox's bash is given after a fresh clone: into the
// clone, and where it stands.
func (c Checkout) ShellLine() string {
	return c.shellLine(false)
}

// shellLine is what the sandbox's bash is given. When an issue or repository
// checkout reuses a kept volume, it also checks out (or creates) the requested
// branch without resetting any existing local branch.
func (c Checkout) shellLine(cloned bool) string {
	if (c.Kind == CheckoutIssue || c.Kind == CheckoutRepo) && cloned {
		b := shellQuote(c.branch())
		return "cd " + shellQuote(c.MountPath()) + " && (git checkout " + b + " 2>/dev/null || git checkout -b " + b + ") && git status -sb"
	}
	return "cd " + shellQuote(c.MountPath()) + " && git status -sb"
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

// shellQuote leaves plain words bare, so the typed line stays readable, and
// single-quotes the rest: branch names may hold $, ;, or quotes.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) && !strings.HasPrefix(s, "-") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WarmPools is the fleet's pools by name. A pull request's sandbox starts
// from the chosen pool's template.
func (m model) WarmPools() []cluster.Pool {
	pools := make([]cluster.Pool, 0, len(m.pools))
	for _, p := range m.pools {
		pools = append(pools, p)
	}
	sort.Slice(pools, func(i, j int) bool { return pools[i].Name < pools[j].Name })
	return pools
}

// WithFleet is m showing boxes and pools, as a fetch would leave it.
func (m model) WithFleet(boxes []cluster.Sandbox, pools map[string]cluster.Pool) Model {
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

var stepTitles = [stepCount]string{"Volume", "Clone", "Sandbox", "Shell"}

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

// checkoutOps is what the pipeline does to the cluster; tests swap it.
type checkoutOps struct {
	volumeState   func(string) (bool, bool, error)
	createVolume  func(string, cluster.VolumeFor) error
	templatePod   func(string) (map[string]any, string, error)
	clone         func(volume, image, script string, timeout time.Duration, progress func(string)) error
	createSandbox func(template string, pod map[string]any, volume, mount string) (string, error)
	waitPod       func(pod string, timeout time.Duration, progress func(string)) error
	ensure        func(pod string) error
	send          func(pod string, keys []session.Key) error
}

var liveOps = checkoutOps{
	volumeState:   cluster.VolumeState,
	createVolume:  cluster.CreateVolume,
	templatePod:   cluster.TemplatePod,
	clone:         cluster.CloneIntoVolume,
	createSandbox: cluster.CreateVolumeSandbox,
	waitPod:       cluster.WaitForPod,
	ensure:        session.Ensure,
	send:          func(pod string, keys []session.Key) error { return session.SendBatch(pod, "", keys) },
}

// runCheckout puts c on its volume, cloning it the first time, starts a
// sandbox from template on it, and brings its bash into the clone, telling
// emit each step as it goes. Its last event is done or an error.
func runCheckout(ops checkoutOps, c Checkout, template string, emit func(checkoutEvent)) {
	var last checkoutEvent
	say := func(ev checkoutEvent) {
		if ev != last {
			last = ev
			emit(ev)
		}
	}
	fail := func(s step, err error) { emit(checkoutEvent{step: s, err: err}) }

	vol := c.Volume()
	say(checkoutEvent{step: stepVolume, detail: "looking for " + vol})
	exists, cloned, err := ops.volumeState(vol)
	if err == nil && !exists {
		say(checkoutEvent{step: stepVolume, detail: "creating " + vol})
		err = ops.createVolume(vol, cluster.VolumeFor{Repo: c.Repo, Number: c.Number, Branch: c.branch(), Kind: c.volumeKind()})
	}
	if err != nil {
		fail(stepVolume, err)
		return
	}
	note := vol + " · new"
	if exists {
		note = vol + " · kept"
	}
	say(checkoutEvent{step: stepVolume, finished: true, detail: note})

	say(checkoutEvent{step: stepClone, detail: "reading " + template})
	pod, image, err := ops.templatePod(template)
	if err != nil {
		fail(stepClone, err)
		return
	}
	if cloned {
		say(checkoutEvent{step: stepClone, finished: true, detail: "already on the volume, kept as it was"})
	} else {
		say(checkoutEvent{step: stepClone, detail: "starting the clone pod"})
		err := ops.clone(vol, image, c.CloneScript(), cloneTimeout, func(line string) {
			say(checkoutEvent{step: stepClone, detail: cloneDetail(line)})
		})
		if err != nil {
			fail(stepClone, err)
			return
		}
		say(checkoutEvent{step: stepClone, finished: true, detail: c.branch() + " checked out"})
	}

	say(checkoutEvent{step: stepSandbox, detail: "creating from " + template})
	name, err := ops.createSandbox(template, pod, vol, c.MountPath())
	if err != nil {
		fail(stepSandbox, err)
		return
	}
	say(checkoutEvent{step: stepSandbox, pod: name, detail: "waiting for " + name})
	err = ops.waitPod(name, podTimeout, func(state string) {
		say(checkoutEvent{step: stepSandbox, pod: name, detail: podDetail(state)})
	})
	if err != nil {
		fail(stepSandbox, err)
		return
	}
	say(checkoutEvent{step: stepSandbox, pod: name, finished: true, detail: name})

	say(checkoutEvent{step: stepShell, pod: name, detail: "starting bash"})
	if err := ops.ensure(name); err != nil {
		fail(stepShell, err)
		return
	}
	if err := ops.send(name, []session.Key{{Text: c.shellLine(cloned)}, {Name: "Enter"}}); err != nil {
		fail(stepShell, fmt.Errorf("could not type into bash: %w", err))
		return
	}
	emit(checkoutEvent{step: stepShell, pod: name, done: true})
}

// cloneDetail is the clone Job's latest log line as the card says it.
func cloneDetail(line string) string {
	line = strings.TrimSpace(strings.TrimPrefix(line, "remote: "))
	if line == "" {
		return "starting the clone pod"
	}
	return line
}

func podDetail(state string) string {
	phase, reason, _ := strings.Cut(state, " ")
	switch {
	case phase == "":
		return "waiting for the pod"
	case reason == "ContainerCreating":
		return "pod " + phase + " · pulling the image"
	case reason != "":
		return "pod " + phase + " · " + reason
	}
	return "pod " + phase
}

var gitProgress = regexp.MustCompile(`^(.+?):\s+(\d+)% \(\d+/\d+\)(?:,\s*(.*?))?(?:,\s*done\.)?$`)

// parseProgress reads a git progress line: its phase, percent, and the rest
// (size and speed).
func parseProgress(line string) (phase string, percent int, rest string, ok bool) {
	g := gitProgress.FindStringSubmatch(line)
	if g == nil {
		return "", 0, "", false
	}
	pct, _ := strconv.Atoi(g[2])
	rest = strings.TrimSuffix(strings.TrimSpace(g[3]), ", done.")
	if rest == "done." {
		rest = ""
	}
	return g[1], min(pct, 100), rest, true
}

// startCheckoutCmd runs the checkout once the command runs, and answers
// with its first event; each event's message asks for the next.
func startCheckoutCmd(ops checkoutOps, c Checkout, template string) tea.Cmd {
	return func() tea.Msg {
		events := make(chan checkoutEvent, 16)
		go func() {
			defer close(events)
			runCheckout(ops, c, template, func(ev checkoutEvent) { events <- ev })
		}()
		return nextCheckoutEvent(events)()
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

// CheckOut puts c's pull request on its volume, cloning it there the first
// time, and starts a sandbox from pool's template on it, showing each step
// in a card. A sandbox already on that volume is where the work goes on
// instead. One checkout runs at a time.
func (m model) CheckOut(pool string, c Checkout) (Model, tea.Cmd) {
	m.confirmDestroy = false
	if r := m.checkout; r != nil {
		m.flash("still checking out " + r.co.String() + ", one at a time")
		return m, nil
	}
	vol := c.Volume()
	for _, b := range m.boxes {
		if b.Volume != vol {
			continue
		}
		m.section = sectionAll
		m.selectPod(b.Name)
		if b.Phase != "Running" {
			m.flash(c.String() + " is already starting in " + b.Name)
			return m, nil
		}
		m.flash(c.String() + " is already open in " + b.Name)
		return m, attachCmd(b.Name)
	}
	p, ok := m.pools[pool]
	if !ok || p.Template == "" {
		m.fail("no template for " + pool)
		return m, nil
	}
	now := time.Now()
	m.checkout = &checkoutRun{co: c, template: p.Template, began: now, since: now, detail: "starting"}
	m.status, m.statusTTL = "", 0
	return m, startCheckoutCmd(liveOps, c, p.Template)
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
		m.fail(strings.ToLower(stepTitles[ev.step]) + " failed for " + run.co.String() + ": " + ev.err.Error())
		return m, fetchCmd()
	case ev.done:
		m.checkout = nil
		co := run.co
		m.pendingPod, m.pendingCheckout = ev.pod, &co
		m.flash(co.String() + " is ready in " + ev.pod)
		return m, fetchCmd()
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
	return fmt.Sprintf("%s · %s %d/%d · %s", r.co.String(), strings.ToLower(stepTitles[r.step]),
		int(r.step)+1, stepCount, r.detail)
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const cardInner = 64

// checkoutCard draws the checkout in flight: each step done, running, or
// still to come, and a bar for git's progress.
func (m model) checkoutCard(now time.Time) string {
	r := m.checkout
	inner := min(cardInner, max(10, m.width-6))
	plain := lipgloss.NewStyle()
	faint := plain.Foreground(th.FaintText)
	lines := []string{
		dialog.Line(plain.Foreground(th.PrimaryText).Bold(true), "Checking out "+r.co.String(), inner),
		dialog.Line(faint, "on "+r.template+" · "+elapsed(now.Sub(r.began)), inner),
		"",
	}
	const label = 9
	for s := stepVolume; s < stepCount; s++ {
		name := fmt.Sprintf("%-*s", label, stepTitles[s])
		switch {
		case s < r.step:
			lines = append(lines, dialog.Line(plain.Foreground(th.SuccessText), "✓ "+name+r.notes[s], inner))
		case s == r.step:
			frame := spinner[int(now.UnixMilli()/100)%len(spinner)]
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
					faint.Render(ansiCut(tail, inner-label-2-20)))
			}
		default:
			lines = append(lines, dialog.Line(faint, "○ "+name, inner))
		}
	}
	return dialog.Box(th, lines)
}

func ansiCut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return truncateANSI(s, width)
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

// focusCheckout shows pod's shell, in the clone, in the All section if pod's
// section would hide it.
func (m model) focusCheckout(pod string, c Checkout) (Model, tea.Cmd) {
	if m.section == sectionWarm {
		m.section = sectionAll
	}
	m.focus = focusShell
	m.prefix = false
	m.flash(c.String() + " is in " + c.MountPath() + " — C-b q leaves bash")
	return m, tea.Batch(fetchCmd(), m.watchShell())
}
