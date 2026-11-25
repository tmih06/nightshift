package checkout

import (
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// LaunchCommand builds the shell command to run command with an optional model.
// An empty command returns "" (leave the sandbox in bash).
func LaunchCommand(command, model string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return shellQuote(command)
	}
	return shellQuote(command) + " --model " + shellQuote(model)
}

// ForPool resolves the coding agent harness and selectable models of a pool, from
// its name, its template's name, and the template's manifest.
func ForPool(name, template, templateYAML string) v1.Harness {
	sources := []string{name, template, templateYAML}
	for _, s := range sources {
		if h, ok := matchHarness(s); ok {
			return h
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

func matchHarness(s string) (v1.Harness, bool) {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "claude"):
		return v1.Harness{
			Name:    "claude-code",
			Label:   "Claude Code",
			Command: "claude",
			Models: []v1.HarnessModel{
				{ID: "sonnet", Label: "sonnet", Note: "latest Sonnet"},
				{ID: "opus", Label: "opus", Note: "latest Opus"},
				{ID: "haiku", Label: "haiku", Note: "latest Haiku"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "codex"):
		return v1.Harness{
			Name:    "codex",
			Label:   "Codex",
			Command: "codex",
			Models: []v1.HarnessModel{
				{ID: "gpt-5.3-codex", Label: "gpt-5.3-codex", Note: "coding"},
				{ID: "gpt-5.2-codex", Label: "gpt-5.2-codex", Note: "coding"},
				{ID: "gpt-5.5", Label: "gpt-5.5", Note: "general"},
				{ID: "gpt-5.2", Label: "gpt-5.2", Note: "general"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "opencode"):
		return v1.Harness{
			Name:    "opencode",
			Label:   "OpenCode",
			Command: "opencode",
			Models: []v1.HarnessModel{
				{ID: "opencode/big-pickle", Label: "opencode/big-pickle", Note: "opencode"},
				{ID: "opencode/deepseek-v4.1-flash-free", Label: "opencode/deepseek-v4.1-flash-free", Note: "free"},
				{ID: "opencode/nemotron-3.5-super-vl-free", Label: "opencode/nemotron-3.5-super-vl-free", Note: "free"},
				{ID: "anthropic/claude-sonnet", Label: "anthropic/claude-sonnet", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case hasToken(low, "agy"):
		return v1.Harness{
			Name:    "agy",
			Label:   "Antigravity",
			Command: "agy",
			Models: []v1.HarnessModel{
				{ID: "gemini-3.8-flash-high", Label: "gemini-3.8-flash-high", Note: "gemini"},
				{ID: "gemini-3.8-flash-medium", Label: "gemini-3.8-flash-medium", Note: "gemini"},
				{ID: "claude-sonnet-4-6", Label: "claude-sonnet-4-6", Note: "anthropic"},
				{ID: "claude-opus-4-6-thinking", Label: "claude-opus-4-6-thinking", Note: "anthropic"},
				{ID: "gpt-oss-120b-medium", Label: "gpt-oss-120b-medium", Note: "openai"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "ompig"):
		return v1.Harness{
			Name:    "ompig-slim",
			Label:   "OMPi slim",
			Command: "ompig-slim",
			Models: []v1.HarnessModel{
				{ID: "anthropic/claude-sonnet-5-5", Label: "anthropic/claude-sonnet-5-5", Note: "anthropic"},
				{ID: "anthropic/claude-opus-5-5", Label: "anthropic/claude-opus-5-5", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "google/gemini-2.5-pro", Label: "google/gemini-2.5-pro", Note: "google"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case hasToken(low, "pi"):
		return v1.Harness{
			Name:    "pi",
			Label:   "Pi",
			Command: "pi",
			Models: []v1.HarnessModel{
				{ID: "anthropic/claude-sonnet", Label: "anthropic/claude-sonnet", Note: "anthropic"},
				{ID: "anthropic/claude-opus", Label: "anthropic/claude-opus", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "google/gemini-2.5-pro", Label: "google/gemini-2.5-pro", Note: "google"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	}
	return v1.Harness{}, false
}

// hasToken checks whether tok appears as a standalone identifier segment
// delimited by non-alphanumeric boundaries (so "pi-pool" matches "pi" while
// "api-pool" does not).
func hasToken(s, tok string) bool {
	if tok == "" {
		return false
	}
	for i := 0; i <= len(s)-len(tok); {
		idx := strings.Index(s[i:], tok)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(tok)
		leftOK := start == 0 || !isAlphaNum(s[start-1])
		rightOK := end == len(s) || !isAlphaNum(s[end])
		if leftOK && rightOK {
			return true
		}
		i = start + 1
	}
	return false
}

func isAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
