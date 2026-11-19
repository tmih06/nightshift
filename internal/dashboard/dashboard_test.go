package dashboard

import (
	"errors"
	"strings"
	"testing"

	"github.com/tmih06/nightshift/internal/cluster"
)

func TestBar(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{0, "░░░░░░░░░░░"},
		{0.5, "█████░░░░░░"},
		{1, "███████████"},
		{-1, "░░░░░░░░░░░"},
		{2, "███████████"},
	}
	for _, tc := range cases {
		if got := bar(tc.ratio, 11); got != tc.want {
			t.Errorf("bar(%v, 11) = %q, want %q", tc.ratio, got, tc.want)
		}
	}
}

func TestResourceLine(t *testing.T) {
	if got := resourceLine("CPU", true, 4, 500, "m"); !strings.Contains(got, "4m/500m") || !strings.Contains(got, "1%") || strings.Contains(got, "█") {
		t.Errorf("cpu line = %q, want 4m/500m at 1%% with an empty bar", got)
	}
	if got := resourceLine("CPU", true, 250, 500, "m"); !strings.Contains(got, "50%") || !strings.Contains(got, "█") || !strings.Contains(got, "░") {
		t.Errorf("cpu line = %q, want 50%% with a half-filled bar", got)
	}
	if got := resourceLine("MEM", false, 0, 256, "Mi"); !strings.Contains(got, "--/256Mi") {
		t.Errorf("mem line = %q, want --/256Mi when usage is unknown", got)
	}
	if got := resourceLine("CPU", true, 4, 0, "m"); !strings.Contains(got, "no limit") {
		t.Errorf("cpu line = %q, want the no-limit fallback", got)
	}
}

func TestViewFrame(t *testing.T) {
	m := model{
		boxes: []cluster.Sandbox{
			{Name: "agent-pool-5ss7w", OwnerKind: "SandboxWarmPool", Owner: "agent-pool",
				Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256, CPUUsageMC: 2, MemUsageMiB: 6},
			{Name: "agent-pool-bkl2p", OwnerKind: "SandboxClaim", Owner: "my-agent",
				Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256, CPUUsageMC: 0, MemUsageMiB: 3},
		},
		metricsOK: true,
		loaded:    true,
		width:     100,
	}
	got := m.View()
	for _, want := range []string{
		"nightshift",
		"agent-pool-5ss7w",
		"agent-pool-bkl2p",
		"warm",
		"claimed",
		"Running",
		"2m/500m",
		"6Mi/256Mi",
		"0m/500m",
		"3Mi/256Mi",
		"● warm",
		"● claimed",
		"╭",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q\n%v", want, got)
		}
	}
	// The warm box sorts ahead of the claimed one.
	if strings.Index(got, "agent-pool-5ss7w") > strings.Index(got, "agent-pool-bkl2p") {
		t.Error("warm box should render before the claimed box")
	}
}

func TestViewStates(t *testing.T) {
	m := model{
		boxes: []cluster.Sandbox{{Name: "agent-pool-5ss7w", OwnerKind: "SandboxWarmPool",
			Phase: "Running", CPULimitMC: 500, MemLimitMiB: 256}},
		metricsOK: false,
		loaded:    true,
		width:     100,
	}
	if got := m.View(); !strings.Contains(got, "--/500m") || !strings.Contains(got, "metrics-server") {
		t.Errorf("view without metrics = %q, want --/500m and the metrics-server hint", got)
	}

	m.err = errFixture
	if got := m.View(); !strings.Contains(got, "fetch error") {
		t.Errorf("view with a fetch error = %q, want the error banner", got)
	}

	m.err = nil
	m.boxes = nil
	if got := m.View(); !strings.Contains(got, "no sandboxes") {
		t.Errorf("empty view = %q, want the no-sandboxes note", got)
	}

	if got := (model{}).View(); !strings.Contains(got, "loading") {
		t.Errorf("initial view = %q, want the loading note", got)
	}
}

var errFixture = errors.New("cluster unreachable")
