package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

type wireModelUsage struct {
	Requests   int   `json:"requests"`
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

// wireUsage is one entry of the egress proxy's /usage.
type wireUsage struct {
	Binding         string                    `json:"binding"`
	Label           string                    `json:"label"`
	Requests        int                       `json:"requests"`
	OK              int                       `json:"ok"`
	ClientError     int                       `json:"client_error"`
	RateLimited     int                       `json:"rate_limited"`
	ServerError     int                       `json:"server_error"`
	InputTokens     int64                     `json:"input_tokens"`
	OutputTokens    int64                     `json:"output_tokens"`
	CacheRead       int64                     `json:"cache_read_tokens"`
	CacheWrite      int64                     `json:"cache_write_tokens"`
	Reasoning       int64                     `json:"reasoning_tokens"`
	TotalTokens     int64                     `json:"total_tokens"`
	CacheHitPercent float64                   `json:"cache_hit_percent"`
	AvgLatencyMS    int64                     `json:"avg_latency_ms"`
	LastRequest     time.Time                 `json:"last_request"`
	ByModel         map[string]wireModelUsage `json:"by_model"`
	Recent          []int                     `json:"recent"`
	RecentTokens    []int64                   `json:"recent_tokens"`
}

func (w wireUsage) usage() *Usage {
	out := &Usage{
		Requests:        w.Requests,
		OK:              w.OK,
		ClientError:     w.ClientError,
		RateLimited:     w.RateLimited,
		ServerError:     w.ServerError,
		Input:           w.InputTokens,
		Output:          w.OutputTokens,
		CacheRead:       w.CacheRead,
		CacheWrite:      w.CacheWrite,
		Reasoning:       w.Reasoning,
		Total:           w.TotalTokens,
		CacheHitPercent: w.CacheHitPercent,
		AvgLatencyMS:    w.AvgLatencyMS,
		Last:            w.LastRequest,
		Recent:          w.Recent,
		RecentVolume:    w.RecentTokens,
	}
	if len(w.ByModel) > 0 {
		out.ByModel = make(map[string]ModelUsage, len(w.ByModel))
		for k, v := range w.ByModel {
			out.ByModel[k] = ModelUsage(v)
		}
	}
	return out
}

// wirePoolStatus is one entry of the egress proxy's /pools.
type wirePoolStatus struct {
	Binding        string    `json:"binding"`
	Name           string    `json:"name"`
	Label          string    `json:"label"`
	Priority       int       `json:"priority"`
	ActiveNow      bool      `json:"active_now"`
	CooldownUntil  time.Time `json:"cooldown_until"`
	CooldownReason string    `json:"cooldown_reason"`
	Failovers      int       `json:"failovers"`
}

type wireDay struct {
	Date     string `json:"date"`
	Requests int    `json:"requests"`
	Total    int64  `json:"total"`
}

// wireHistory is one entry of the egress proxy's /history.
type wireHistory struct {
	Binding string    `json:"binding"`
	Label   string    `json:"label"`
	Days    []wireDay `json:"days"`
}

// applyAccountHistory gives acct the daily usage recorded under its label.
func applyAccountHistory(acct *Account, p Provider, history []wireHistory) {
	lbl := acct.Label()
	for i := range history {
		if history[i].Binding != p.Secret || history[i].Label != lbl {
			continue
		}
		acct.History = make([]DayUsage, len(history[i].Days))
		for j, d := range history[i].Days {
			acct.History[j] = DayUsage(d)
		}
	}
}

type wireQuotaWindow struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	Group         string    `json:"group"`
	UsedPercent   float64   `json:"used_percent"`
	Limit         int64     `json:"limit"`
	Used          int64     `json:"used"`
	Remaining     int64     `json:"remaining"`
	ResetsAt      time.Time `json:"resets_at"`
	WindowMinutes int       `json:"window_minutes"`
	Status        string    `json:"status"`
}

type wireQuota struct {
	Binding        string            `json:"binding"`
	Name           string            `json:"name"`
	Label          string            `json:"label"`
	Plan           string            `json:"plan"`
	CreditsBalance string            `json:"credits_balance"`
	Source         string            `json:"source"`
	FetchedAt      time.Time         `json:"fetched_at"`
	NextResetAt    time.Time         `json:"next_reset_at"`
	Windows        []wireQuotaWindow `json:"windows"`
	Error          string            `json:"error"`
	Models         []wireModel       `json:"models"`
	ModelsError    string            `json:"models_error"`
}

type wireModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (w wireQuota) quota() *Quota {
	out := &Quota{
		Plan:           w.Plan,
		CreditsBalance: w.CreditsBalance,
		Source:         w.Source,
		FetchedAt:      w.FetchedAt,
		NextResetAt:    w.NextResetAt,
		Error:          w.Error,
		ModelsError:    w.ModelsError,
	}
	for _, m := range w.Models {
		out.Models = append(out.Models, Model(m))
	}
	if len(w.Windows) > 0 {
		out.Windows = make([]QuotaWindow, len(w.Windows))
		for i, win := range w.Windows {
			out.Windows[i] = QuotaWindow(win)
		}
	}
	return out
}

func proxyGetJSON[T any](ctx context.Context, base, path, what string, allowNotFound bool) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, http.NoBody)
	if err != nil {
		return zero, fmt.Errorf("build %s request: %w", what, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, fmt.Errorf("read proxy %s: %w", what, err)
	}
	defer closeBody(resp.Body)
	if allowNotFound && resp.StatusCode == http.StatusNotFound {
		return zero, nil
	}
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("read proxy %s: HTTP %d", what, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return zero, fmt.Errorf("read proxy %s: %w", what, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, fmt.Errorf("parse proxy %s: %w", what, err)
	}
	return out, nil
}

// fetchUsage reads the egress proxy's per-account request and token counts.
func fetchUsage(ctx context.Context, base string) ([]wireUsage, error) {
	return proxyGetJSON[[]wireUsage](ctx, base, "/usage", "usage", false)
}

// fetchPools reads the egress proxy's live fallback-chain and cooldown state.
func fetchPools(ctx context.Context, base string) ([]wirePoolStatus, error) {
	return proxyGetJSON[[]wirePoolStatus](ctx, base, "/pools", "pools", true)
}

// fetchQuotas reads the egress proxy's live per-account quota windows.
func fetchQuotas(ctx context.Context, base string) ([]wireQuota, error) {
	return proxyGetJSON[[]wireQuota](ctx, base, "/quota", "quota", true)
}

// fetchHistory reads the egress proxy's daily usage. An older proxy has none.
func fetchHistory(ctx context.Context, base string) ([]wireHistory, error) {
	return proxyGetJSON[[]wireHistory](ctx, base, "/history", "history", true)
}

// triggerQuotaRefresh asks the egress proxy to actively fetch provider quotas now.
func triggerQuotaRefresh(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/quota/refresh", http.NoBody)
	if err != nil {
		return fmt.Errorf("build quota refresh request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh proxy quotas: %w", err)
	}
	defer closeBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh proxy quotas: HTTP %d", resp.StatusCode)
	}
	return nil
}

func summarizeAnalytics(usage []wireUsage) *Analytics {
	if len(usage) == 0 {
		return nil
	}
	out := &Analytics{}
	var latSum int64
	var latCount int
	for i := range usage {
		u := &usage[i]
		out.Requests += u.Requests
		out.OK += u.OK
		out.ClientError += u.ClientError
		out.RateLimited += u.RateLimited
		out.ServerError += u.ServerError
		out.Input += u.InputTokens
		out.Output += u.OutputTokens
		out.CacheRead += u.CacheRead
		out.CacheWrite += u.CacheWrite
		out.Reasoning += u.Reasoning
		out.Total += u.TotalTokens
		if u.AvgLatencyMS > 0 && u.Requests > 0 {
			latSum += u.AvgLatencyMS * int64(u.Requests)
			latCount += u.Requests
		}
		out.ByModel = mergeWireModels(out.ByModel, u.ByModel)
	}
	if denom := out.Input + out.CacheRead + out.CacheWrite; denom > 0 && out.CacheRead > 0 {
		out.CacheHitPercent = math.Round(float64(out.CacheRead)*1000/float64(denom)) / 10
	}
	if latCount > 0 {
		out.AvgLatencyMS = latSum / int64(latCount)
	}
	return out
}

func mergeWireModels(dst map[string]ModelUsage, src map[string]wireModelUsage) map[string]ModelUsage {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]ModelUsage, len(src))
	}
	for k, v := range src {
		cur := dst[k]
		cur.Requests += v.Requests
		cur.Input += v.Input
		cur.Output += v.Output
		cur.CacheRead += v.CacheRead
		cur.CacheWrite += v.CacheWrite
		cur.Reasoning += v.Reasoning
		cur.Total += v.Total
		dst[k] = cur
	}
	return dst
}
