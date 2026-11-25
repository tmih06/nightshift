package dashboard

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tmih06/nightshift/internal/services/cluster"
)

const podLogTailLines = 200

type logsMsg struct {
	pod string
	out string
	err error
}

func fetchLogsCmd(namespace, pod string) tea.Cmd {
	return func() tea.Msg {
		out, err := cluster.PodLogs(namespace, pod, podLogTailLines)
		return logsMsg{pod: pod, out: out, err: err}
	}
}

type egressAuditEntry struct {
	Time       string `json:"time"`
	Action     string `json:"action"`
	Mode       string `json:"mode"`
	Client     string `json:"client"`
	Method     string `json:"method"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Rule       string `json:"rule"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Reason     string `json:"reason"`
	Scrubbed   bool   `json:"scrubbed"`
}

func egressAuditTail(e *egressAuditEntry) string {
	tail := make([]string, 0, 3)
	if e.Rule != "" {
		tail = append(tail, claimStyle.Render("["+e.Rule+"]"))
	}
	if e.Reason != "" {
		tail = append(tail, badStyle.Render(e.Reason))
	} else if e.DurationMS >= 0 {
		tail = append(tail, dim(fmt.Sprintf("%dms", e.DurationMS)))
	}
	if e.Scrubbed {
		tail = append(tail, warnStyle.Render("scrubbed"))
	}
	return strings.Join(tail, " ")
}

func formatEgressAuditLine(raw string, width int) (string, bool) {
	if !strings.HasPrefix(raw, "{") {
		return "", false
	}
	var e egressAuditEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil || e.Action == "" {
		return "", false
	}

	target := e.Host
	if e.Port > 0 && e.Port != 443 {
		target = fmt.Sprintf("%s:%d", e.Host, e.Port)
	}

	var b strings.Builder
	if ts := shortLogTime(e.Time); ts != "" {
		b.WriteString(dim(ts))
		b.WriteByte(' ')
	}
	b.WriteString(logActionStyle(strings.EqualFold(e.Action, "DENY")).Render(fmt.Sprintf("%-5s", e.Action)))
	b.WriteByte(' ')
	if e.Method != "" {
		b.WriteString(dim(e.Method))
		b.WriteByte(' ')
	}
	b.WriteString(lipgloss.NewStyle().Foreground(th.PrimaryText).Render(target))
	if tail := egressAuditTail(&e); tail != "" {
		b.WriteString("  ")
		b.WriteString(tail)
	}
	return truncateANSI(b.String(), width), true
}

func shortLogTime(raw string) string {
	if raw == "" {
		return ""
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.Local().Format("15:04:05")
	}
	if len(raw) >= 19 {
		return raw[11:19]
	}
	return raw
}

func stylePlainLogLine(line string, width int) []string {
	upper := strings.ToUpper(line)
	style := lipgloss.NewStyle().Foreground(th.SecondaryText)
	switch {
	case strings.Contains(upper, "ERROR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "PANIC") || strings.HasPrefix(line, "E"):
		style = badStyle
	case strings.Contains(upper, "WARN") || strings.HasPrefix(line, "W"):
		style = warnStyle
	}
	chunks := splitWidth(line, width, width)
	out := make([]string, len(chunks))
	for i, ch := range chunks {
		out[i] = truncateANSI(style.Render(ch), width)
	}
	return out
}

func (m model) podLogLines(width int) []string {
	if width < 1 {
		width = 1
	}
	b := m.selectedBox()
	if b == nil {
		return []string{dim("nothing selected")}
	}
	if msg := m.logErr[b.Name]; msg != "" {
		return stylePlainLogLine("log error: "+msg, width)
	}
	raw, loaded := m.logs[b.Name]
	if !loaded {
		if m.loadingLogs == b.Name {
			return []string{dim("loading logs…")}
		}
		return []string{dim("press r to load logs")}
	}
	if strings.TrimSpace(raw) == "" {
		return []string{dim("no log output yet")}
	}
	rawLines := strings.Split(raw, "\n")
	out := make([]string, 0, len(rawLines))
	isEgress := b.PodRole() == cluster.RoleEgress
	for _, ln := range rawLines {
		if isEgress {
			if formatted, ok := formatEgressAuditLine(ln, width); ok {
				out = append(out, formatted)
				continue
			}
		}
		out = append(out, stylePlainLogLine(ln, width)...)
	}
	return out
}
