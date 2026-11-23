package dashboard

import (
	"strings"

	"github.com/tmih06/nightshift/internal/cluster"
)

// ModelOption is one selectable model for a coding agent harness.
type ModelOption struct {
	ID    string // value passed to --model; empty uses the harness default
	Label string // display name in the submenu
	Note  string // short hint shown beside the model name
}

// Harness describes the coding agent CLI installed in a sandbox pool's image.
type Harness struct {
	Name    string        // canonical harness name (e.g. "claude-code", "codex", "pi", "opencode")
	Label   string        // human-readable label (e.g. "Claude Code", "Codex", "Pi", "OpenCode")
	Command string        // CLI binary invoked inside the sandbox; empty means bash only
	Models  []ModelOption // selectable models offered in the submenu
}

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

// HarnessForPool resolves the coding agent harness and selectable models for pool.
func HarnessForPool(pool cluster.Pool) Harness {
	sources := []string{pool.Name, pool.Template, pool.TemplateYAML}
	for _, s := range sources {
		if h, ok := matchHarness(s); ok {
			return h
		}
	}
	label := pool.Template
	if label == "" {
		label = pool.Name
	}
	if label == "" {
		label = "Sandbox"
	}
	return Harness{
		Name:    pool.Name,
		Label:   label,
		Command: "",
		Models: []ModelOption{
			{ID: "", Label: "default", Note: "bash shell"},
		},
	}
}

func matchHarness(s string) (Harness, bool) {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "claude"):
		return Harness{
			Name:    "claude-code",
			Label:   "Claude Code",
			Command: "claude",
			Models: []ModelOption{
				{ID: "sonnet", Label: "sonnet", Note: "latest Sonnet"},
				{ID: "opus", Label: "opus", Note: "latest Opus"},
				{ID: "haiku", Label: "haiku", Note: "latest Haiku"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "codex"):
		return Harness{
			Name:    "codex",
			Label:   "Codex",
			Command: "codex",
			Models: []ModelOption{
				{ID: "gpt-5.3-codex", Label: "gpt-5.3-codex", Note: "coding"},
				{ID: "gpt-5.2-codex", Label: "gpt-5.2-codex", Note: "coding"},
				{ID: "gpt-5.5", Label: "gpt-5.5", Note: "general"},
				{ID: "gpt-5.2", Label: "gpt-5.2", Note: "general"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "opencode"):
		return Harness{
			Name:    "opencode",
			Label:   "OpenCode",
			Command: "opencode",
			Models: []ModelOption{
				{ID: "opencode/big-pickle", Label: "opencode/big-pickle", Note: "opencode"},
				{ID: "opencode/deepseek-v4.1-flash-free", Label: "opencode/deepseek-v4.1-flash-free", Note: "free"},
				{ID: "opencode/nemotron-3.5-super-vl-free", Label: "opencode/nemotron-3.5-super-vl-free", Note: "free"},
				{ID: "anthropic/claude-sonnet", Label: "anthropic/claude-sonnet", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case hasToken(low, "agy"):
		return Harness{
			Name:    "agy",
			Label:   "Antigravity",
			Command: "agy",
			Models: []ModelOption{
				{ID: "gemini-3.8-flash-high", Label: "gemini-3.8-flash-high", Note: "gemini"},
				{ID: "gemini-3.8-flash-medium", Label: "gemini-3.8-flash-medium", Note: "gemini"},
				{ID: "claude-sonnet-4-6", Label: "claude-sonnet-4-6", Note: "anthropic"},
				{ID: "claude-opus-4-6-thinking", Label: "claude-opus-4-6-thinking", Note: "anthropic"},
				{ID: "gpt-oss-120b-medium", Label: "gpt-oss-120b-medium", Note: "openai"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case strings.Contains(low, "ompig"):
		return Harness{
			Name:    "ompig-slim",
			Label:   "OMPi slim",
			Command: "ompig-slim",
			Models: []ModelOption{
				{ID: "anthropic/claude-sonnet-5-5", Label: "anthropic/claude-sonnet-5-5", Note: "anthropic"},
				{ID: "anthropic/claude-opus-5-5", Label: "anthropic/claude-opus-5-5", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "google/gemini-2.5-pro", Label: "google/gemini-2.5-pro", Note: "google"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	case hasToken(low, "pi"):
		return Harness{
			Name:    "pi",
			Label:   "Pi",
			Command: "pi",
			Models: []ModelOption{
				{ID: "anthropic/claude-sonnet", Label: "anthropic/claude-sonnet", Note: "anthropic"},
				{ID: "anthropic/claude-opus", Label: "anthropic/claude-opus", Note: "anthropic"},
				{ID: "openai/gpt-5.3-codex", Label: "openai/gpt-5.3-codex", Note: "openai"},
				{ID: "google/gemini-2.5-pro", Label: "google/gemini-2.5-pro", Note: "google"},
				{ID: "", Label: "default", Note: "harness default"},
			},
		}, true
	}
	return Harness{}, false
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
