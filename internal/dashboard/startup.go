package dashboard

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/cluster"
)

const startingLabel = "starting"

// stageLabel is the startup stage as the row and sidebar name it.
func stageLabel(stage cluster.StartupStage) string {
	switch stage {
	case cluster.StageScheduling:
		return "scheduling"
	case cluster.StagePulling:
		return "pulling"
	case cluster.StageReadying:
		return "readying"
	default:
		return startingLabel
	}
}

// startupBar is one glyph per startup stage: done, current, and pending.
func startupBar(stage cluster.StartupStage, failing bool, bg func(lipgloss.Style) lipgloss.Style) string {
	var b strings.Builder
	for s := 0; s < cluster.StartupStageCount; s++ {
		switch {
		case s < int(stage):
			b.WriteString(bg(lipgloss.NewStyle().Foreground(warmColor)).Render("█"))
		case s == int(stage):
			color := warnColor
			if failing {
				color = badColor
			}
			b.WriteString(bg(lipgloss.NewStyle().Foreground(color)).Render("█"))
		default:
			b.WriteString(bg(lipgloss.NewStyle().Foreground(th.FaintText)).Render("░"))
		}
	}
	return b.String()
}

// startupSubtitle is the row's bottom-left cell while a pod starts up: an
// animated marker, the stage with its failure count, a stage bar, and how
// long the stage has been going.
func startupSubtitle(st *cluster.Startup, now time.Time, compact bool, bg func(lipgloss.Style) lipgloss.Style) string {
	frame := spinner[int(now.UnixMilli()/100)%len(spinner)]
	active := bg(lipgloss.NewStyle().Foreground(warnColor))
	if st.Failing() {
		frame = "✗"
		active = bg(lipgloss.NewStyle().Foreground(badColor))
	}
	label := stageLabel(st.Stage)
	if st.PullFails > 0 {
		label += " ×" + strconv.Itoa(st.PullFails)
	}
	parts := active.Render(frame+" "+label) + " " + startupBar(st.Stage, st.Failing(), bg)
	if compact {
		parts = active.Render(frame + " " + label)
	}
	return parts + " " + bg(lipgloss.NewStyle().Foreground(th.SecondaryText)).Render(elapsed(now.Sub(st.Since)))
}

// startupStageLine is the sidebar's pipeline checklist: past stages ticked,
// the current one spinning with its elapsed time, pending ones faint.
func startupStageLine(st *cluster.Startup, now time.Time) string {
	parts := make([]string, 0, cluster.StartupStageCount)
	for s := 0; s < cluster.StartupStageCount; s++ {
		name := stageLabel(cluster.StartupStage(s))
		switch {
		case s < int(st.Stage):
			parts = append(parts, warmStyle.Render("✓ "+name))
		case s == int(st.Stage):
			frame := spinner[int(now.UnixMilli()/100)%len(spinner)]
			style := warnStyle
			if st.Failing() {
				frame, style = "✗", badStyle
			}
			parts = append(parts, style.Render(frame+" "+name+" "+elapsed(now.Sub(st.Since))))
		default:
			parts = append(parts, dimStyle.Render("○ "+name))
		}
	}
	return strings.Join(parts, dim(" · "))
}

// trimPullError keeps the informative tail of a pull failure message: the
// wrappers kubelet nests around the cause ("failed to pull image …", "failed
// to resolve reference …") push the actual error out of view.
func trimPullError(msg string) string {
	const prefix = "Failed to pull image "
	if strings.HasPrefix(msg, prefix) {
		if i := strings.Index(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
	}
	return msg
}

// tailCut keeps the end of a plain string when it is longer than width,
// since the cause of a wrapped error sits at the end.
func tailCut(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) <= width {
		return s
	}
	return "…" + s[len(s)-width+1:]
}

// startupInfoLines is the sidebar's startup block: the pipeline checklist,
// and a failure line when the startup is backing off.
func startupInfoLines(st *cluster.Startup, now time.Time, width int) []string {
	lines := []string{truncateANSI(startupStageLine(st, now), max(width, 1))}
	if st.Failing() {
		label := st.Err
		if st.PullFails > 0 {
			label += " ×" + strconv.Itoa(st.PullFails)
		}
		detail := tailCut(trimPullError(st.Detail), max(width-len(label)-4, 8))
		line := badStyle.Render("✗ "+label) + dim(" · ") + dimStyle.Render(detail)
		lines = append(lines, line)
	}
	return lines
}

// startupLineCount is how many lines the sidebar's startup block takes, so
// the layout can make room for it.
func startupLineCount(st *cluster.Startup) int {
	if st == nil {
		return 0
	}
	if st.Failing() {
		return 2
	}
	return 1
}
