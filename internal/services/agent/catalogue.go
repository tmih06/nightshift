package agent

import v1 "github.com/tmih06/nightshift/api/v1"

// Names and notes the catalogue repeats.
const (
	labelDefault        = "default"
	noteAnthropic       = "anthropic"
	noteOpenAI          = "openai"
	modelClaudeSonnetPi = "anthropic/claude-sonnet"
	modelCodexOpenAI    = "openai/gpt-5.3-codex"
	modelGemini         = "google/gemini-2.5-pro"
	nameCodex           = "codex"
	nameOpencode        = "opencode"
	nameDevin           = "devin"
	nameAgy             = "agy"
	shortSonnet         = "sonnet"
	shortOpus           = "opus"
)

// model is a model offered under its own ID.
func model(id, note string) v1.HarnessModel { return v1.HarnessModel{ID: id, Label: id, Note: note} }

// short is a model the CLI resolves by family name to its latest version.
func short(id, family string) v1.HarnessModel {
	return v1.HarnessModel{ID: id, Label: id, Note: "latest " + family}
}

// catalogue is every agent. Match takes the first that names a pool, so an
// agent whose words another's could contain comes first, and the short
// tokens come last.
func catalogue() []Agent {
	return []Agent{claudeCode(), codex(), opencode(), devin(), agy(), ompigSlim(), pi()}
}

func claudeCode() Agent {
	return Agent{
		Name: "claude-code", Label: "Claude Code", Command: "claude",
		Contains: []string{"claude"},
		Task:     `claude --dangerously-skip-permissions%s --print -- "$p"`,
		Models:   []v1.HarnessModel{short(shortSonnet, "Sonnet"), short(shortOpus, "Opus"), short("haiku", "Haiku")},
	}
}

func codex() Agent {
	return Agent{
		Name: nameCodex, Label: "Codex", Command: nameCodex,
		Contains: []string{nameCodex},
		Task:     `codex exec --dangerously-bypass-approvals-and-sandbox%s -- "$p"`,
		Models: []v1.HarnessModel{
			model("gpt-5.3-codex", "coding"), model("gpt-5.2-codex", "coding"),
			model("gpt-5.5", "general"), model("gpt-5.2", "general"),
		},
	}
}

func opencode() Agent {
	return Agent{
		Name: nameOpencode, Label: "OpenCode", Command: nameOpencode,
		Contains: []string{nameOpencode},
		Task:     `opencode run --auto%s -- "$p"`,
		Models: []v1.HarnessModel{
			model("opencode/big-pickle", nameOpencode),
			model("opencode/deepseek-v4.1-flash-free", "free"),
			model("opencode/nemotron-3.5-super-vl-free", "free"),
			model(modelClaudeSonnetPi, noteAnthropic),
			model(modelCodexOpenAI, noteOpenAI),
		},
	}
}

// devin is Cognition's Devin CLI. Its family names always resolve to the
// newest model in the family.
func devin() Agent {
	return Agent{
		Name: nameDevin, Label: "Devin", Command: nameDevin,
		Contains: []string{nameDevin},
		// --print cannot ask whether to trust the workspace, so it skips it.
		Task: `devin --permission-mode dangerous --respect-workspace-trust=false%s --print -- "$p"`,
		Models: []v1.HarnessModel{
			short("swe", "SWE"), short(shortOpus, "Opus"), short(shortSonnet, "Sonnet"),
			short("gpt", "GPT"), short(nameCodex, "Codex"), short("gemini", "Gemini"),
		},
	}
}

func agy() Agent {
	return Agent{
		Name: nameAgy, Label: "Antigravity", Command: nameAgy,
		Tokens: []string{nameAgy},
		Task:   `agy --dangerously-skip-permissions%s --print="$p"`,
		Models: []v1.HarnessModel{
			model("gemini-3.8-flash-high", "gemini"), model("gemini-3.8-flash-medium", "gemini"),
			model("claude-sonnet-4-6", noteAnthropic), model("claude-opus-4-6-thinking", noteAnthropic),
			model("gpt-oss-120b-medium", noteOpenAI),
		},
	}
}

func ompigSlim() Agent {
	return Agent{
		Name: "ompig-slim", Label: "OMPi slim", Command: "ompig-slim",
		Contains: []string{"ompig"},
		Task:     `ompig-slim%s --print -- "$p"`,
		Models: []v1.HarnessModel{
			model("anthropic/claude-sonnet-5-5", noteAnthropic), model("anthropic/claude-opus-5-5", noteAnthropic),
			model(modelCodexOpenAI, noteOpenAI), model(modelGemini, "google"),
		},
	}
}

func pi() Agent {
	return Agent{
		Name: "pi", Label: "Pi", Command: "pi",
		Tokens: []string{"pi"},
		Task:   `pi%s --print -- "$p"`,
		Models: []v1.HarnessModel{
			model(modelClaudeSonnetPi, noteAnthropic), model("anthropic/claude-opus", noteAnthropic),
			model(modelCodexOpenAI, noteOpenAI), model(modelGemini, "google"),
		},
	}
}
