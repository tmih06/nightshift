package cluster

import (
	"strings"
	"testing"
)

func mustPod(t *testing.T, raw string) *parsedPod {
	t.Helper()
	var pl podList
	mustParse(t, `{"items":[`+raw+`]}`, &pl)
	byKey, ordered, err := parsePodIndex(pl)
	if err != nil {
		t.Fatalf("parsePodIndex: %v", err)
	}
	if len(ordered) != 1 {
		t.Fatalf("got %d pods, want 1", len(ordered))
	}
	for _, pp := range byKey {
		return pp
	}
	return ordered[0]
}

func podJSON(status string) string {
	return `{"metadata":{"name":"p1","namespace":"nightshift","creationTimestamp":"2026-11-25T10:00:00Z"},` +
		`"spec":{"nodeName":"n1","containers":[{"name":"agent","image":"img:latest"}]},` +
		`"status":{` + status + `}}`
}

func startupFor(t *testing.T, pod string, events []podEvent) *Startup {
	t.Helper()
	return computeStartup(mustPod(t, pod), events)
}

func TestComputeStartupStages(t *testing.T) {
	pulling := `"phase":"Pending","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"ContainerCreating"}}}]`
	cases := []struct {
		name      string
		pod       string
		events    []podEvent
		wantNil   bool
		wantStage StartupStage
	}{
		{
			name:      "unscheduled pod is scheduling",
			pod:       strings.Replace(podJSON(`"phase":"Pending"`), `"nodeName":"n1",`, "", 1),
			wantStage: StageScheduling,
		},
		{
			name:      "creating container without events is pulling",
			pod:       podJSON(pulling),
			wantStage: StagePulling,
		},
		{
			name:      "pulled event moves the pod to starting",
			pod:       podJSON(pulling),
			events:    []podEvent{{reason: "Pulled"}},
			wantStage: StageStarting,
		},
		{
			name:      "running unready container is readying",
			pod:       podJSON(`"phase":"Running","containerStatuses":[{"ready":false,"state":{"running":{"startedAt":"2026-11-25T10:01:00Z"}}}]`),
			wantStage: StageReadying,
		},
		{
			name:    "ready pod needs no startup",
			pod:     podJSON(`"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{"startedAt":"2026-11-25T10:01:00Z"}}}]`),
			wantNil: true,
		},
		{
			name:    "succeeded job needs no startup",
			pod:     podJSON(`"phase":"Succeeded","containerStatuses":[{"ready":false}]`),
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := startupFor(t, tc.pod, tc.events)
			if tc.wantNil {
				if st != nil {
					t.Errorf("got %+v, want nil", st)
				}
				return
			}
			if st == nil {
				t.Fatalf("got nil, want stage %d", tc.wantStage)
			}
			if st.Stage != tc.wantStage {
				t.Errorf("stage = %d, want %d", st.Stage, tc.wantStage)
			}
		})
	}
}

func TestComputeStartupDeletedPodNeedsNone(t *testing.T) {
	raw := strings.Replace(podJSON(`"phase":"Pending"`),
		`"creationTimestamp":"2026-11-25T10:00:00Z"`,
		`"creationTimestamp":"2026-11-25T10:00:00Z","deletionTimestamp":"2026-11-25T10:05:00Z"`, 1)
	if st := startupFor(t, raw, nil); st != nil {
		t.Errorf("terminating pod got %+v, want nil", st)
	}
}

func TestComputeStartupFailures(t *testing.T) {
	backoff := podJSON(`"phase":"Pending","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"ImagePullBackOff"}}}]`)
	events := []podEvent{
		{reason: "Pulling", count: 1, last: parseTime("2026-11-25T10:00:05Z")},
		{reason: "Failed", count: 3, message: `Failed to pull image "img:latest": x509: certificate is not valid`, last: parseTime("2026-11-25T10:02:00Z")},
	}
	st := startupFor(t, backoff, events)
	if st == nil {
		t.Fatal("backing-off pod got nil startup")
	}
	if st.Stage != StagePulling || st.Err != "ImagePullBackOff" {
		t.Errorf("stage/err = %d/%q", st.Stage, st.Err)
	}
	if st.PullFails != 3 {
		t.Errorf("pull fails = %d, want 3", st.PullFails)
	}
	if !strings.Contains(st.Detail, "x509") {
		t.Errorf("detail = %q, want the x509 message", st.Detail)
	}
	if st.Since != parseTime("2026-11-25T10:00:05Z") {
		t.Errorf("since = %v, want the pull event's time", st.Since)
	}
	if !st.Failing() {
		t.Error("Failing() = false for a backing-off pod")
	}
}

func TestComputeStartupCrashLoopIsReadying(t *testing.T) {
	crash := podJSON(`"phase":"Running","containerStatuses":[{"ready":false,"restartCount":4,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]`)
	st := startupFor(t, crash, nil)
	if st == nil {
		t.Fatal("crashlooping pod got nil startup")
	}
	if st.Stage != StageReadying || st.Err != "CrashLoopBackOff" {
		t.Errorf("stage/err = %d/%q, want readying/CrashLoopBackOff", st.Stage, st.Err)
	}
}

func TestParseEventsIndexesOnlyPods(t *testing.T) {
	raw := `{"items":[
		{"reason":"Pulling","message":"Pulling image","count":1,"lastTimestamp":"2026-11-25T10:00:05Z","involvedObject":{"kind":"Pod","namespace":"nightshift","name":"p1"}},
		{"reason":"LeaderElection","count":1,"involvedObject":{"kind":"Lease","namespace":"kube-system","name":"x"}},
		{"reason":"Pulled","count":2,"lastTimestamp":"2026-11-25T10:01:00Z","involvedObject":{"kind":"Pod","namespace":"nightshift","name":"p1"}}
	]}`
	events, err := parseEvents([]byte(raw))
	if err != nil {
		t.Fatalf("parseEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d keys, want 1: %v", len(events), events)
	}
	got := events["nightshift/p1"]
	if len(got) != 2 {
		t.Fatalf("got %d events for p1, want 2", len(got))
	}
	if got[0].reason != "Pulling" || got[1].reason != "Pulled" {
		t.Errorf("reasons = %q, %q", got[0].reason, got[1].reason)
	}
	if _, err := parseEvents([]byte("{")); err == nil {
		t.Error("parseEvents accepted broken json")
	}
}

func TestAnyPodStarting(t *testing.T) {
	cases := []struct {
		name string
		box  Sandbox
		want bool
	}{
		{"pending pod", Sandbox{Phase: "Pending"}, true},
		{"waiting pod", Sandbox{Phase: "Running", Waiting: "ImagePullBackOff"}, true},
		{"sandbox without a pod yet", Sandbox{Phase: "…"}, true},
		{"running ready pod", Sandbox{Phase: "Running", Ready: "1/1"}, false},
		{"succeeded job", Sandbox{Phase: "Succeeded"}, false},
	}
	for _, tc := range cases {
		if got := anyPodStarting([]Sandbox{tc.box}); got != tc.want {
			t.Errorf("%s: anyPodStarting = %v, want %v", tc.name, got, tc.want)
		}
	}
	if anyPodStarting(nil) {
		t.Error("empty fleet counts as starting")
	}
}

func TestAttachStartupKeysByNamespace(t *testing.T) {
	const podsRaw = `{"items":[
		{"metadata":{"name":"pi-pool-a","namespace":"nightshift","creationTimestamp":"2026-11-25T10:00:00Z"},"spec":{"nodeName":"n1","containers":[{"name":"agent","image":"img"}]},"status":{"phase":"Pending","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"ContainerCreating"}}}]}}
	]}`
	var pl podList
	mustParse(t, podsRaw, &pl)
	boxes := []Sandbox{{Name: "pi-pool-a"}}
	if err := attachStartup(boxes, pl, nil); err != nil {
		t.Fatalf("attachStartup: %v", err)
	}
	if boxes[0].Startup == nil {
		t.Fatal("pending pod got no startup")
	}
	if boxes[0].Startup.Stage != StagePulling {
		t.Errorf("stage = %d, want pulling", boxes[0].Startup.Stage)
	}
}

func TestAttachStartupPodlessSandboxIsScheduling(t *testing.T) {
	boxes := []Sandbox{{Name: "pr-acme-app-1-xyz", Phase: "…"}}
	if err := attachStartup(boxes, podList{}, nil); err != nil {
		t.Fatalf("attachStartup: %v", err)
	}
	st := boxes[0].Startup
	if st == nil {
		t.Fatal("podless sandbox got no startup")
	}
	if st.Stage != StageScheduling {
		t.Errorf("stage = %d, want scheduling", st.Stage)
	}
}
