package checkout

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
	"github.com/tmih06/nightshift/internal/services/session"
)

// How long a clone Job and a sandbox's pod are given.
const (
	CloneTimeout = 10 * time.Minute
	PodTimeout   = 3 * time.Minute
)

// Ops is what the pipeline does to the cluster. Live is the real thing; tests
// swap it.
type Ops struct {
	PoolTemplate  func(pool string) (string, error)
	SandboxOn     func(volume string) (string, error)
	VolumeState   func(volume string) (exists, cloned bool, err error)
	CreateVolume  func(volume string, target cluster.VolumeFor) error
	TemplatePod   func(template string) (pod map[string]any, image string, err error)
	Clone         func(volume, image, script string, timeout time.Duration, progress func(string)) error
	CreateSandbox func(template string, pod map[string]any, volume, mount string) (string, error)
	WaitPod       func(pod string, timeout time.Duration, progress func(string)) error
	Ensure        func(pod string) error
	Send          func(pod string, keys []session.Key) error
}

// Live is Ops against the cluster kubectl points at.
func Live() Ops {
	return Ops{
		PoolTemplate:  cluster.PoolTemplate,
		SandboxOn:     cluster.SandboxOnVolume,
		VolumeState:   cluster.VolumeState,
		CreateVolume:  cluster.CreateVolume,
		TemplatePod:   cluster.TemplatePod,
		Clone:         cluster.CloneIntoVolume,
		CreateSandbox: cluster.CreateVolumeSandbox,
		WaitPod:       cluster.WaitForPod,
		Ensure:        session.Ensure,
		Send:          func(pod string, keys []session.Key) error { return session.SendBatch(pod, "", keys) },
	}
}

// Run puts s on its volume, cloning it the first time, starts a sandbox from
// template on it, and brings its bash into the clone, telling emit each step as
// it goes. Its last event is Done or has Error set.
func Run(ops Ops, s Spec, template string, emit func(v1.CheckoutEvent)) {
	p := &pipeline{ops: ops, spec: s, template: template, emit: emit}
	cloned, ok := p.volume()
	if !ok {
		return
	}
	pod, ok := p.clone(cloned)
	if !ok {
		return
	}
	name, ok := p.sandbox(pod)
	if !ok {
		return
	}
	p.shell(name, cloned)
}

// pipeline is one run of the checkout: what it works on, and how it reports.
type pipeline struct {
	ops      Ops
	spec     Spec
	template string
	emit     func(v1.CheckoutEvent)
	last     v1.CheckoutEvent
}

// say reports progress, once for each distinct thing said.
func (p *pipeline) say(ev v1.CheckoutEvent) {
	ev.At = time.Time{}
	if ev == p.last {
		return
	}
	p.last = ev
	ev.At = time.Now().UTC()
	p.emit(ev)
}

// fail ends the run at step, with why.
func (p *pipeline) fail(step string, err error) {
	p.emit(v1.CheckoutEvent{Step: step, Error: err.Error(), At: time.Now().UTC()})
}

// volume finds or makes the volume, and says whether a clone is already on it.
func (p *pipeline) volume() (cloned, ok bool) {
	vol := p.spec.Volume()
	p.say(v1.CheckoutEvent{Step: v1.StepVolume, Detail: "looking for " + vol})
	exists, cloned, err := p.ops.VolumeState(vol)
	if err == nil && !exists {
		p.say(v1.CheckoutEvent{Step: v1.StepVolume, Detail: "creating " + vol})
		s := p.spec
		err = p.ops.CreateVolume(vol, cluster.VolumeFor{Repo: s.Target.Repo, Number: s.Target.Number, Branch: s.branch(), Kind: s.volumeKind()})
	}
	if err != nil {
		p.fail(v1.StepVolume, err)
		return false, false
	}
	note := vol + " · new"
	if exists {
		note = vol + " · kept"
	}
	p.say(v1.CheckoutEvent{Step: v1.StepVolume, Finished: true, Detail: note})
	return cloned, true
}

// clone reads the template and, unless a clone is already on the volume, clones
// into it. It returns the template's pod.
func (p *pipeline) clone(cloned bool) (pod map[string]any, ok bool) {
	p.say(v1.CheckoutEvent{Step: v1.StepClone, Detail: "reading " + p.template})
	pod, image, err := p.ops.TemplatePod(p.template)
	if err != nil {
		p.fail(v1.StepClone, err)
		return nil, false
	}
	if cloned {
		p.say(v1.CheckoutEvent{Step: v1.StepClone, Finished: true, Detail: "already on the volume, kept as it was"})
		return pod, true
	}
	p.say(v1.CheckoutEvent{Step: v1.StepClone, Detail: "starting the clone pod"})
	err = p.ops.Clone(p.spec.Volume(), image, p.spec.CloneScript(), CloneTimeout, func(line string) {
		p.say(v1.CheckoutEvent{Step: v1.StepClone, Detail: cloneDetail(line)})
	})
	if err != nil {
		p.fail(v1.StepClone, err)
		return nil, false
	}
	p.say(v1.CheckoutEvent{Step: v1.StepClone, Finished: true, Detail: p.spec.branch() + " checked out"})
	return pod, true
}

// sandbox starts the sandbox on the volume and waits for its pod to run.
func (p *pipeline) sandbox(pod map[string]any) (name string, ok bool) {
	p.say(v1.CheckoutEvent{Step: v1.StepSandbox, Detail: "creating from " + p.template})
	name, err := p.ops.CreateSandbox(p.template, pod, p.spec.Volume(), p.spec.MountPath())
	if err != nil {
		p.fail(v1.StepSandbox, err)
		return "", false
	}
	p.say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Detail: "waiting for " + name})
	err = p.ops.WaitPod(name, PodTimeout, func(state string) {
		p.say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Detail: podDetail(state)})
	})
	if err != nil {
		p.fail(v1.StepSandbox, err)
		return "", false
	}
	p.say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Finished: true, Detail: name})
	return name, true
}

// shell brings the sandbox's bash into the clone and starts the harness.
func (p *pipeline) shell(name string, cloned bool) {
	detail := "starting bash"
	if summary := p.spec.harnessSummary(); summary != "" {
		detail = "starting " + summary
	}
	p.say(v1.CheckoutEvent{Step: v1.StepShell, Sandbox: name, Detail: detail})
	if err := p.ops.Ensure(name); err != nil {
		p.fail(v1.StepShell, err)
		return
	}
	if err := p.ops.Send(name, []session.Key{{Text: p.spec.shellLine(cloned)}, {Name: "Enter"}}); err != nil {
		p.fail(v1.StepShell, fmt.Errorf("could not type into bash: %w", err))
		return
	}
	p.emit(v1.CheckoutEvent{Step: v1.StepShell, Sandbox: name, Done: true, At: time.Now().UTC()})
}

// cloneDetail is the clone Job's latest log line as a status says it.
func cloneDetail(line string) string {
	line = strings.TrimSpace(strings.TrimPrefix(line, "remote: "))
	if line == "" {
		return "starting the clone pod"
	}
	return line
}

// podDetail is a pod's phase and waiting reason as a status says it.
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

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

// Quote leaves plain words bare, so the typed line stays readable, and
// single-quotes the rest: branch names may hold $, ;, or quotes.
func Quote(s string) string {
	if shellSafe.MatchString(s) && !strings.HasPrefix(s, "-") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
