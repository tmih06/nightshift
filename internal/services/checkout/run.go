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

const (
	cloneTimeout = 10 * time.Minute
	podTimeout   = 3 * time.Minute
)

// Ops is what the pipeline does to the cluster. Live is the real thing; tests
// swap it.
type Ops struct {
	PoolTemplate  func(pool string) (string, error)
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
	var last v1.CheckoutEvent
	say := func(ev v1.CheckoutEvent) {
		ev.At = time.Time{}
		if ev != last {
			last = ev
			ev.At = time.Now().UTC()
			emit(ev)
		}
	}
	fail := func(step string, err error) {
		emit(v1.CheckoutEvent{Step: step, Error: err.Error(), At: time.Now().UTC()})
	}

	vol := s.Volume()
	say(v1.CheckoutEvent{Step: v1.StepVolume, Detail: "looking for " + vol})
	exists, cloned, err := ops.VolumeState(vol)
	if err == nil && !exists {
		say(v1.CheckoutEvent{Step: v1.StepVolume, Detail: "creating " + vol})
		err = ops.CreateVolume(vol, cluster.VolumeFor{Repo: s.Target.Repo, Number: s.Target.Number, Branch: s.branch(), Kind: s.volumeKind()})
	}
	if err != nil {
		fail(v1.StepVolume, err)
		return
	}
	note := vol + " · new"
	if exists {
		note = vol + " · kept"
	}
	say(v1.CheckoutEvent{Step: v1.StepVolume, Finished: true, Detail: note})

	say(v1.CheckoutEvent{Step: v1.StepClone, Detail: "reading " + template})
	pod, image, err := ops.TemplatePod(template)
	if err != nil {
		fail(v1.StepClone, err)
		return
	}
	if cloned {
		say(v1.CheckoutEvent{Step: v1.StepClone, Finished: true, Detail: "already on the volume, kept as it was"})
	} else {
		say(v1.CheckoutEvent{Step: v1.StepClone, Detail: "starting the clone pod"})
		err := ops.Clone(vol, image, s.CloneScript(), cloneTimeout, func(line string) {
			say(v1.CheckoutEvent{Step: v1.StepClone, Detail: cloneDetail(line)})
		})
		if err != nil {
			fail(v1.StepClone, err)
			return
		}
		say(v1.CheckoutEvent{Step: v1.StepClone, Finished: true, Detail: s.branch() + " checked out"})
	}

	say(v1.CheckoutEvent{Step: v1.StepSandbox, Detail: "creating from " + template})
	name, err := ops.CreateSandbox(template, pod, vol, s.MountPath())
	if err != nil {
		fail(v1.StepSandbox, err)
		return
	}
	say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Detail: "waiting for " + name})
	err = ops.WaitPod(name, podTimeout, func(state string) {
		say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Detail: podDetail(state)})
	})
	if err != nil {
		fail(v1.StepSandbox, err)
		return
	}
	say(v1.CheckoutEvent{Step: v1.StepSandbox, Sandbox: name, Finished: true, Detail: name})

	shellDetail := "starting bash"
	if summary := s.harnessSummary(); summary != "" {
		shellDetail = "starting " + summary
	}
	say(v1.CheckoutEvent{Step: v1.StepShell, Sandbox: name, Detail: shellDetail})
	if err := ops.Ensure(name); err != nil {
		fail(v1.StepShell, err)
		return
	}
	if err := ops.Send(name, []session.Key{{Text: s.shellLine(cloned)}, {Name: "Enter"}}); err != nil {
		fail(v1.StepShell, fmt.Errorf("could not type into bash: %w", err))
		return
	}
	emit(v1.CheckoutEvent{Step: v1.StepShell, Sandbox: name, Done: true, At: time.Now().UTC()})
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

// shellQuote leaves plain words bare, so the typed line stays readable, and
// single-quotes the rest: branch names may hold $, ;, or quotes.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) && !strings.HasPrefix(s, "-") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
