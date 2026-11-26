package runs

import (
	"fmt"
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

// launchCommand stores script, and its stdin as the prompt, then starts the
// script in a new terminal in dir, which stays a shell afterwards. It is one
// exec that ends at once: typing into a session instead keeps a kubectl alive
// in the services for as long as the sandbox lives.
func launchCommand(script, dir string) []string {
	start := "bash " + stateDir + "/" + scriptFile + "; exec bash"
	launch := "mkdir -p " + stateDir +
		` && printf %s "$1" > ` + stateDir + "/" + scriptFile +
		" && cat > " + stateDir + "/" + promptFile +
		` && hc run --bg --no-parent --cwd "$2" bash -c ` + checkout.Quote(start)
	return []string{"sh", "-c", launch, "sh", script, dir}
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
	lines := append([]string{"export GIT_ALLOW_PROTOCOL=https"}, checkout.CloneLines(s.CloneURL)...)
	if s.Ref == "" {
		return strings.Join(append(lines, "git checkout -B "+checkout.Quote(branchOf(id, s))), "\n")
	}
	return strings.Join(append(lines, checkout.FetchLines(s.Ref, branchOf(id, s))...), "\n")
}
