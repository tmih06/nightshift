package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tmih06/nightshift/api/client"
	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

// fakeRuns holds one run, which finishes the first time it is watched twice.
type fakeRuns struct {
	mu      sync.Mutex
	req     v1.RunRequest
	watched int
	ref     string
	deleted string
}

func (f *fakeRuns) run(state string) v1.Run {
	now := time.Now().UTC()
	return v1.Run{
		ID: "run-1", State: state, Source: f.req.Source, Pool: f.req.Pool,
		ExternalRef: f.req.ExternalRef, CreatedAt: now, UpdatedAt: now,
	}
}

func (f *fakeRuns) Create(req v1.RunRequest) (v1.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.IdempotencyKey == "taken" {
		return v1.Run{}, &checkout.ConflictError{Reason: "taken"}
	}
	f.req = req
	return f.run(v1.RunQueued), nil
}

func (f *fakeRuns) List(ref string) ([]v1.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ref = ref
	return nil, nil
}

func (f *fakeRuns) Get(id string) (v1.Run, error) {
	run, _, err := f.Watch(id)
	return run, err
}

func (f *fakeRuns) Watch(id string) (v1.Run, <-chan struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "run-1" {
		return v1.Run{}, nil, checkout.ErrNotFound
	}
	f.watched++
	changed := make(chan struct{})
	close(changed)
	if f.watched > 1 {
		return f.run(v1.RunDone), changed, nil
	}
	return f.run(v1.RunWorking), changed, nil
}

func (f *fakeRuns) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = id
	return nil
}

func TestTheClientDrivesRuns(t *testing.T) {
	runs := &fakeRuns{}
	ts := newTestServer(t, &fake{}, func(c *Config) { c.Runs = runs })
	c := newClient(t, ts, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := v1.RunRequest{
		Source: v1.RunSource{CloneURL: "https://gitlab.com/acme/rocket.git"},
		Prompt: "fix it", Pool: "pi-pool", ExternalRef: "linear:ENG-42",
	}
	run, err := c.CreateRun(ctx, req)
	if err != nil || run.ID != "run-1" || run.ExternalRef != "linear:ENG-42" {
		t.Fatalf("CreateRun = %+v, %v", run, err)
	}
	var states []string
	if err := c.FollowRun(ctx, run.ID, func(r v1.Run) error { states = append(states, r.State); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states[1] != v1.RunDone {
		t.Errorf("FollowRun states = %v", states)
	}
	if got, err := c.GetRun(ctx, run.ID); err != nil || got.State != v1.RunDone {
		t.Errorf("GetRun = %+v, %v", got, err)
	}
	if list, err := c.Runs(ctx, "linear:ENG-42"); err != nil || list == nil || runs.ref != "linear:ENG-42" {
		t.Errorf("Runs = %v, %v, filter %q", list, err, runs.ref)
	}
	if err := c.DeleteRun(ctx, run.ID); err != nil || runs.deleted != run.ID {
		t.Errorf("DeleteRun: %v, deleted %q", err, runs.deleted)
	}
	if _, err := c.GetRun(ctx, "run-2"); !client.IsNotFound(err) {
		t.Errorf("GetRun of an unknown run: %v", err)
	}
	req.IdempotencyKey = "taken"
	if _, err := c.CreateRun(ctx, req); !client.IsConflict(err) {
		t.Errorf("CreateRun under a taken key: %v", err)
	}
	req.Source.CloneURL = "file:///etc"
	if _, err := c.CreateRun(ctx, req); err == nil {
		t.Error("CreateRun accepted a file address")
	}
}
