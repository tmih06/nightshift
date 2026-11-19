package session

import (
	"strings"
	"testing"
)

func TestName(t *testing.T) {
	if got := Name("agent-pool-5ss7w"); got != "sbx-agent-pool-5ss7w" {
		t.Errorf("Name = %q, want sbx-agent-pool-5ss7w", got)
	}
}

// The pane command is the whole integration: it must target the pod in
// the demo namespace with an interactive bash, through the same context
// the dashboard reads.
func TestPaneCommand(t *testing.T) {
	got := paneCommand("agent-pool-5ss7w")
	for _, want := range []string{
		"kubectl",
		"--context kind-agent-sandbox",
		"-n sandbox-demo",
		"exec -it agent-pool-5ss7w -- bash",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("paneCommand = %q, missing %q", got, want)
		}
	}
}
