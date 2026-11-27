package egress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	claudeModelsURL      = "https://api.anthropic.com/v1/models?limit=1000"
	codexModelsURL       = "https://chatgpt.com/backend-api/codex/models?client_version=0.171.0"
	antigravityModelsURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	// antigravityConsumerProject serves accounts whose load names no project.
	antigravityConsumerProject = "aicode-consumers"
)

// Model is one model an account can be asked for.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// fetchModels lists what the account's own CLI would offer, in its order. A
// provider's raw catalog is not that list: it holds internal, retired, and
// disabled models.
func (qs *quotaStore) fetchModels(ctx context.Context, binding string, acct resolvedAccount) ([]Model, error) {
	switch binding {
	case bindingClaude:
		return qs.fetchClaudeModels(ctx, acct)
	case bindingCodex:
		return qs.fetchCodexModels(ctx, acct)
	case bindingAgy:
		return qs.fetchAntigravityModels(ctx, acct)
	default:
		return nil, nil
	}
}

func (qs *quotaStore) getModels(ctx context.Context, endpoint, token string, header http.Header, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("build models request: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+token)
	raw, err := qs.doJSON(req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("parse models: %w", err)
	}
	return nil
}

func (qs *quotaStore) fetchClaudeModels(ctx context.Context, acct resolvedAccount) ([]Model, error) {
	header := http.Header{}
	header.Set("Anthropic-Beta", "oauth-2025-04-20")
	header.Set("Anthropic-Version", "2023-06-01")
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := qs.getModels(ctx, qs.endpoints.ClaudeModels, acct.value, header, &body); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, Model{ID: m.ID, Name: m.DisplayName})
	}
	return out, nil
}

// fetchCodexModels keeps the models Codex lists in its picker.
func (qs *quotaStore) fetchCodexModels(ctx context.Context, acct resolvedAccount) ([]Model, error) {
	header := http.Header{}
	if id := acct.companions[DefaultPlaceholderPrefix+"CODEX_ACCOUNT_ID"]; id != "" {
		header.Set("Chatgpt-Account-Id", id)
	}
	var body struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err := qs.getModels(ctx, qs.endpoints.CodexModels, acct.value, header, &body); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range body.Models {
		if m.Visibility == "list" {
			out = append(out, Model{ID: m.Slug, Name: m.DisplayName})
		}
	}
	return out, nil
}

type antigravityCatalog struct {
	Models map[string]struct {
		DisplayName string `json:"displayName"`
		Disabled    bool   `json:"disabled"`
	} `json:"models"`
	AgentModelSorts []struct {
		Groups []struct {
			ModelIDs []string `json:"modelIds"`
		} `json:"groups"`
	} `json:"agentModelSorts"`
}

// offered is agy's picker: the sorted agent models, without the disabled.
// This is what `agy models` prints.
func (c antigravityCatalog) offered() []Model {
	var out []Model
	seen := map[string]bool{}
	for _, sorted := range c.AgentModelSorts {
		for _, group := range sorted.Groups {
			for _, id := range group.ModelIDs {
				m, ok := c.Models[id]
				if ok && !m.Disabled && !seen[id] {
					seen[id] = true
					out = append(out, Model{ID: id, Name: m.DisplayName})
				}
			}
		}
	}
	return out
}

func (qs *quotaStore) fetchAntigravityModels(ctx context.Context, acct resolvedAccount) ([]Model, error) {
	project, _, err := qs.fetchAntigravityProject(ctx, acct)
	if err != nil {
		return nil, fmt.Errorf("load antigravity project: %w", err)
	}
	if project == "" {
		project = antigravityConsumerProject
	}
	raw, err := qs.postAntigravity(ctx, qs.endpoints.AntigravityModels, map[string]string{"project": project}, acct.value, "models")
	if err != nil {
		return nil, err
	}
	var catalog antigravityCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("parse antigravity models: %w", err)
	}
	return catalog.offered(), nil
}
