package app_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/tui/prview"
)

// A hint never contains two spaces, so the first run of them ends the hints.
var gapRun = regexp.MustCompile(`\s{2,}`)

func hintTokens(t *testing.T, m tea.Model) []string {
	t.Helper()
	bar := strings.TrimSpace(stripANSI(lastLine(render(t, m))))
	if bar == "" {
		return nil
	}
	return strings.Split(gapRun.Split(bar, 2)[0], " • ")
}

func TestTheJobLogSearchTakesTheBarAndTheRootKeys(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "body")
	client.mu.Lock()
	d := client.details["PR_412"]
	d.Rollup = gh.CheckRollup{Checks: []gh.Check{{Name: "test", Workflow: "CI", State: gh.CheckStateSuccess, JobID: 9001}}}
	client.details["PR_412"] = d
	client.mu.Unlock()
	client.servedJob(9001, gh.Job{
		ID: 9001, Name: "test", State: gh.CheckStateSuccess,
		Steps: []gh.JobStep{{Number: 1, Name: "Run tests", State: gh.CheckStateSuccess}},
	}, "2026-08-19T14:00:00Z queued\n2026-08-19T14:00:01Z ok\n")

	m := settleJob(press(loaded(t, client, 160, 40), "enter", "]", "]"), d.Rollup.Checks[0], false)
	m = press(m, "/")

	bar := strings.Join(hintTokens(t, m), " • ")
	if bar != "⏎ apply • esc clear" {
		t.Errorf("status bar = %q, want the search bar's own two keys", bar)
	}

	m = settle(press(m, "q", "?"), prview.SearchSettleMsg{Query: "q?"})
	out := stripANSI(render(t, m))
	if strings.Contains(out, "Keys") && strings.Contains(out, "next tab") {
		t.Errorf("? opened the help overlay from inside the search bar:\n%s", out)
	}
	if !strings.Contains(out, "q?") {
		t.Errorf("the query did not take the letters:\n%s", out)
	}
}

func TestEscReadsAsClearingASettledJobLogSearch(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "body")
	client.mu.Lock()
	d := client.details["PR_412"]
	d.Rollup = gh.CheckRollup{Checks: []gh.Check{{Name: "test", Workflow: "CI", State: gh.CheckStateSuccess, JobID: 9001}}}
	client.details["PR_412"] = d
	client.mu.Unlock()
	client.servedJob(9001, gh.Job{
		ID: 9001, Name: "test", State: gh.CheckStateSuccess,
		Steps: []gh.JobStep{{Number: 1, Name: "Run tests", State: gh.CheckStateSuccess}},
	}, "2026-08-19T14:00:00Z ok\n")

	m := settleJob(press(loaded(t, client, 160, 40), "enter", "]", "]"), d.Rollup.Checks[0], false)
	m = settle(press(m, "/", "o", "k"), prview.SearchSettleMsg{Query: "ok"})
	m = press(m, "enter")

	bar := strings.Join(hintTokens(t, m), " • ")
	if !strings.Contains(bar, "esc clear search") {
		t.Errorf("status bar = %q, want esc named as clearing the search", bar)
	}
	if strings.Contains(bar, "esc back") {
		t.Errorf("status bar = %q, want esc off the line as a way out while a query stands", bar)
	}
}

func TestWalkingTheChecksColumnDoesNotBlinkTheLogKeys(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "body")
	client.mu.Lock()
	d := client.details["PR_412"]
	d.Rollup = gh.CheckRollup{Checks: []gh.Check{
		{Name: "test", Workflow: "CI", State: gh.CheckStateFailure, JobID: 9001},
		{Name: "lint", Workflow: "CI", State: gh.CheckStateFailure, JobID: 9002},
	}}
	client.details["PR_412"] = d
	client.mu.Unlock()
	for _, id := range []int64{9001, 9002} {
		client.servedJob(id, gh.Job{
			ID: id, Name: "test", State: gh.CheckStateFailure,
			Steps: []gh.JobStep{{Number: 1, Name: "Run", State: gh.CheckStateFailure}},
		}, "2026-08-19T14:00:00Z boom\n")
	}

	m := settleJob(press(loaded(t, client, 160, 40), "enter", "]", "]"), d.Rollup.Checks[0], false)
	landed := hintTokens(t, m)
	for _, want := range []string{"{/} block", "/ search log", "f first failure"} {
		if !slices.Contains(landed, want) {
			t.Fatalf("setup: line = %v, want %q on it once a job has landed", landed, want)
		}
	}

	if moved := hintTokens(t, press(m, "j")); !slices.Equal(moved, landed) {
		t.Errorf("the line changed while the next job was still on its way:\n before %v\n after  %v", landed, moved)
	}
}

func TestTheBarGoesQuietForEveryBoxThatDrawsItsOwnHints(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T, *fakeSearcher) tea.Model
	}{
		{name: "merge form", open: openMergeForm},
		{
			name: "compose box",
			open: func(t *testing.T, client *fakeSearcher) tea.Model {
				t.Helper()
				m := press(loaded(t, client, 160, 40), "enter", "c")
				if out := stripANSI(render(t, m)); !strings.Contains(out, "ctrl+e") {
					t.Fatalf("setup: c opened no compose box:\n%s", out)
				}
				return m
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSearcher{prs: samplePRs()}
			client.serveDetail("PR_412", "Caps the backoff at 30s.")

			bar := stripANSI(lastLine(render(t, tt.open(t, client))))
			for _, gone := range []string{"j/k move", "esc back", "? help"} {
				if strings.Contains(bar, gone) {
					t.Errorf("status bar = %q, want %q off it: the box has that key", strings.TrimSpace(bar), gone)
				}
			}
		})
	}
}
