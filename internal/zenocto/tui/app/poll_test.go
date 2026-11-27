package app_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

func beat(m tea.Model, after time.Duration) tea.Model {
	return settle(m, app.PollTick(time.Now().Add(after)))
}

func opened(t *testing.T, client *fakeSearcher) tea.Model {
	t.Helper()

	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	return press(loaded(t, client, 160, 44), "enter")
}

func (f *fakeSearcher) bumpUpdated(id string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	held := f.details[id]
	held.UpdatedAt = at
	f.details[id] = held
}

func TestTheChecksBeatFiresOnlyOnTheChecksTab(t *testing.T) {
	tests := []struct {
		name   string
		keys   []string
		pulses int
	}{
		{"conversation", nil, 0},
		{"commits", []string{"]"}, 0},
		{"checks", []string{"]", "]"}, 1},
		{"files", []string{"]", "]", "]"}, 0},
		{"list", []string{"]", "]", "esc"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSearcher{prs: samplePRs()}
			m := press(opened(t, client), tt.keys...)

			_ = settle(m, app.ChecksTick(time.Now().Add(app.ChecksBeat+time.Second)))
			if got := len(client.pulsed()); got != tt.pulses {
				t.Errorf("the Checks beat made %d rechecks, want %d", got, tt.pulses)
			}
		})
	}
}

func TestTheChecksBeatRefreshesSelectedRunningJobMetadata(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "body")
	client.mu.Lock()
	d := client.details["PR_412"]
	d.Rollup = gh.CheckRollup{Checks: []gh.Check{{
		Name: "test", Workflow: "CI", State: gh.CheckStatePending, JobID: 9001,
	}}}
	client.details["PR_412"] = d
	client.mu.Unlock()
	client.servedJob(9001, gh.Job{ID: 9001, State: gh.CheckStatePending,
		Steps: []gh.JobStep{{Number: 1, Name: "Build", State: gh.CheckStatePending}}}, "")

	m := press(loaded(t, client, 160, 44), "enter", "]", "]")
	m = settleJob(m, d.Rollup.Checks[0], false)
	client.servedJob(9001, gh.Job{ID: 9001, State: gh.CheckStatePending,
		Steps: []gh.JobStep{{Number: 1, Name: "Build", State: gh.CheckStateSuccess},
			{Number: 2, Name: "Test", State: gh.CheckStatePending}}}, "")
	m = settle(m, app.ChecksTick(time.Now().Add(app.ChecksBeat+time.Second)))

	if got := client.askedJobs(); len(got) != 2 {
		t.Errorf("job asks = %v, want metadata refreshed on the beat", got)
	}
	if out := render(t, m); !strings.Contains(out, "Test") {
		t.Errorf("refreshed running steps did not reach the pane:\n%s", out)
	}
}

func TestTheChecksBeatDoesNotDoubleTheBackgroundBeat(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := press(opened(t, client), "]", "]")
	at := time.Now()

	m, background := m.Update(app.PollTick(at.Add(6 * time.Second)))
	m, checks := m.Update(app.ChecksTick(at.Add(app.ChecksBeat + time.Second)))
	_ = settle(m, append(responses(background), responses(checks)...)...)

	if got := len(client.pulsed()); got != 1 {
		t.Errorf("the two beats made %d rechecks, want one", got)
	}
}

func TestABeatRechecksAPullRequestStillMoving(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	beat(m, 6*time.Second)

	if got := client.pulsed(); len(got) != 1 {
		t.Errorf("the beat made %d rechecks, want the one the screen was owed", len(got))
	}
}

func TestABeatInsideTheIntervalAsksNothing(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	beat(m, time.Second)

	if got := client.pulsed(); len(got) != 0 {
		t.Errorf("the beat rechecked %v a second after the page landed", got)
	}
}

func TestASettledPullRequestIsAskedLessOften(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.serveMergeable("PR_412")

	m := press(loaded(t, client, 160, 44), "enter")

	m = beat(m, 6*time.Second)
	if got := client.pulsed(); len(got) != 0 {
		t.Fatalf("a settled pull request was rechecked %v after six seconds", got)
	}

	beat(m, app.PollIdle+time.Second)
	if got := client.pulsed(); len(got) != 1 {
		t.Errorf("made %d rechecks past the idle interval, want one", len(got))
	}
}

func TestABeatUnderAFormAsksNothing(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := openMergeForm(t, client)

	before := len(client.pulsed())
	beat(m, app.PollIdle+time.Second)

	if got := len(client.pulsed()); got != before {
		t.Errorf("the beat rechecked %d times under an open form, want none", got-before)
	}
}

func TestABeatSaysNothingOnTheStatusBar(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	m = beat(m, 6*time.Second)
	if got := client.pulsed(); len(got) != 1 {
		t.Fatalf("setup: the beat made %d rechecks, want one to have happened at all", len(got))
	}

	bar := lastLine(render(t, m))
	for _, word := range []string{"Refreshing", "Refreshed"} {
		if strings.Contains(bar, word) {
			t.Errorf("status bar = %q, want a beat to pass without saying so", strings.TrimSpace(bar))
		}
	}
}

func TestACommentArrivingBringsTheWholePage(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	before := len(client.opened())
	client.bumpUpdated("PR_412", time.Now())
	beat(m, 6*time.Second)

	if got := len(client.opened()) - before; got != 1 {
		t.Errorf("the page was fetched %d more times, want the one the recheck said it owed", got)
	}
}

func TestTheWholePageWaitsForTheTabItShowsOn(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := press(opened(t, client), "]")

	before := len(client.opened())
	client.bumpUpdated("PR_412", time.Now())
	m = beat(m, 6*time.Second)

	if got := len(client.opened()); got != before {
		t.Fatalf("the page was fetched %d more times away from the conversation", got-before)
	}

	m = press(m, "]", "]", "]")
	beat(m, 12*time.Second)

	if got := len(client.opened()) - before; got != 1 {
		t.Errorf("the reader came back to the conversation and the page was fetched %d times", got)
	}
}

func TestAFailedPageIsNotAskedForOnEveryBeat(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	client.bumpUpdated("PR_412", time.Now())
	client.detailErr = errors.New("502 Bad Gateway")

	before := len(client.opened())
	m = beat(m, 6*time.Second)
	if got := len(client.opened()) - before; got != 1 {
		t.Fatalf("setup: the beat asked for the page %d times, want the one that fails", got)
	}

	beat(beat(m, 12*time.Second), 18*time.Second)
	if got := len(client.opened()) - before; got != 1 {
		t.Errorf("the failed page was asked for %d times over three beats, want one", got)
	}
}

func TestAFailedPageSaysNothing(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	client.bumpUpdated("PR_412", time.Now())
	client.detailErr = errors.New("502 Bad Gateway")

	before := len(client.opened())
	m = beat(m, 6*time.Second)
	if len(client.opened()) == before {
		t.Fatal("setup: the beat asked for no page, so nothing failed")
	}

	out := stripANSI(render(t, m))
	if !strings.Contains(out, "Caps the backoff") {
		t.Errorf("the failed page took the conversation with it:\n%s", out)
	}
	if bar := lastLine(render(t, m)); strings.Contains(bar, "Could not refresh") || strings.Contains(bar, "502") {
		t.Errorf("status bar = %q, want a beat's failure kept quiet", strings.TrimSpace(bar))
	}
}

func TestARecheckThatChangesNothingLeavesTheFrameAlone(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := opened(t, client)

	was := render(t, m)
	m = beat(m, 6*time.Second)

	if got := client.pulsed(); len(got) != 1 {
		t.Fatalf("setup: the beat made %d rechecks, want one to compare across", len(got))
	}
	if now := render(t, m); now != was {
		t.Errorf("a recheck answering the same thing moved the frame:\n%s", now)
	}
}
