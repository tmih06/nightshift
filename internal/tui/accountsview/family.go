package accountsview

import "strings"

// Model families, as the quota meters and the usage breakdown group them.
const (
	familyAccount = "Account limits"
	familyClaude  = "Claude"
	familyOpus    = "Opus"
	familySonnet  = "Sonnet"
	familyHaiku   = "Haiku"
	familyFable   = "Fable"
	familyGemini  = "Gemini"
	familyGPT     = "GPT"
	familyOther   = "Other"

	providerClaude      = "claude"
	providerCodex       = "codex"
	providerAntigravity = "antigravity"
)

// modelFamily names the family of a model, or "" when name mentions none. A
// Claude account only reaches Claude models, so there the tier is the family.
func modelFamily(provider, name string) string {
	s := strings.ToLower(name)
	switch {
	case strings.Contains(s, "gemini"):
		return familyGemini
	case strings.Contains(s, "gpt"), strings.Contains(s, "codex"), strings.HasPrefix(s, "o3"), strings.HasPrefix(s, "o4"):
		return familyGPT
	}
	tier := claudeTier(s)
	switch {
	case provider == providerClaude:
		return tier
	case tier != "" || strings.Contains(s, "claude"):
		return familyClaude
	}
	return ""
}

// claudeTier is the Claude tier a lower-case model name mentions, or "".
func claudeTier(name string) string {
	for _, tier := range []string{familyOpus, familySonnet, familyHaiku, familyFable} {
		if strings.Contains(name, strings.ToLower(tier)) {
			return tier
		}
	}
	return ""
}

// quotaFamily is the group a quota window belongs to. A window that names no
// model limits the whole account, except on Antigravity, which meters every
// model by itself.
func quotaFamily(provider, name, label string) string {
	if f := modelFamily(provider, name+" "+label); f != "" {
		return f
	}
	if provider == providerAntigravity {
		return familyOther
	}
	return familyAccount
}
