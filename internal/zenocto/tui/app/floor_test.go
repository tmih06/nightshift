package app_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

func TestAPickerSurvivesADragBelowTheFloor(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := openLabelPicker(t, client)

	m = settle(m, tea.WindowSizeMsg{Width: 30, Height: 8})
	if out := stripANSI(render(t, m)); !strings.Contains(out, "needs") {
		t.Fatalf("the frame below the floor is not the message:\n%s", out)
	}

	m = settle(m, tea.WindowSizeMsg{Width: 160, Height: 40})
	if out := stripANSI(render(t, m)); !strings.Contains(out, "space toggle") {
		t.Errorf("the picker did not come back with the terminal:\n%s", out)
	}
}

func TestKeysUnderTheMessageDoNotReachTheScreen(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	m := settle(openLabelPicker(t, client), tea.WindowSizeMsg{Width: 30, Height: 8})

	m = press(m, " ", "enter")
	if got := client.labelWrites(); len(got) != 0 {
		t.Errorf("a label set was written from under the message: %v", got)
	}

	m = settle(press(m, "esc"), tea.WindowSizeMsg{Width: 160, Height: 40})
	if out := stripANSI(render(t, m)); strings.Contains(out, "space toggle") {
		t.Errorf("esc under the message did not close the picker:\n%s", out)
	}
}

func TestTheRailReachesTheNarrowestFrame(t *testing.T) {
	client := &fakeSearcher{prs: samplePRs()}
	client.serveDetail("PR_412", "Caps the backoff at 30s.")

	m := press(loaded(t, client, app.MinWidth, app.MinHeight), "enter")
	if out := stripANSI(render(t, m)); !strings.Contains(out, "d details") {
		t.Fatalf("the bar does not name the key that opens the rail:\n%s", out)
	}
	out := stripANSI(render(t, press(m, "d")))
	for _, want := range []string{"Reviewers", "+ Add reviewer"} {
		if !strings.Contains(out, want) {
			t.Errorf("the rail has no %q at %dx%d:\n%s", want, app.MinWidth, app.MinHeight, out)
		}
	}
}
