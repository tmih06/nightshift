package checkout

import (
	"fmt"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/agent"
)

// LaunchCommand builds the shell command to run command with an optional model.
// An empty command returns "" (leave the sandbox in bash).
func LaunchCommand(command, model string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	return Quote(command) + modelFlag(strings.TrimSpace(model))
}

func modelFlag(model string) string {
	if model == "" {
		return ""
	}
	return " --model " + Quote(model)
}

// TaskCommand is the shell command that has the agent whose CLI is command
// run the prompt in $p and exit, and whether the agent has such a mode.
// Approvals are off: nobody is there to answer them, and the sandbox is what
// contains the agent.
func TaskCommand(command, model string) (string, bool) {
	a, ok := agent.ByCommand(command)
	if !ok || a.Task == "" {
		return "", false
	}
	return fmt.Sprintf(a.Task, modelFlag(strings.TrimSpace(model))), true
}

// ForPool resolves the coding agent harness and selectable models of a pool, from
// its name, its template's name, and the template's manifest.
func ForPool(name, template, templateYAML string) v1.Harness {
	for _, s := range []string{name, template, templateYAML} {
		if a, ok := agent.Match(s); ok {
			return a.Harness()
		}
	}
	label := template
	if label == "" {
		label = name
	}
	if label == "" {
		label = "Sandbox"
	}
	return v1.Harness{
		Name:    name,
		Label:   label,
		Command: "",
		Models: []v1.HarnessModel{
			{ID: "", Label: "default", Note: "bash shell"},
		},
	}
}
