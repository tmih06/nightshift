package app_test

import (
	"context"
	"testing"

	"github.com/tmih06/nightshift/internal/zenocto/tui/app"
)

// Read from the client rather than a frame: mismatched times only cost a refetch, which nothing on screen shows.
func TestTheMockupPulseAgreesWithTheFetch(t *testing.T) {
	const id = "PR_webhook-replay"
	fetched, err := app.Mock{}.PullRequest(context.Background(), id, "")
	if err != nil {
		t.Fatal(err)
	}
	pulsed, err := app.Mock{}.Pulse(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	if !pulsed.Pulse.UpdatedAt.Equal(fetched.Detail.UpdatedAt) {
		t.Errorf("pulse updatedAt %v, fetch %v, so every pulse marks the timeline stale", pulsed.Pulse.UpdatedAt, fetched.Detail.UpdatedAt)
	}
}
