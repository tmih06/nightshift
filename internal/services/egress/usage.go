package egress

import (
	"math"
	"sort"
	"sync"
	"time"
)

const (
	usageBucket  = 10 * time.Minute
	usageBuckets = 144 // 24 hours
)

// ModelUsage holds request and token counts for one model.
type ModelUsage struct {
	Requests   int   `json:"requests"`
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
	Total      int64 `json:"total"`
}

// AccountUsage counts requests, latency, and token metrics that carried one
// secret for one account. Counts live in memory and start over on restart.
type AccountUsage struct {
	Binding         string                `json:"binding"` // the secret binding's name
	Label           string                `json:"label"`
	Requests        int                   `json:"requests"`
	OK              int                   `json:"ok"`
	ClientError     int                   `json:"client_error"`
	RateLimited     int                   `json:"rate_limited"`
	ServerError     int                   `json:"server_error"`
	InputTokens     int64                 `json:"input_tokens,omitempty"`
	OutputTokens    int64                 `json:"output_tokens,omitempty"`
	CacheRead       int64                 `json:"cache_read_tokens,omitempty"`
	CacheWrite      int64                 `json:"cache_write_tokens,omitempty"`
	Reasoning       int64                 `json:"reasoning_tokens,omitempty"`
	TotalTokens     int64                 `json:"total_tokens,omitempty"`
	CacheHitPercent float64               `json:"cache_hit_percent,omitempty"`
	AvgLatencyMS    int64                 `json:"avg_latency_ms,omitempty"`
	LastRequest     time.Time             `json:"last_request"`
	ByModel         map[string]ModelUsage `json:"by_model,omitempty"`
	// Recent holds the request count of each ten-minute window, oldest first,
	// ending with the window now in progress.
	Recent []int `json:"recent"`
	// RecentTokens holds total tokens per ten-minute window over the last day.
	RecentTokens []int64 `json:"recent_tokens,omitempty"`
}

// AnalyticsSnapshot aggregates fleet-wide requests, tokens, latency, models,
// and live quotas across all accounts.
type AnalyticsSnapshot struct {
	GeneratedAt     time.Time             `json:"generated_at"`
	Requests        int                   `json:"requests"`
	OK              int                   `json:"ok"`
	ClientError     int                   `json:"client_error"`
	RateLimited     int                   `json:"rate_limited"`
	ServerError     int                   `json:"server_error"`
	InputTokens     int64                 `json:"input_tokens"`
	OutputTokens    int64                 `json:"output_tokens"`
	CacheRead       int64                 `json:"cache_read_tokens"`
	CacheWrite      int64                 `json:"cache_write_tokens"`
	Reasoning       int64                 `json:"reasoning_tokens"`
	TotalTokens     int64                 `json:"total_tokens"`
	CacheHitPercent float64               `json:"cache_hit_percent"`
	AvgLatencyMS    int64                 `json:"avg_latency_ms"`
	ByModel         map[string]ModelUsage `json:"by_model,omitempty"`
	Accounts        []AccountUsage        `json:"accounts"`
	Quotas          []AccountQuota        `json:"quotas,omitempty"`
}

type usageKey struct{ secret, label string }

type usageSeries struct {
	AccountUsage
	totalLatencyMS int64
	windows        map[int64]int   // window start (unix / bucket) -> requests
	tokenWindows   map[int64]int64 // window start (unix / bucket) -> total tokens
}

type usageStore struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[usageKey]*usageSeries
	// days is the daily history, kept apart from m because it can be saved
	// and restored across restarts.
	days    map[usageKey]map[string]DayUsage
	unsaved bool
}

func newUsageStore() *usageStore {
	return &usageStore{now: time.Now, m: map[usageKey]*usageSeries{}, days: map[usageKey]map[string]DayUsage{}}
}

func (u *usageStore) window(t time.Time) int64 { return t.Unix() / int64(usageBucket.Seconds()) }

// record counts one request that carried secret, tagged with its account label.
func (u *usageStore) record(secret, label string, status int) {
	u.recordDetailed(secret, label, status, 0, TokenMetrics{})
}

// recordDetailed counts one request along with latency and token telemetry.
func (u *usageStore) recordDetailed(secret, label string, status int, latencyMS int64, tok TokenMetrics) {
	u.mu.Lock()
	defer u.mu.Unlock()
	k := usageKey{secret, label}
	s := u.m[k]
	if s == nil {
		s = &usageSeries{
			AccountUsage: AccountUsage{Binding: secret, Label: label},
			windows:      map[int64]int{},
			tokenWindows: map[int64]int64{},
		}
		u.m[k] = s
	}
	now := u.now()
	u.addDay(k, now, tok.Total)
	s.Requests++
	s.LastRequest = now.UTC()
	if latencyMS > 0 {
		s.totalLatencyMS += latencyMS
	}
	recordStatus(&s.AccountUsage, status)
	recordTokens(s, tok)
	w := u.window(now)
	s.windows[w]++
	if tok.Total > 0 {
		s.tokenWindows[w] += tok.Total
	}
	pruneOldWindows(s, w)
}

func recordStatus(a *AccountUsage, status int) {
	switch {
	case status == 429:
		a.RateLimited++
	case status >= 500:
		a.ServerError++
	case status >= 400:
		a.ClientError++
	default:
		a.OK++
	}
}

func recordTokens(s *usageSeries, tok TokenMetrics) {
	if tok.empty() && tok.Model == "" {
		return
	}
	s.InputTokens += tok.Input
	s.OutputTokens += tok.Output
	s.CacheRead += tok.CacheRead
	s.CacheWrite += tok.CacheWrite
	s.Reasoning += tok.Reasoning
	s.TotalTokens += tok.Total
	if tok.Model != "" {
		if s.ByModel == nil {
			s.ByModel = map[string]ModelUsage{}
		}
		m := s.ByModel[tok.Model]
		m.Requests++
		m.Input += tok.Input
		m.Output += tok.Output
		m.CacheRead += tok.CacheRead
		m.CacheWrite += tok.CacheWrite
		m.Reasoning += tok.Reasoning
		m.Total += tok.Total
		s.ByModel[tok.Model] = m
	}
}

func pruneOldWindows(s *usageSeries, w int64) {
	cutoff := w - usageBuckets
	for old := range s.windows {
		if old <= cutoff {
			delete(s.windows, old)
		}
	}
	for old := range s.tokenWindows {
		if old <= cutoff {
			delete(s.tokenWindows, old)
		}
	}
}

func cacheHitPercent(input, cacheRead, cacheWrite int64) float64 {
	denom := input + cacheRead + cacheWrite
	if denom <= 0 || cacheRead <= 0 {
		return 0
	}
	return math.Round(float64(cacheRead)*1000/float64(denom)) / 10
}

// snapshot returns every series, most recently used first.
func (u *usageStore) snapshot() []AccountUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.window(u.now())
	out := make([]AccountUsage, 0, len(u.m))
	for _, s := range u.m {
		out = append(out, buildUsageSnapshot(s, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastRequest.After(out[j].LastRequest) })
	return out
}

func buildUsageSnapshot(s *usageSeries, nowWindow int64) AccountUsage {
	cp := s.AccountUsage
	cp.CacheHitPercent = cacheHitPercent(cp.InputTokens, cp.CacheRead, cp.CacheWrite)
	if cp.Requests > 0 && s.totalLatencyMS > 0 {
		cp.AvgLatencyMS = s.totalLatencyMS / int64(cp.Requests)
	}
	if len(s.ByModel) > 0 {
		cp.ByModel = make(map[string]ModelUsage, len(s.ByModel))
		for k, v := range s.ByModel {
			cp.ByModel[k] = v
		}
	}
	cp.Recent = make([]int, usageBuckets)
	hasTok := false
	toks := make([]int64, usageBuckets)
	for i := range cp.Recent {
		w := nowWindow - int64(usageBuckets-1-i)
		cp.Recent[i] = s.windows[w]
		if v := s.tokenWindows[w]; v > 0 {
			toks[i] = v
			hasTok = true
		}
	}
	if hasTok {
		cp.RecentTokens = toks
	}
	return cp
}

// Usage reports, per secret and account, how many requests and tokens carried it.
func (p *Proxy) Usage() []AccountUsage { return p.usage.snapshot() }

// Analytics reports fleet-wide request, token, model, and quota analytics.
func (p *Proxy) Analytics() AnalyticsSnapshot {
	accounts := p.usage.snapshot()
	quotas := p.Quota()
	out := AnalyticsSnapshot{
		GeneratedAt: p.pool.now().UTC(),
		Accounts:    accounts,
		Quotas:      quotas,
	}
	var latSum int64
	var latCount int
	for i := range accounts {
		a := &accounts[i]
		out.Requests += a.Requests
		out.OK += a.OK
		out.ClientError += a.ClientError
		out.RateLimited += a.RateLimited
		out.ServerError += a.ServerError
		out.InputTokens += a.InputTokens
		out.OutputTokens += a.OutputTokens
		out.CacheRead += a.CacheRead
		out.CacheWrite += a.CacheWrite
		out.Reasoning += a.Reasoning
		out.TotalTokens += a.TotalTokens
		if a.AvgLatencyMS > 0 && a.Requests > 0 {
			latSum += a.AvgLatencyMS * int64(a.Requests)
			latCount += a.Requests
		}
		out.ByModel = mergeModelUsage(out.ByModel, a.ByModel)
	}
	out.CacheHitPercent = cacheHitPercent(out.InputTokens, out.CacheRead, out.CacheWrite)
	if latCount > 0 {
		out.AvgLatencyMS = latSum / int64(latCount)
	}
	return out
}

func mergeModelUsage(dst, src map[string]ModelUsage) map[string]ModelUsage {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]ModelUsage, len(src))
	}
	for model, u := range src {
		cur := dst[model]
		cur.Requests += u.Requests
		cur.Input += u.Input
		cur.Output += u.Output
		cur.CacheRead += u.CacheRead
		cur.CacheWrite += u.CacheWrite
		cur.Reasoning += u.Reasoning
		cur.Total += u.Total
		dst[model] = cur
	}
	return dst
}
