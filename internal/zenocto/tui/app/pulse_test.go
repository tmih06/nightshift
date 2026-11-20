package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tmih06/nightshift/internal/zenocto/gh"
	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

func (f *fakeSearcher) setDetailState(id string, state gh.PRState) {
	f.mu.Lock()
	defer f.mu.Unlock()

	held := f.details[id]
	held.State = state
	f.details[id] = held
}

func TestAPulseSaysNothingOnTheStatusBar(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, 160, 44), "enter")
	client.serveMergeable("PR_412")
	m = settle(m, app.MergeProbe("PR_412"))

	if got := client.pulsed(); len(got) != 1 {
		t.Fatalf("setup: %d rechecks reached the client, want the one the probe makes", len(got))
	}
	bar := lastLine(render(t, m))
	if strings.Contains(bar, "Refreshing") || strings.Contains(bar, "Refreshed") {
		t.Errorf("status bar = %q, want a recheck to pass without saying so", strings.TrimSpace(bar))
	}
}

func TestAFailedPulseSaysNothingAndKeepsThePage(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")
	client.pulseErr = errors.New("502 Bad Gateway")

	m := press(loaded(t, client, 160, 44), "enter")
	m = settle(m, app.MergeProbe("PR_412"))

	out := stripANSI(render(t, m))
	if !strings.Contains(out, "Caps the backoff") {
		t.Errorf("the failed recheck took the conversation with it:\n%s", out)
	}
	if strings.Contains(lastLine(render(t, m)), "502") {
		t.Errorf("status bar = %q, want the failure kept quiet", strings.TrimSpace(lastLine(render(t, m))))
	}
}

func TestAPulseDroppedByAWriteIsAskedAgain(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, 160, 44), "enter")

	m, held := holdBack(m, app.MergeProbe("PR_412"), "pulseFetched")
	if len(held) == 0 {
		t.Fatal("setup: the probe started no recheck")
	}

	m = press(m, "1", "enter", "enter")
	client.serveMergeable("PR_412")

	before := len(client.pulsed())
	m = settle(m, held...)

	if got := len(client.pulsed()); got <= before {
		t.Fatal("the dropped recheck was never asked for again")
	}
	if out := stripANSI(render(t, m)); strings.Contains(out, "Checking") {
		t.Errorf("the Merge row latched on Checking:\n%s", out)
	}
}
