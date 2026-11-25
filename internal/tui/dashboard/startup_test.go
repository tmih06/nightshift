package dashboard

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/tmih06/nightshift/internal/tui/fleet"
)

func startupFixture(stage fleet.StartupStage) *fleet.Startup {
	return &fleet.Startup{
		Stage: stage,
		Since: time.Now().Add(-83 * time.Second),
	}
}

func TestStartupRowShowsStageAndElapsed(t *testing.T) {
	m := baseModel()
	m.boxes[0].Startup = startupFixture(fleet.StagePulling)
	row := m.renderRow(0, 100, false)
	plain := ansi.Strip(row[1])
	for _, want := range []string{"pulling", "1m23s"} {
		if !strings.Contains(plain, want) {
			t.Errorf("startup row missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "pi-template") {
		t.Errorf("startup row still shows the template subtitle: %q", plain)
	}
}

func TestStartupRowCompactDropsTheBar(t *testing.T) {
	m := baseModel()
	m.boxes[0].Startup = startupFixture(fleet.StagePulling)
	wide := ansi.Strip(m.renderRow(0, 100, false)[1])
	if !strings.Contains(wide, "█") {
		t.Errorf("wide row missing the stage bar: %q", wide)
	}
	compact := ansi.Strip(m.renderRow(0, 60, false)[1])
	if strings.Contains(compact, "█") || !strings.Contains(compact, "pulling") {
		t.Errorf("compact row should drop the bar but keep the stage: %q", compact)
	}
}

func TestStartupRowFailingShowsCrossAndCount(t *testing.T) {
	m := baseModel()
	st := startupFixture(fleet.StagePulling)
	st.Err, st.PullFails = "ImagePullBackOff", 3
	m.boxes[0].Startup = st
	plain := ansi.Strip(m.renderRow(0, 100, false)[1])
	for _, want := range []string{"✗", "×3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("failing row missing %q: %q", want, plain)
		}
	}
}

func TestStartupSidebarLines(t *testing.T) {
	st := startupFixture(fleet.StagePulling)
	lines := startupInfoLines(st, time.Now(), 60)
	if len(lines) != 1 {
		t.Fatalf("non-failing startup gave %d lines, want 1", len(lines))
	}
	plain := ansi.Strip(lines[0])
	for _, want := range []string{"✓ scheduling", "pulling 1m23s", "○ starting", "○ readying"} {
		if !strings.Contains(plain, want) {
			t.Errorf("stage line missing %q: %q", want, plain)
		}
	}
}

func TestStartupSidebarFailureLine(t *testing.T) {
	st := startupFixture(fleet.StagePulling)
	st.Err, st.PullFails = "ImagePullBackOff", 2
	st.Detail = `Failed to pull image "img:latest": failed to resolve reference: x509: certificate is not valid for any names`
	lines := startupInfoLines(st, time.Now(), 100)
	if len(lines) != 2 {
		t.Fatalf("failing startup gave %d lines, want 2", len(lines))
	}
	plain := ansi.Strip(lines[1])
	if !strings.Contains(plain, "✗ ImagePullBackOff ×2") {
		t.Errorf("failure line missing the reason and count: %q", plain)
	}
	if !strings.Contains(plain, "x509: certificate is not valid") {
		t.Errorf("failure line should keep the cause's tail: %q", plain)
	}
	if strings.Contains(plain, "Failed to pull image") {
		t.Errorf("failure line kept the boilerplate prefix: %q", plain)
	}
}

func TestStartupLineCountMatchesInfoLines(t *testing.T) {
	st := startupFixture(fleet.StageScheduling)
	if n := startupLineCount(st); n != 1 {
		t.Errorf("plain startup count = %d, want 1", n)
	}
	st.Err = "CrashLoopBackOff"
	if n := startupLineCount(st); n != 2 {
		t.Errorf("failing startup count = %d, want 2", n)
	}
	var nilStartup *fleet.Startup
	if n := startupLineCount(nilStartup); n != 0 {
		t.Errorf("nil startup count = %d, want 0", n)
	}
}

func TestInfoRowCountIncludesStartupLines(t *testing.T) {
	m := baseModel()
	before := len(m.infoLines(60))
	if got := m.infoRowCount(); got != before {
		t.Fatalf("infoRowCount = %d but infoLines emitted %d", got, before)
	}
	m.boxes[0].Startup = startupFixture(fleet.StagePulling)
	m.boxes[0].Startup.Err = "ImagePullBackOff"
	m.selectPod(m.boxes[0].Name)
	count, emitted := m.infoRowCount(), len(m.infoLines(60))
	if count != emitted {
		t.Errorf("with a failing startup: infoRowCount = %d but infoLines emitted %d", count, emitted)
	}
}

func TestTailCutKeepsTheEnd(t *testing.T) {
	if got := tailCut("short", 10); got != "short" {
		t.Errorf("tailCut short = %q", got)
	}
	got := tailCut("0123456789abcdef", 8)
	if got != "…9abcdef" {
		t.Errorf("tailCut long = %q, want %q", got, "…9abcdef")
	}
	if tailCut("abc", 0) != "" {
		t.Error("tailCut at zero width should be empty")
	}
}
