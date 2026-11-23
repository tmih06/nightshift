package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tmih06/nightshift/internal/cluster"
)

func fullPodModel() model {
	created := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return model{
		boxes: []cluster.Sandbox{
			{
				Name: "pi-pool-w82js", Namespace: cluster.Namespace,
				OwnerKind: "SandboxWarmPool", Owner: "pi-pool", Template: "pi-template",
				NodeName: "control-plane", Phase: "Running", Ready: "1/1", PodIP: "10.244.0.10",
				Created: created, CPULimitMC: 1000, MemLimitMiB: 2048, CPUUsageMC: 5, MemUsageMiB: 48,
				Image: "ghcr.io/tmih06/nightshift-pi:latest", RestartPolicy: "OnFailure",
				CPUReqMC: 100, MemReqMiB: 128,
			},
			{
				Name: "claude-pr-acme-app-1-xyz", Namespace: cluster.Namespace,
				Owner: "pr-acme-app-1", Volume: "pr-acme-app-1", Template: "claude-code-template",
				NodeName: "control-plane", Phase: "Running", Ready: "1/1", PodIP: "10.244.0.11",
				Created: created, CPULimitMC: 1000, MemLimitMiB: 2048, CPUUsageMC: 120, MemUsageMiB: 310,
				Image: "ghcr.io/tmih06/nightshift-claude-code:latest",
			},
			{
				Name: "egress-proxy-6fc5ff4467-5l88s", Namespace: cluster.EgressNamespace,
				OwnerKind: "ReplicaSet", Owner: "egress-proxy-6fc5ff4467",
				NodeName: "control-plane", Phase: "Running", Ready: "1/1", PodIP: "10.244.0.224",
				Ports:   []string{"8080/proxy", "9090/admin"},
				Created: created, CPULimitMC: 200, MemLimitMiB: 128, CPUUsageMC: 1, MemUsageMiB: 8,
				Image: "nightshift-egress-proxy:local", RestartPolicy: "Always",
				CPUReqMC: 10, MemReqMiB: 32,
				PodYAML:   "apiVersion: v1\nkind: Pod\nmetadata:\n  name: egress-proxy-6fc5ff4467-5l88s",
				ExtraYAML: "## ConfigMap · egress-policy\n```yaml\nkind: ConfigMap\nmetadata:\n  name: egress-policy\n```",
			},
			{
				Name: "agent-sandbox-controller-1", Namespace: cluster.ControllerNamespace,
				OwnerKind: "ReplicaSet", Owner: "agent-sandbox-controller",
				NodeName: "control-plane", Phase: "Running", Ready: "1/1", Restarts: 7, PodIP: "10.244.0.5",
				Created: created, CPUUsageMC: 2, MemUsageMiB: 18,
				Image: "registry.k8s.io/agent-sandbox/agent-sandbox-controller:v1.0.11",
			},
			{
				Name: "clone-pr-acme-app-1-abc", Namespace: cluster.Namespace,
				OwnerKind: "Job", Owner: "clone-pr-acme-app-1",
				NodeName: "control-plane", Phase: "Pending", Ready: "0/1", Waiting: "ContainerCreating",
				Created: created, CPULimitMC: 500, MemLimitMiB: 512,
			},
		},
		pools: map[string]cluster.Pool{
			"pi-pool": {Name: "pi-pool", Template: "pi-template", Ready: 1, Desired: 1},
		},
		selected:  "egress-proxy-6fc5ff4467-5l88s",
		sessions:  map[string]bool{"claude-pr-acme-app-1-xyz": true},
		logs:      map[string]string{},
		logErr:    map[string]string{},
		logFollow: true,
		metricsOK: true,
		loaded:    true,
		width:     140,
		height:    34,
	}
}

func TestInfraSectionAndGroupHeadings(t *testing.T) {
	m := fullPodModel()
	plain := m.plainView()
	for _, want := range []string{
		"All (5)", "Claimed (1)", "Warm (1)", "Infra (3)",
		"4/5 running", "egress up",
		"CLAIMED (1)", "WARM POOLS (1)", "EGRESS PROXY (1)", "CLUSTER CORE (1)", "JOBS & OTHER (1)",
		"egress-proxy-6fc5ff4467-5l88s", "agent-sandbox-controller-1", "clone-pr-acme-app-1-abc",
		"Egress", "10.244.0.224", "8080/proxy, 9090/admin", "Logs", "Configs",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("full pods view missing %q:\n%s", want, plain)
		}
	}

	// Switch to Infra section: excludes warm and claimed sandboxes.
	m.setSection(sectionInfra)
	if len(m.navigationIndices()) != 3 {
		t.Fatalf("infra section has %d pods, want 3", len(m.navigationIndices()))
	}
	infraTable := ansi.Strip(strings.Join(m.tableView(m.layout()), "\n"))
	if strings.Contains(infraTable, "pi-pool-w82js") || strings.Contains(infraTable, "claude-pr-acme-app-1-xyz") {
		t.Errorf("infra section leaked sandbox pods:\n%s", infraTable)
	}
	if !strings.Contains(infraTable, "EGRESS PROXY (1)") || !strings.Contains(infraTable, "egress-proxy-6fc5ff4467-5l88s") {
		t.Errorf("infra section missing egress proxy:\n%s", infraTable)
	}

	// Clicking a group heading row does not change selection.
	l := m.layout()
	headingY := l.bodyTop + tableHeaderH // first line of Infra body is EGRESS PROXY (1)
	if _, ok := m.rowAt(headingY); ok {
		t.Errorf("rowAt(%d) hit a group heading row", headingY)
	}
}

func TestEgressAuditLogsAndConfigsToggle(t *testing.T) {
	m := fullPodModel()
	if m.effectivePane() != paneLogs {
		t.Fatalf("egress pod default pane = %v, want paneLogs", m.effectivePane())
	}

	// Deliver structured egress audit logs.
	rawLogs := strings.Join([]string{
		`{"time":"2026-11-22T09:12:22Z","action":"ALLOW","mode":"tunnel","method":"CONNECT","host":"registry.npmjs.org","port":443,"rule":"npm","status":200,"duration_ms":20}`,
		`{"time":"2026-11-22T09:12:55Z","action":"DENY","mode":"tunnel","method":"CONNECT","host":"models.opencode.ai","port":443,"status":403,"reason":"host/port not allowed"}`,
		`{"time":"2026-11-22T09:13:00Z","action":"ALLOW","mode":"mitm","method":"POST","host":"api.anthropic.com","port":443,"rule":"anthropic","status":200,"duration_ms":410,"scrubbed":true}`,
		`plain startup line`,
	}, "\n")
	m, _ = m.Update(logsMsg{pod: "egress-proxy-6fc5ff4467-5l88s", out: rawLogs})

	view := m.plainView()
	for _, want := range []string{
		"ALLOW", "registry.npmjs.org", "[npm]", "20ms",
		"DENY", "models.opencode.ai", "host/port not allowed",
		"api.anthropic.com", "[anthropic]", "scrubbed",
		"plain startup line",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("egress logs view missing %q:\n%s", want, view)
		}
	}

	// Pressing v switches from Logs to Configs, showing egress policy + Pod YAML.
	m, _ = m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if m.effectivePane() != paneConfigs {
		t.Fatalf("after v on egress pod, pane = %v, want paneConfigs", m.effectivePane())
	}
	cfgView := m.plainView()
	for _, want := range []string{"egress-proxy-6fc5ff4467-5l88s · egress proxy", "ConfigMap · egress-policy"} {
		if !strings.Contains(cfgView, want) {
			t.Errorf("egress configs view missing %q:\n%s", want, cfgView)
		}
	}

	// Pressing L jumps back to Logs and triggers a log fetch.
	m, cmd := m.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	if m.effectivePane() != paneLogs || cmd == nil {
		t.Fatalf("after L, pane = %v cmd=%v, want paneLogs and fetch cmd", m.effectivePane(), cmd != nil)
	}
}

func TestSidebarSubTabClicksAndClaimedViews(t *testing.T) {
	m := fullPodModel()
	m.selectPod("claude-pr-acme-app-1-xyz")
	if m.effectivePane() != paneShell {
		t.Fatalf("claimed pod default pane = %v, want paneShell", m.effectivePane())
	}

	// Verify sub-tab hit boxes match rendered text on subTabRow().
	rows := strings.Split(m.plainView(), "\n")
	subY := m.subTabRow()
	spans := m.subTabSpans()
	if len(spans) != 3 {
		t.Fatalf("claimed pod subTabSpans = %d, want 3 (Shell, Logs, Configs)", len(spans))
	}
	lineRunes := []rune(rows[subY])
	if got := string(lineRunes[spans[0].from:spans[0].to]); !strings.Contains(got, "Shell") {
		t.Errorf("span[0] = %q, want Shell", got)
	}
	if got := string(lineRunes[spans[1].from:spans[1].to]); !strings.Contains(got, "Logs") {
		t.Errorf("span[1] = %q, want Logs", got)
	}
	if got := string(lineRunes[spans[2].from:spans[2].to]); !strings.Contains(got, "Configs") {
		t.Errorf("span[2] = %q, want Configs", got)
	}

	// Clicking Logs switches from Shell to Logs even while shell is focused.
	m.focus = focusShell
	next, cmd := m.updateMouse(press(spans[1].from+1, subY))
	if next.effectivePane() != paneLogs || next.focus != focusNav || cmd == nil {
		t.Fatalf("click Logs from focusShell: pane=%v focus=%v cmd=%v", next.effectivePane(), next.focus, cmd != nil)
	}

	// Clicking Configs switches to Configs.
	nextSpans := next.subTabSpans()
	next, _ = next.updateMouse(press(nextSpans[2].from+1, next.subTabRow()))
	if next.effectivePane() != paneConfigs {
		t.Fatalf("click Configs: pane=%v, want paneConfigs", next.effectivePane())
	}

	// Clicking Shell switches back to Shell.
	nextSpans = next.subTabSpans()
	next, _ = next.updateMouse(press(nextSpans[0].from+1, next.subTabRow()))
	if next.effectivePane() != paneShell {
		t.Fatalf("click Shell: pane=%v, want paneShell", next.effectivePane())
	}
}

func TestLogsFollowAndScroll(t *testing.T) {
	m := fullPodModel()
	m.height = 18
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, "log line "+string(rune('A'+i%26)))
	}
	m, _ = m.Update(logsMsg{pod: m.selected, out: strings.Join(lines, "\n")})
	if !m.logFollow {
		t.Fatal("logs should start with follow enabled")
	}

	// Focusing logs and scrolling up disables follow and moves offset up.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.focus != focusInspect {
		t.Fatalf("enter on infra pod logs focus = %v, want focusInspect", m.focus)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.logFollow {
		t.Error("scrolling up in logs should disable follow")
	}
	// G jumps back to bottom and re-enables follow.
	m, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	if !m.logFollow {
		t.Error("G in logs should re-enable follow")
	}
}

func TestInfraPodsRefuseBashAndDestroy(t *testing.T) {
	m := fullPodModel()
	m, cmd := m.updateKeyboard(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if cmd != nil || !strings.Contains(m.status, "infra pod") {
		t.Errorf("b on egress pod = status %q cmd=%v", m.status, cmd != nil)
	}
	m, cmd = m.updateKeyboard(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if cmd != nil || !strings.Contains(m.status, "only a claimed sandbox") {
		t.Errorf("x on egress pod = status %q cmd=%v", m.status, cmd != nil)
	}
}

func TestFullPodFrameFillsItsSizeAcrossWidths(t *testing.T) {
	for _, width := range []int{60, 80, 100, 120, 160} {
		for _, sel := range []string{"claude-pr-acme-app-1-xyz", "pi-pool-w82js", "egress-proxy-6fc5ff4467-5l88s", "agent-sandbox-controller-1"} {
			m := fullPodModel()
			m.width, m.height = width, 30
			m.selectPod(sel)
			rows := strings.Split(m.View(), "\n")
			if len(rows) != m.height {
				t.Errorf("w=%d sel=%s: %d rows, want %d", width, sel, len(rows), m.height)
			}
			for i, r := range rows {
				if got := ansi.StringWidth(r); got != width {
					t.Errorf("w=%d sel=%s: row %d is %d wide, want %d", width, sel, i, got, width)
				}
			}
		}
	}
}
