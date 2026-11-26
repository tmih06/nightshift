package egress

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TokenMetrics captures LLM token usage extracted from one upstream response.
type TokenMetrics struct {
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
	Total      int64
}

func (m TokenMetrics) empty() bool {
	return m.Input == 0 && m.Output == 0 && m.CacheRead == 0 && m.CacheWrite == 0 && m.Reasoning == 0 && m.Total == 0
}

func (m *TokenMetrics) mergeMax(o TokenMetrics) {
	if o.Model != "" {
		m.Model = o.Model
	}
	m.Input = max(m.Input, o.Input)
	m.Output = max(m.Output, o.Output)
	m.CacheRead = max(m.CacheRead, o.CacheRead)
	m.CacheWrite = max(m.CacheWrite, o.CacheWrite)
	m.Reasoning = max(m.Reasoning, o.Reasoning)
	m.Total = max(m.Total, o.Total)
}

func (m *TokenMetrics) finalize() {
	sum := m.Input + m.Output + m.CacheRead + m.CacheWrite + m.Reasoning
	if m.Total == 0 && sum > 0 {
		m.Total = sum
	}
}

// extractTokenMetrics parses token usage from JSON or SSE bodies returned by
// Anthropic, OpenAI/Codex, and Google Gemini/Antigravity APIs.
func extractTokenMetrics(contentType string, body []byte) TokenMetrics {
	if len(body) == 0 {
		return TokenMetrics{}
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return TokenMetrics{}
	}
	var out TokenMetrics
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") || bytes.HasPrefix(trimmed, []byte("event:")) || bytes.HasPrefix(trimmed, []byte("data:")) {
		out = extractSSEMetrics(trimmed)
	} else if trimmed[0] == '{' {
		out = extractJSONMetrics(trimmed)
	}
	out.finalize()
	return out
}

func extractSSEMetrics(body []byte) TokenMetrics {
	var out TokenMetrics
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		payload, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		payload = bytes.TrimSpace(payload)
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) || payload[0] != '{' {
			continue
		}
		out.mergeMax(extractJSONMetrics(payload))
	}
	return out
}

type usagePayload struct {
	Model        string           `json:"model"`
	ModelVersion string           `json:"modelVersion"`
	Usage        *flatUsage       `json:"usage"`
	UsageMeta    *geminiUsage     `json:"usageMetadata"`
	Message      *nestedContainer `json:"message"`
	Response     *nestedContainer `json:"response"`
}

type nestedContainer struct {
	Model        string       `json:"model"`
	ModelVersion string       `json:"modelVersion"`
	Usage        *flatUsage   `json:"usage"`
	UsageMeta    *geminiUsage `json:"usageMetadata"`
}

type flatUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	PromptTokens             int64 `json:"prompt_tokens"`
	CompletionTokens         int64 `json:"completion_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	InputDetails             *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	PromptDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	OutputDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	CompletionDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type geminiUsage struct {
	PromptTokenCount        int64 `json:"promptTokenCount"`
	CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
	ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
	TotalTokenCount         int64 `json:"totalTokenCount"`
}

func extractJSONMetrics(raw []byte) TokenMetrics {
	var p usagePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return TokenMetrics{}
	}
	var out TokenMetrics
	out.Model = firstNonEmpty(p.Model, p.ModelVersion)
	applyFlatUsage(&out, p.Usage)
	applyGeminiUsage(&out, p.UsageMeta)
	applyContainer(&out, p.Message)
	applyContainer(&out, p.Response)
	return out
}

func applyContainer(out *TokenMetrics, c *nestedContainer) {
	if c == nil {
		return
	}
	if m := firstNonEmpty(c.Model, c.ModelVersion); m != "" {
		out.Model = m
	}
	applyFlatUsage(out, c.Usage)
	applyGeminiUsage(out, c.UsageMeta)
}

func applyFlatUsage(out *TokenMetrics, u *flatUsage) {
	if u == nil {
		return
	}
	out.Input = max(out.Input, u.InputTokens, u.PromptTokens)
	out.Output = max(out.Output, u.OutputTokens, u.CompletionTokens)
	out.CacheRead = max(out.CacheRead, u.CacheReadInputTokens)
	out.CacheWrite = max(out.CacheWrite, u.CacheCreationInputTokens)
	out.Total = max(out.Total, u.TotalTokens)
	if u.InputDetails != nil {
		out.CacheRead = max(out.CacheRead, u.InputDetails.CachedTokens)
	}
	if u.PromptDetails != nil {
		out.CacheRead = max(out.CacheRead, u.PromptDetails.CachedTokens)
	}
	if u.OutputDetails != nil {
		out.Reasoning = max(out.Reasoning, u.OutputDetails.ReasoningTokens)
	}
	if u.CompletionDetails != nil {
		out.Reasoning = max(out.Reasoning, u.CompletionDetails.ReasoningTokens)
	}
}

func applyGeminiUsage(out *TokenMetrics, g *geminiUsage) {
	if g == nil {
		return
	}
	out.Input = max(out.Input, g.PromptTokenCount)
	out.Output = max(out.Output, g.CandidatesTokenCount)
	out.CacheRead = max(out.CacheRead, g.CachedContentTokenCount)
	out.Reasoning = max(out.Reasoning, g.ThoughtsTokenCount)
	out.Total = max(out.Total, g.TotalTokenCount)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// extractHeaderQuota reads quota windows and plan metadata from upstream
// Anthropic, Codex, and OpenAI response headers.
func extractHeaderQuota(now time.Time, hdr http.Header) (plan string, windows []QuotaWindow) {
	if len(hdr) == 0 {
		return "", nil
	}
	plan = strings.TrimSpace(hdr.Get("X-Codex-Plan-Type"))
	windows = append(windows, parseAnthropicUnifiedHeaders(now, hdr)...)
	windows = append(windows, parseAnthropicRateHeaders(now, hdr)...)
	windows = append(windows, parseCodexHeaders(now, hdr)...)
	windows = append(windows, parseOpenAIRateHeaders(now, hdr)...)
	return plan, windows
}

const (
	labelWindow5h  = "5-hour window"
	labelWindow24h = "24-hour window"
	labelWindow7d  = "7-day window"
)

func parseAnthropicUnifiedHeaders(now time.Time, hdr http.Header) []QuotaWindow {
	specs := []struct {
		key, name, label string
		minutes          int
	}{
		{"5h", "5h", labelWindow5h, 300},
		{"7d", "7d", labelWindow7d, 10080},
		{"7d_sonnet", "7d_sonnet", "7-day Sonnet", 10080},
		{"7d_opus", "7d_opus", "7-day Opus", 10080},
	}
	var out []QuotaWindow
	for _, sp := range specs {
		prefix := "Anthropic-Ratelimit-Unified-" + sp.key
		utilStr := strings.TrimSpace(hdr.Get(prefix + "-Utilization"))
		resetStr := strings.TrimSpace(hdr.Get(prefix + "-Reset"))
		status := strings.TrimSpace(hdr.Get(prefix + "-Status"))
		if utilStr == "" && resetStr == "" && status == "" {
			continue
		}
		pct := parseUtilizationPercent(utilStr)
		out = append(out, QuotaWindow{
			Name:          sp.name,
			Label:         sp.label,
			UsedPercent:   pct,
			ResetsAt:      parseResetTime(now, resetStr),
			WindowMinutes: sp.minutes,
			Status:        status,
		})
	}
	return out
}

type rateLimitSpec struct {
	name, label  string
	limitKey     string
	remainingKey string
	resetKey     string
}

func parseAnthropicRateHeaders(now time.Time, hdr http.Header) []QuotaWindow {
	specs := []rateLimitSpec{
		{name: "rpm", label: "Requests / min", limitKey: "Anthropic-Ratelimit-Requests-Limit", remainingKey: "Anthropic-Ratelimit-Requests-Remaining", resetKey: "Anthropic-Ratelimit-Requests-Reset"},
		{name: "tpm", label: "Tokens / min", limitKey: "Anthropic-Ratelimit-Tokens-Limit", remainingKey: "Anthropic-Ratelimit-Tokens-Remaining", resetKey: "Anthropic-Ratelimit-Tokens-Reset"},
		{name: "itpm", label: "Input tokens / min", limitKey: "Anthropic-Ratelimit-Input-Tokens-Limit", remainingKey: "Anthropic-Ratelimit-Input-Tokens-Remaining", resetKey: "Anthropic-Ratelimit-Input-Tokens-Reset"},
		{name: "otpm", label: "Output tokens / min", limitKey: "Anthropic-Ratelimit-Output-Tokens-Limit", remainingKey: "Anthropic-Ratelimit-Output-Tokens-Remaining", resetKey: "Anthropic-Ratelimit-Output-Tokens-Reset"},
	}
	return parseRateLimitSpecs(now, hdr, specs)
}

type codexHeaderSpec struct {
	prefix, name, label string
	defaultMin          int
}

func parseCodexHeaders(now time.Time, hdr http.Header) []QuotaWindow {
	specs := []codexHeaderSpec{
		{"X-Codex-Primary", "primary", labelWindow5h, 300},
		{"X-Codex-Secondary", "secondary", labelWindow7d, 10080},
	}
	var out []QuotaWindow
	for _, sp := range specs {
		if w, ok := parseCodexWindow(now, hdr, sp); ok {
			out = append(out, w)
		}
	}
	return out
}

func parseCodexWindow(now time.Time, hdr http.Header, sp codexHeaderSpec) (QuotaWindow, bool) {
	usedStr := strings.TrimSpace(hdr.Get(sp.prefix + "-Used-Percent"))
	resetAt := strings.TrimSpace(hdr.Get(sp.prefix + "-Reset-At"))
	resetAfter := strings.TrimSpace(hdr.Get(sp.prefix + "-Reset-After-Seconds"))
	if usedStr == "" && resetAt == "" && resetAfter == "" {
		return QuotaWindow{}, false
	}
	mins := sp.defaultMin
	if v, err := strconv.Atoi(strings.TrimSpace(hdr.Get(sp.prefix + "-Window-Minutes"))); err == nil && v > 0 {
		mins = v
	}
	reset := parseResetTime(now, resetAt)
	if reset.IsZero() {
		if secs, err := strconv.ParseInt(resetAfter, 10, 64); err == nil && secs > 0 {
			reset = now.Add(time.Duration(secs) * time.Second).UTC()
		}
	}
	var pct float64
	if v, err := strconv.ParseFloat(usedStr, 64); err == nil {
		pct = v
	}
	return QuotaWindow{
		Name:          windowNameForMinutes(sp.name, mins),
		Label:         windowLabelForMinutes(sp.label, mins),
		UsedPercent:   clampPercent(pct),
		ResetsAt:      reset,
		WindowMinutes: mins,
	}, true
}

func parseOpenAIRateHeaders(now time.Time, hdr http.Header) []QuotaWindow {
	specs := []rateLimitSpec{
		{name: "rpm", label: "Requests / min", limitKey: "X-Ratelimit-Limit-Requests", remainingKey: "X-Ratelimit-Remaining-Requests", resetKey: "X-Ratelimit-Reset-Requests"},
		{name: "tpm", label: "Tokens / min", limitKey: "X-Ratelimit-Limit-Tokens", remainingKey: "X-Ratelimit-Remaining-Tokens", resetKey: "X-Ratelimit-Reset-Tokens"},
	}
	return parseRateLimitSpecs(now, hdr, specs)
}

func parseRateLimitSpecs(now time.Time, hdr http.Header, specs []rateLimitSpec) []QuotaWindow {
	var out []QuotaWindow
	for _, sp := range specs {
		if w, ok := parseLimitRemainingWindow(now, hdr, sp); ok {
			out = append(out, w)
		}
	}
	return out
}

func parseLimitRemainingWindow(now time.Time, hdr http.Header, sp rateLimitSpec) (QuotaWindow, bool) {
	limit, errL := strconv.ParseInt(strings.TrimSpace(hdr.Get(sp.limitKey)), 10, 64)
	rem, errR := strconv.ParseInt(strings.TrimSpace(hdr.Get(sp.remainingKey)), 10, 64)
	if errL != nil || errR != nil || limit <= 0 {
		return QuotaWindow{}, false
	}
	used := max(int64(0), limit-rem)
	return QuotaWindow{
		Name:        sp.name,
		Label:       sp.label,
		UsedPercent: clampPercent(float64(used) * 100 / float64(limit)),
		Limit:       limit,
		Used:        used,
		Remaining:   max(int64(0), rem),
		ResetsAt:    parseResetTime(now, hdr.Get(sp.resetKey)),
	}, true
}

func parseUtilizationPercent(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	if v <= 1.0 && v >= 0 {
		return clampPercent(v * 100)
	}
	return clampPercent(v)
}

func parseResetTime(now time.Time, raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if epoch, err := strconv.ParseInt(raw, 10, 64); err == nil && epoch > 1_000_000_000 {
		return time.Unix(epoch, 0).UTC()
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return now.Add(d).UTC()
	}
	return time.Time{}
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return math.Round(v*10) / 10
}

func windowNameForMinutes(fallback string, mins int) string {
	switch mins {
	case 300:
		return "5h"
	case 1440:
		return "24h"
	case 10080:
		return "7d"
	default:
		return fallback
	}
}

func windowLabelForMinutes(fallback string, mins int) string {
	switch mins {
	case 300:
		return labelWindow5h
	case 1440:
		return labelWindow24h
	case 10080:
		return labelWindow7d
	default:
		return fallback
	}
}
