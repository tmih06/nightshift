package egress

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	claudeUsageURL      = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL       = "https://chatgpt.com/backend-api/wham/usage"
	antigravityLoadURL  = "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	antigravityUsageURL = "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary"
	quotaFetchTimeout   = 15 * time.Second
	quotaMaxConcurrent  = 4
)

// antigravityUA is the User-Agent agy 1.5.18 sends on this architecture.
func antigravityUA() string {
	return "antigravity/cli/1.5.18 (aidev_client; os_type=" + runtime.GOOS + "; arch=" + runtime.GOARCH + "; cl=1030252320; auth_method=consumer)"
}

// QuotaWindow is one normalized provider quota window (5h, 7d, RPM/TPM, or model).
type QuotaWindow struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	Group         string    `json:"group,omitempty"`
	UsedPercent   float64   `json:"used_percent"`
	Limit         int64     `json:"limit,omitempty"`
	Used          int64     `json:"used,omitempty"`
	Remaining     int64     `json:"remaining,omitempty"`
	ResetsAt      time.Time `json:"resets_at,omitempty"`
	WindowMinutes int       `json:"window_minutes,omitempty"`
	Status        string    `json:"status,omitempty"`
}

// AccountQuota holds the latest fetched or header-observed quota for one account.
type AccountQuota struct {
	Binding        string        `json:"binding"`
	Name           string        `json:"name"`
	Label          string        `json:"label"`
	Plan           string        `json:"plan,omitempty"`
	CreditsBalance string        `json:"credits_balance,omitempty"`
	Source         string        `json:"source,omitempty"` // "fetch" or "header"
	FetchedAt      time.Time     `json:"fetched_at,omitempty"`
	NextResetAt    time.Time     `json:"next_reset_at,omitempty"`
	Windows        []QuotaWindow `json:"windows,omitempty"`
	Error          string        `json:"error,omitempty"`
}

// QuotaEndpoints overrides provider quota URLs (used in tests).
type QuotaEndpoints struct {
	Claude          string
	Codex           string
	Antigravity     string
	AntigravityLoad string
}

func (q QuotaEndpoints) withDefaults() QuotaEndpoints {
	if q.Claude == "" {
		q.Claude = claudeUsageURL
	}
	if q.Codex == "" {
		q.Codex = codexUsageURL
	}
	if q.Antigravity == "" {
		q.Antigravity = antigravityUsageURL
	}
	if q.AntigravityLoad == "" {
		q.AntigravityLoad = antigravityLoadURL
	}
	return q
}

type quotaStore struct {
	mu        sync.Mutex
	now       func() time.Time
	client    *http.Client
	endpoints QuotaEndpoints
	records   map[poolKey]*AccountQuota
}

func newQuotaStore(now func() time.Time, tr http.RoundTripper, ep QuotaEndpoints) *quotaStore {
	if now == nil {
		now = time.Now
	}
	return &quotaStore{
		now:       now,
		client:    &http.Client{Transport: tr, Timeout: quotaFetchTimeout},
		endpoints: ep.withDefaults(),
		records:   map[poolKey]*AccountQuota{},
	}
}

// observeHeaders merges passive rate-limit/quota headers from a real upstream
// response into the account's quota record.
func (qs *quotaStore) observeHeaders(binding, accountName, label string, hdr http.Header) {
	now := qs.now().UTC()
	plan, windows := extractHeaderQuota(now, hdr)
	if plan == "" && len(windows) == 0 {
		return
	}
	qs.mu.Lock()
	defer qs.mu.Unlock()
	k := poolKey{secret: binding, account: accountName}
	rec := qs.records[k]
	if rec == nil {
		rec = &AccountQuota{Binding: binding, Name: accountName, Label: label}
		qs.records[k] = rec
	}
	if label != "" {
		rec.Label = label
	}
	if plan != "" {
		rec.Plan = plan
	}
	rec.Windows = mergeQuotaWindows(rec.Windows, windows)
	rec.NextResetAt = earliestFutureReset(now, rec.Windows)
	if rec.Source == "" {
		rec.Source = "header"
	}
	if rec.FetchedAt.IsZero() {
		rec.FetchedAt = now
	}
}

func (qs *quotaStore) putFetched(res AccountQuota) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	now := qs.now().UTC()
	k := poolKey{secret: res.Binding, account: res.Name}
	prev := qs.records[k]
	if res.Error != "" && prev != nil {
		prev.Error = res.Error
		prev.FetchedAt = now
		return
	}
	if prev != nil {
		res.Windows = mergeQuotaWindows(prev.Windows, res.Windows)
		if res.Plan == "" {
			res.Plan = prev.Plan
		}
		if res.CreditsBalance == "" {
			res.CreditsBalance = prev.CreditsBalance
		}
	}
	res.FetchedAt = now
	res.NextResetAt = earliestFutureReset(now, res.Windows)
	cp := res
	qs.records[k] = &cp
}

func (qs *quotaStore) snapshot(eng *SecretEngine) []AccountQuota {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	now := qs.now().UTC()
	out := make([]AccountQuota, 0, len(qs.records))
	seen := map[poolKey]bool{}
	if eng != nil {
		for _, rs := range eng.all {
			for _, acct := range rs.accounts {
				k := poolKey{secret: rs.name, account: acct.name}
				if rec := qs.records[k]; rec != nil {
					seen[k] = true
					out = append(out, copyQuota(rec, now))
				}
			}
		}
	}
	for k, rec := range qs.records {
		if !seen[k] {
			out = append(out, copyQuota(rec, now))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Binding != out[j].Binding {
			return out[i].Binding < out[j].Binding
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func copyQuota(rec *AccountQuota, now time.Time) AccountQuota {
	cp := *rec
	cp.Windows = append([]QuotaWindow(nil), rec.Windows...)
	cp.NextResetAt = earliestFutureReset(now, cp.Windows)
	return cp
}

// refreshAll actively fetches provider quota for every supported OAuth account
// currently loaded in eng, with bounded concurrency.
func (qs *quotaStore) refreshAll(ctx context.Context, eng *SecretEngine) []AccountQuota {
	if eng == nil {
		return nil
	}
	type job struct {
		binding string
		acct    resolvedAccount
	}
	var jobs []job
	for _, rs := range eng.all {
		if !supportsQuotaFetch(rs.name) {
			continue
		}
		for _, acct := range rs.accounts {
			jobs = append(jobs, job{binding: rs.name, acct: acct})
		}
	}
	sem := make(chan struct{}, quotaMaxConcurrent)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			qs.putFetched(qs.fetchOne(ctx, j.binding, j.acct))
		}(j)
	}
	wg.Wait()
	return qs.snapshot(eng)
}

const (
	bindingClaude = "CLAUDE_ACCESS_TOKEN"
	bindingCodex  = "CODEX_ACCESS_TOKEN"
	bindingAgy    = "AGY_ACCESS_TOKEN"
)

func supportsQuotaFetch(binding string) bool {
	switch binding {
	case bindingClaude, bindingCodex, bindingAgy:
		return true
	default:
		return false
	}
}

func (qs *quotaStore) fetchOne(ctx context.Context, binding string, acct resolvedAccount) AccountQuota {
	base := AccountQuota{
		Binding: binding,
		Name:    acct.name,
		Label:   acct.label,
		Source:  "fetch",
	}
	var err error
	switch binding {
	case bindingClaude:
		err = qs.fetchClaude(ctx, acct, &base)
	case bindingCodex:
		err = qs.fetchCodex(ctx, acct, &base)
	case bindingAgy:
		err = qs.fetchAntigravity(ctx, acct, &base)
	default:
		return base
	}
	if err != nil {
		base.Error = err.Error()
	}
	return base
}

func (qs *quotaStore) fetchClaude(ctx context.Context, acct resolvedAccount, out *AccountQuota) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qs.endpoints.Claude, http.NoBody)
	if err != nil {
		return fmt.Errorf("build claude quota request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+acct.value)
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	raw, err := qs.doJSON(req)
	if err != nil {
		return err
	}
	return parseClaudeQuotaJSON(raw, out)
}

type claudeUsageResp struct {
	FiveHour       *claudeWindow `json:"five_hour"`
	SevenDay       *claudeWindow `json:"seven_day"`
	SevenDaySonnet *claudeWindow `json:"seven_day_sonnet"`
	SevenDayOpus   *claudeWindow `json:"seven_day_opus"`
	ExtraUsage     *struct {
		IsEnabled    bool    `json:"is_enabled"`
		MonthlyLimit float64 `json:"monthly_limit"`
		UsedCredits  float64 `json:"used_credits"`
		Utilization  float64 `json:"utilization"`
	} `json:"extra_usage"`
}

type claudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func parseClaudeQuotaJSON(raw []byte, out *AccountQuota) error {
	var body claudeUsageResp
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("parse claude quota: %w", err)
	}
	addWin := func(name, label string, mins int, w *claudeWindow) {
		if w == nil {
			return
		}
		out.Windows = append(out.Windows, QuotaWindow{
			Name:          name,
			Label:         label,
			UsedPercent:   normalizePercent(w.Utilization),
			ResetsAt:      parseResetTime(time.Now().UTC(), w.ResetsAt),
			WindowMinutes: mins,
		})
	}
	addWin("5h", labelWindow5h, 300, body.FiveHour)
	addWin("7d", labelWindow7d, 10080, body.SevenDay)
	addWin("7d_sonnet", "7-day Sonnet", 10080, body.SevenDaySonnet)
	addWin("7d_opus", "7-day Opus", 10080, body.SevenDayOpus)
	if body.ExtraUsage != nil && body.ExtraUsage.IsEnabled {
		out.CreditsBalance = fmt.Sprintf("%.0f/%.0f", body.ExtraUsage.UsedCredits, body.ExtraUsage.MonthlyLimit)
	}
	return nil
}

func (qs *quotaStore) fetchCodex(ctx context.Context, acct resolvedAccount, out *AccountQuota) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qs.endpoints.Codex, http.NoBody)
	if err != nil {
		return fmt.Errorf("build codex quota request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+acct.value)
	if id := acct.companions[DefaultPlaceholderPrefix+"CODEX_ACCOUNT_ID"]; id != "" {
		req.Header.Set("ChatGPT-Account-Id", id)
	}
	raw, err := qs.doJSON(req)
	if err != nil {
		return err
	}
	return parseCodexQuotaJSON(qs.now().UTC(), raw, out)
}

type codexUsageResp struct {
	PlanType  string `json:"plan_type"`
	RateLimit *struct {
		Allowed         bool         `json:"allowed"`
		LimitReached    bool         `json:"limit_reached"`
		PrimaryWindow   *codexWindow `json:"primary_window"`
		SecondaryWindow *codexWindow `json:"secondary_window"`
	} `json:"rate_limit"`
	Credits *struct {
		HasCredits bool   `json:"has_credits"`
		Unlimited  bool   `json:"unlimited"`
		Balance    string `json:"balance"`
	} `json:"credits"`
}

type codexWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

func parseCodexQuotaJSON(now time.Time, raw []byte, out *AccountQuota) error {
	var body codexUsageResp
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("parse codex quota: %w", err)
	}
	out.Plan = body.PlanType
	if body.Credits != nil && body.Credits.HasCredits {
		if body.Credits.Unlimited {
			out.CreditsBalance = "unlimited"
		} else if body.Credits.Balance != "" {
			out.CreditsBalance = body.Credits.Balance
		}
	}
	if body.RateLimit == nil {
		return nil
	}
	status := "allowed"
	if body.RateLimit.LimitReached || !body.RateLimit.Allowed {
		status = "rejected"
	}
	addWin := func(fallbackName, fallbackLabel string, w *codexWindow) {
		if w == nil {
			return
		}
		mins := w.LimitWindowSeconds / 60
		var reset time.Time
		if w.ResetAt > 0 {
			reset = time.Unix(w.ResetAt, 0).UTC()
		} else if w.ResetAfterSeconds > 0 {
			reset = now.Add(time.Duration(w.ResetAfterSeconds) * time.Second).UTC()
		}
		out.Windows = append(out.Windows, QuotaWindow{
			Name:          windowNameForMinutes(fallbackName, mins),
			Label:         windowLabelForMinutes(fallbackLabel, mins),
			UsedPercent:   clampPercent(w.UsedPercent),
			ResetsAt:      reset,
			WindowMinutes: mins,
			Status:        status,
		})
	}
	addWin("primary", labelWindow5h, body.RateLimit.PrimaryWindow)
	addWin("secondary", labelWindow7d, body.RateLimit.SecondaryWindow)
	return nil
}

func (qs *quotaStore) postAntigravity(ctx context.Context, endpoint string, body any, token, what string) ([]byte, error) {
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode antigravity %s request: %w", what, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, fmt.Errorf("build antigravity %s request: %w", what, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", antigravityUA())
	return qs.doJSON(req)
}

// fetchAntigravityProject asks Code Assist which project serves the account,
// as agy does before reading /usage. Consumer accounts are served under
// aicode-consumers when the answer names no project.
func (qs *quotaStore) fetchAntigravityProject(ctx context.Context, acct resolvedAccount) (project, plan string, err error) {
	raw, err := qs.postAntigravity(ctx, qs.endpoints.AntigravityLoad, map[string]any{
		"metadata": map[string]string{"ideType": "ANTIGRAVITY"},
	}, acct.value, "load")
	if err != nil {
		return "", "", err
	}
	var body struct {
		Project     json.RawMessage        `json:"cloudaicompanionProject"`
		PaidTier    *struct{ Name string } `json:"paidTier"`
		CurrentTier *struct{ Name string } `json:"currentTier"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", "", fmt.Errorf("parse antigravity load: %w", err)
	}
	project = antigravityProjectID(body.Project)

	if body.PaidTier != nil && body.PaidTier.Name != "" {
		plan = body.PaidTier.Name
	} else if body.CurrentTier != nil {
		plan = body.CurrentTier.Name
	}
	return project, plan, nil
}

func antigravityProjectID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.ID
	}
	return ""
}

func (qs *quotaStore) fetchAntigravity(ctx context.Context, acct resolvedAccount, out *AccountQuota) error {
	project, plan, err := qs.fetchAntigravityProject(ctx, acct)
	if err != nil {
		return fmt.Errorf("load antigravity project: %w", err)
	}
	if plan != "" {
		out.Plan = plan
	}
	payload := map[string]string{}
	if project != "" {
		payload["project"] = project
	}
	raw, err := qs.postAntigravity(ctx, qs.endpoints.Antigravity, payload, acct.value, "quota")
	if err != nil {
		return err
	}
	return parseAntigravityQuotaJSON(qs.now().UTC(), raw, out)
}

type antigravityQuotaBucket struct {
	BucketID          string          `json:"bucketId"`
	DisplayName       string          `json:"displayName"`
	Window            string          `json:"window"`
	ResetTime         string          `json:"resetTime"`
	Disabled          bool            `json:"disabled"`
	RemainingFraction *float64        `json:"remainingFraction"`
	RemainingAmount   json.RawMessage `json:"remainingAmount"`
}

func parseAntigravityQuotaJSON(now time.Time, raw []byte, out *AccountQuota) error {
	var body struct {
		Groups []struct {
			DisplayName string                   `json:"displayName"`
			Buckets     []antigravityQuotaBucket `json:"buckets"`
		} `json:"groups"`
		Buckets []antigravityQuotaBucket `json:"buckets"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("parse antigravity quota: %w", err)
	}
	for _, g := range body.Groups {
		for _, b := range g.Buckets {
			addAntigravityBucket(now, out, g.DisplayName, b)
		}
	}
	for _, b := range body.Buckets {
		addAntigravityBucket(now, out, "", b)
	}
	return nil
}

func addAntigravityBucket(now time.Time, out *AccountQuota, group string, b antigravityQuotaBucket) {
	if b.BucketID == "" || b.Disabled {
		return
	}
	w := QuotaWindow{
		Name:          b.BucketID,
		Label:         cmp.Or(b.DisplayName, b.BucketID),
		Group:         group,
		ResetsAt:      parseResetTime(now, b.ResetTime),
		WindowMinutes: antigravityWindowMinutes(b.Window, b.BucketID, b.DisplayName),
	}
	amount, hasAmount := antigravityRemainingAmount(b.RemainingAmount)
	switch {
	case b.RemainingFraction != nil:
		w.UsedPercent = math.Round(clampPercent((1.0-*b.RemainingFraction)*100)*10) / 10
	case hasAmount:
		w.Remaining = amount
		w.Status = "amount"
	case b.ResetTime != "":
		// Proto3 JSON omits a zero fraction, so a reset with no fraction is exhausted.
		w.UsedPercent = 100
	default:
		w.Status = "unknown"
	}
	out.Windows = append(out.Windows, w)
}

func antigravityRemainingAmount(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if v, err := strconv.ParseInt(text, 10, 64); err == nil {
			return v, true
		}
	}
	return 0, false
}

func antigravityWindowMinutes(window, id, label string) int {
	s := strings.ToLower(window + " " + id + " " + label)
	switch {
	case strings.Contains(s, "5h"), strings.Contains(s, "five hour"), strings.Contains(s, "five-hour"):
		return 300
	case strings.Contains(s, "weekly"), strings.Contains(s, "week"), strings.Contains(s, "7d"):
		return 10080
	default:
		return 0
	}
}

func (qs *quotaStore) doJSON(req *http.Request) ([]byte, error) {
	resp, err := qs.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("quota request: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read quota response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close quota response: %w", closeErr)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return raw, nil
}

func normalizePercent(v float64) float64 {
	if v > 0 && v <= 1.0 {
		return clampPercent(v * 100)
	}
	return clampPercent(v)
}

func mergeQuotaWindows(existing, incoming []QuotaWindow) []QuotaWindow {
	byName := make(map[string]QuotaWindow, len(existing)+len(incoming))
	order := make([]string, 0, len(existing)+len(incoming))
	add := func(w QuotaWindow) {
		if _, ok := byName[w.Name]; !ok {
			order = append(order, w.Name)
		}
		byName[w.Name] = w
	}
	for _, w := range existing {
		add(w)
	}
	for _, w := range incoming {
		add(w)
	}
	out := make([]QuotaWindow, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}

func earliestFutureReset(now time.Time, windows []QuotaWindow) time.Time {
	var best time.Time
	for _, w := range windows {
		if w.ResetsAt.IsZero() || !w.ResetsAt.After(now) {
			continue
		}
		if best.IsZero() || w.ResetsAt.Before(best) {
			best = w.ResetsAt
		}
	}
	return best
}

// WatchQuota periodically refreshes provider quota for every loaded account
// until ctx ends.
func (p *Proxy) WatchQuota(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	// Initial fetch shortly after startup once secrets are mounted.
	p.RefreshQuota(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.RefreshQuota(ctx)
		}
	}
}

// Quota returns the latest cached quota records for all observed accounts.
func (p *Proxy) Quota() []AccountQuota { return p.quota.snapshot(p.secrets.Load()) }

// RefreshQuota actively fetches live quota for every supported loaded account.
func (p *Proxy) RefreshQuota(ctx context.Context) []AccountQuota {
	return p.quota.refreshAll(ctx, p.secrets.Load())
}
