package cluster

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StartupStage is how far a pod is on its way to readiness.
type StartupStage int

const (
	StageScheduling StartupStage = iota
	StagePulling
	StageStarting
	StageReadying
)

// StartupStageCount is the number of startup stages, for segmented displays.
const StartupStageCount = 4

// Startup is a pod's progress toward readiness, inferred from its status and
// events. Kubernetes exposes no byte-level pull progress, so the stages are
// the honest granularity: scheduled, image pulled, container started, ready.
type Startup struct {
	Stage     StartupStage
	Since     time.Time // when the current stage began, best known
	Err       string    // waiting reason when the stage is failing (e.g. ImagePullBackOff)
	Detail    string    // the latest failing pull event's message
	PullFails int       // failed pull attempts so far
}

// Failing is whether the startup is retrying after failures.
func (s *Startup) Failing() bool { return s != nil && s.Err != "" }

type podEvent struct {
	reason  string
	message string
	count   int
	last    time.Time
}

type eventItem struct {
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Count          int    `json:"count"`
	LastTimestamp  string `json:"lastTimestamp"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"involvedObject"`
}

type eventList struct {
	Items []eventItem `json:"items"`
}

// parseEvents indexes pod events by namespace/name. Events on anything but
// pods (leader elections, sandbox CRs) are dropped.
func parseEvents(raw []byte) (map[string][]podEvent, error) {
	var list eventList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse events list: %w", err)
	}
	out := make(map[string][]podEvent, len(list.Items))
	for _, item := range list.Items {
		if item.InvolvedObject.Kind != "Pod" {
			continue
		}
		key := podKey(item.InvolvedObject.Namespace, item.InvolvedObject.Name)
		out[key] = append(out[key], podEvent{
			reason:  item.Reason,
			message: item.Message,
			count:   item.Count,
			last:    parseTime(item.LastTimestamp),
		})
	}
	return out, nil
}

// pullMilestones is what a pod's events say about its image pull and start.
// hasPulled marks the event's presence: a zero timestamp still counts.
type pullMilestones struct {
	pulling, pulled, started time.Time
	hasPulled                bool
	fails                    int
	detail                   string
	detailAt                 time.Time
}

func latestEvent(ev, cur time.Time) time.Time {
	if ev.After(cur) {
		return ev
	}
	return cur
}

func milestones(events []podEvent) pullMilestones {
	var ms pullMilestones
	for _, ev := range events {
		switch ev.reason {
		case "Pulling":
			ms.pulling = latestEvent(ev.last, ms.pulling)
		case "Pulled":
			ms.hasPulled = true
			ms.pulled = latestEvent(ev.last, ms.pulled)
		case "Started":
			ms.started = latestEvent(ev.last, ms.started)
		case "Failed":
			if !strings.Contains(ev.message, "pull image") {
				continue
			}
			ms.fails += ev.count
			if ev.last.After(ms.detailAt) {
				ms.detail, ms.detailAt = ev.message, ev.last
			}
		}
	}
	return ms
}

// isPullWait is whether a container waiting reason means the image pull is
// failing or backing off.
func isPullWait(reason string) bool {
	switch reason {
	case "ErrImagePull", "ImagePullBackOff", "InvalidImageName", "ImageInspectError":
		return true
	}
	return false
}

// waitingIsProgress is whether a waiting reason is part of a normal startup
// rather than a failure.
func waitingIsProgress(reason string) bool {
	return reason == "" || reason == "ContainerCreating" || reason == "PodInitializing"
}

func allContainersReady(p *podItem) bool {
	if len(p.Status.ContainerStatuses) == 0 {
		return false
	}
	for _, cs := range p.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}

func anyContainerRunning(p *podItem) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Running != nil {
			return true
		}
	}
	return false
}

func firstWaitingReason(p *podItem) string {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
	}
	return ""
}

// startupDone is whether the pod needs no startup display: terminal phases,
// or running with every container ready.
func startupDone(p *podItem) bool {
	if p.Status.Phase == PodPhaseRunning {
		return allContainersReady(p)
	}
	return p.Status.Phase != "Pending" && p.Status.Phase != ""
}

func orTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

// inferStage locates the pod on the startup pipeline. Events decide the
// pulling/starting boundary; without them a creating container counts as
// pulling, the dominant stage for fresh images.
func inferStage(p *podItem, ms pullMilestones) (StartupStage, time.Time) {
	created := parseTime(p.Metadata.Created)
	waiting := firstWaitingReason(p)
	switch {
	case p.Spec.NodeName == "":
		return StageScheduling, created
	case isPullWait(waiting):
		return StagePulling, orTime(ms.pulling, created)
	case anyContainerRunning(p), waiting == "CrashLoopBackOff":
		return StageReadying, orTime(ms.started, created)
	case waiting == "CreateContainerConfigError", waiting == "CreateContainerError":
		return StageStarting, orTime(ms.pulled, created)
	case ms.hasPulled:
		return StageStarting, orTime(ms.pulled, created)
	default:
		return StagePulling, orTime(ms.pulling, created)
	}
}

// computeStartup derives a pod's startup display state from its spec, status,
// and events. It returns nil for pods that need none: terminal, ready, or
// being deleted.
func computeStartup(p *parsedPod, events []podEvent) *Startup {
	if p.item.Metadata.Deleted != "" || startupDone(&p.item) {
		return nil
	}
	ms := milestones(events)
	stage, since := inferStage(&p.item, ms)
	st := &Startup{Stage: stage, Since: since, PullFails: ms.fails, Detail: ms.detail}
	if waiting := firstWaitingReason(&p.item); !waitingIsProgress(waiting) {
		st.Err = waiting
	}
	return st
}

// fetchStartupEvents reads the cluster's pod events, degrading to nil when
// either the fetch or the parse fails: pod status alone still yields stages.
func fetchStartupEvents() map[string][]podEvent {
	raw, err := runKubectl(nil, "get", "events", "-A", "-o", "json")
	if err != nil {
		return nil
	}
	events, err := parseEvents(raw)
	if err != nil {
		return nil
	}
	return events
}

// anyPodStarting is whether any box is still on its way to readiness, so the
// events fetch — useless for a healthy fleet — only runs when it matters.
func anyPodStarting(boxes []Sandbox) bool {
	for i := range boxes {
		b := &boxes[i]
		if b.Phase == "Pending" || b.Phase == "" || b.Phase == "…" || b.Waiting != "" {
			return true
		}
	}
	return false
}

// attachStartup computes each box's startup state from the pods list and
// events, which may be nil when the fetch failed or nothing is starting.
func attachStartup(boxes []Sandbox, pl podList, events map[string][]podEvent) error {
	byKey, _, err := parsePodIndex(pl)
	if err != nil {
		return err
	}
	for i := range boxes {
		key := podKey(boxes[i].EffectiveNamespace(), boxes[i].Name)
		pp := byKey[key]
		if pp == nil {
			// A sandbox CR whose pod does not exist yet is still scheduling.
			if boxes[i].Phase == "…" {
				boxes[i].Startup = &Startup{Stage: StageScheduling, Since: boxes[i].Created}
			}
			continue
		}
		boxes[i].Startup = computeStartup(pp, events[key])
	}
	return nil
}
