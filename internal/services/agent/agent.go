// Package agent is the catalogue of coding agents Nightshift can put in a
// sandbox: how to recognise each one's pool, which models to offer, and how
// to start it on a task.
//
// Adding an agent is one entry here plus its directory under
// images/agent/agents, named the same; k8s/warmpool.sh makes its template and
// pool from that directory. TestEveryAgentHasAnImageAndAPool keeps the three
// in step.
package agent

import (
	"slices"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// Agent is one coding agent CLI.
type Agent struct {
	// Name names the agent's image directory, template (<Name>-template), and
	// pool (<Name>-pool).
	Name  string
	Label string
	// Command is the CLI on the image's PATH.
	Command string
	// Contains and Tokens are what name the agent in a pool, template, or
	// manifest: a substring, or a whole word between non-alphanumerics (so
	// "pi" matches "pi-pool" but not "api-pool").
	Contains []string
	Tokens   []string
	// Task is a format for the shell command that runs the prompt in $p to
	// the end with nobody there to approve anything; %s is where the model
	// flag goes. Empty when the CLI has no such mode.
	Task string
	// Models are offered before the harness default.
	Models []v1.HarnessModel
}

// Harness is the agent as the contract describes it, its models ending with
// the CLI's own default.
func (a Agent) Harness() v1.Harness {
	models := append(slices.Clone(a.Models), v1.HarnessModel{ID: "", Label: labelDefault, Note: "harness default"})
	return v1.Harness{Name: a.Name, Label: a.Label, Command: a.Command, Models: models}
}

// names is whether s names a.
func (a Agent) names(s string) bool {
	low := strings.ToLower(s)
	for _, sub := range a.Contains {
		if strings.Contains(low, sub) {
			return true
		}
	}
	words := strings.FieldsFunc(low, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, tok := range a.Tokens {
		if slices.Contains(words, tok) {
			return true
		}
	}
	return false
}

// All is every agent, in the order Match tries them.
func All() []Agent { return catalogue() }

// Match is the first agent that s names.
func Match(s string) (Agent, bool) {
	all := catalogue()
	for i := range all {
		if all[i].names(s) {
			return all[i], true
		}
	}
	return Agent{}, false
}

// ByCommand is the agent whose CLI is command.
func ByCommand(command string) (Agent, bool) {
	all := catalogue()
	for i := range all {
		if all[i].Command == command {
			return all[i], true
		}
	}
	return Agent{}, false
}

// Commands are every agent's CLI, for asking a pod which one it runs.
func Commands() []string {
	all := catalogue()
	out := make([]string, 0, len(all))
	for i := range all {
		out = append(out, all[i].Command)
	}
	return out
}
