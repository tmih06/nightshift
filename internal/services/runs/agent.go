package runs

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

// Files a run keeps in the sandbox's home, out of the clone.
const (
	promptFile = "prompt"
	scriptFile = "run.sh"
	stateDir   = `"$HOME/.nightshift"`
)

// agentCommand is how each coding agent runs one prompt and exits. It reads the
// prompt from $p. Approvals are off: nobody is there to answer them, and the
// sandbox is what contains the agent. model is "" or a flag and its value.
func agentCommand(agent, model string) (string, bool) {
	format, ok := map[string]string{
		"claude":     `claude --dangerously-skip-permissions%s --print -- "$p"`,
		"codex":      `codex exec --dangerously-bypass-approvals-and-sandbox%s -- "$p"`,
		"opencode":   `opencode run --auto%s -- "$p"`,
		"agy":        `agy --dangerously-skip-permissions%s --print="$p"`,
		"pi":         `pi%s --print -- "$p"`,
		"ompig-slim": `ompig-slim%s --print -- "$p"`,
	}[agent]
	if !ok {
		return "", false
	}
	if model != "" {
		model = " --model " + checkout.Quote(model)
	}
	return fmt.Sprintf(format, model), true
}

// agentScript runs the agent in dir, shows what it prints, and leaves its exit
// status in a file however it ends, so the services can read the result after a
// restart. tail follows the log rather than a pipe, which a process the agent
// left behind would hold open.
func agentScript(command, dir string, seconds int) string {
	return strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -u",
		"d=" + stateDir,
		`finish() { rc=$?; [ -f "$d/exit" ] || { echo "$rc" > "$d/exit.tmp" && mv "$d/exit.tmp" "$d/exit"; }; }`,
		"trap finish EXIT",
		"trap 'exit 129' HUP",
		"trap 'exit 130' INT",
		"trap 'exit 143' TERM",
		"cd " + checkout.Quote(dir) + " || exit 1",
		`p=$(cat "$d/` + promptFile + `")`,
		`: > "$d/log"`,
		"timeout -k 30 " + strconv.Itoa(seconds) + " " + command + ` < /dev/null >> "$d/log" 2>&1 &`,
		"agent=$!",
		`tail -n +1 -f --pid="$agent" "$d/log"`,
		`wait "$agent"`,
		"exit $?",
		"",
	}, "\n")
}

// writeCommand stores its stdin as name in the run's directory.
func writeCommand(name string) []string {
	return []string{"sh", "-c", "mkdir -p " + stateDir + " && cat > " + stateDir + "/" + name}
}

// statusScript prints how the agent stands: "exit N" and the end of its output,
// "running", or "gone" when the sandbox no longer has the run.
const statusScript = `d="$HOME/.nightshift"
if [ -f "$d/exit" ]; then printf 'exit %s\n' "$(cat "$d/exit")"; tail -c 16384 "$d/log" 2>/dev/null
elif [ -f "$d/run.sh" ]; then echo running
else echo gone; fi`

// status is what statusScript said.
type status struct {
	exited bool
	code   int
	gone   bool
	output string
}

func parseStatus(out []byte) (status, error) {
	first, rest, _ := strings.Cut(string(out), "\n")
	switch first {
	case "running":
		return status{}, nil
	case "gone":
		return status{gone: true}, nil
	}
	raw, ok := strings.CutPrefix(first, "exit ")
	code, err := strconv.Atoi(raw)
	if !ok || err != nil {
		return status{}, fmt.Errorf("the sandbox answered %q", first)
	}
	return status{exited: true, code: code, output: strings.ToValidUTF8(rest, "")}, nil
}

// branchOf is the local branch the agent works on.
func branchOf(id string, s v1.RunSource) string {
	if s.Branch != "" {
		return s.Branch
	}
	return "nightshift/" + id
}

// cloneScript runs at the volume's root in the clone Job. Only https is
// allowed, redirects included.
func cloneScript(id string, s v1.RunSource) string {
	u, err := url.Parse(s.CloneURL)
	github := err == nil && strings.EqualFold(u.Host, "github.com")
	lines := append([]string{"export GIT_ALLOW_PROTOCOL=https"}, checkout.CloneLines(s.CloneURL, github)...)
	branch := checkout.Quote(branchOf(id, s))
	if s.Ref == "" {
		return strings.Join(append(lines, "git checkout -B "+branch), "\n")
	}
	lines = append(lines, checkout.RetryLines("fetch", "", "git fetch --progress origin "+checkout.Quote(s.Ref)+" 2>&1 | lines")...)
	return strings.Join(append(lines, "git checkout -B "+branch+" FETCH_HEAD"), "\n")
}

// repoPath names the repository for the volume listing: the address's path.
func repoPath(s v1.RunSource) string {
	u, err := url.Parse(s.CloneURL)
	if err != nil {
		return s.RepoName()
	}
	return strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
}
