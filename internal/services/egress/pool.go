package egress

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultFailoverCooldown  = 5 * time.Minute
	maxFailoverCooldown      = 1 * time.Hour
	maxHeaderCooldown        = 6 * time.Hour
	minHeaderCooldown        = 5 * time.Second
	defaultMaxRetryBodyBytes = 32 << 20 // 32 MiB
)

// PoolAccountStatus describes one account in a pooled secret binding, as
// served on the proxy's admin /pools endpoint.
type PoolAccountStatus struct {
	Binding        string    `json:"binding"`
	Name           string    `json:"name"`
	Label          string    `json:"label"`
	Priority       int       `json:"priority"`
	ActiveNow      bool      `json:"active_now"`
	CooldownUntil  time.Time `json:"cooldown_until,omitempty"`
	CooldownReason string    `json:"cooldown_reason,omitempty"`
	Failovers      int       `json:"failovers,omitempty"`
}

type poolKey struct {
	secret  string
	account string
}

type accountCooldown struct {
	until             time.Time
	reason            string
	status            int
	tokenHash         string
	consecutiveErrors int
	failovers         int
}

type poolState struct {
	mu              sync.Mutex
	now             func() time.Time
	defaultCooldown time.Duration
	maxCooldown     time.Duration
	cooldowns       map[poolKey]*accountCooldown
}

func newPoolState(now func() time.Time, defaultCD time.Duration) *poolState {
	if now == nil {
		now = time.Now
	}
	if defaultCD <= 0 {
		defaultCD = defaultFailoverCooldown
	}
	maxCD := maxFailoverCooldown
	if defaultCD > maxCD {
		maxCD = defaultCD * 4
	}
	return &poolState{
		now:             now,
		defaultCooldown: defaultCD,
		maxCooldown:     maxCD,
		cooldowns:       map[poolKey]*accountCooldown{},
	}
}

func hashToken(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// shouldFailover reports whether an upstream HTTP status warrants trying the
// next account in a fallback chain.
func shouldFailover(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusForbidden,
		529:
		return true
	default:
		return false
	}
}

func failoverReason(status int) string {
	switch status {
	case http.StatusTooManyRequests:
		return "rate limited (429)"
	case http.StatusUnauthorized:
		return "unauthorized (401)"
	case http.StatusForbidden:
		return "forbidden (403)"
	case 529:
		return "overloaded (529)"
	default:
		return fmt.Sprintf("error (%d)", status)
	}
}

// candidates returns accounts to try in priority order: every non-cooled-down
// account first (preserving chain order), or if every account is cooling down,
// only the single account closest to recovery.
func (ps *poolState) candidates(secret string, accounts []resolvedAccount) []resolvedAccount {
	if len(accounts) <= 1 {
		return accounts
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := ps.now()
	healthy := make([]resolvedAccount, 0, len(accounts))
	earliestIdx := 0
	var earliest time.Time
	for i, acct := range accounts {
		cd := ps.activeCooldownLocked(secret, acct)
		if cd == nil || !cd.until.After(now) {
			healthy = append(healthy, acct)
			continue
		}
		if earliest.IsZero() || cd.until.Before(earliest) {
			earliest = cd.until
			earliestIdx = i
		}
	}
	if len(healthy) > 0 {
		return healthy
	}
	return []resolvedAccount{accounts[earliestIdx]}
}

func (ps *poolState) activeCooldownLocked(secret string, acct resolvedAccount) *accountCooldown {
	cd := ps.cooldowns[poolKey{secret: secret, account: acct.name}]
	if cd != nil && cd.status == http.StatusUnauthorized && cd.tokenHash != acct.tokenHash {
		cd.until = time.Time{}
		cd.reason = ""
		cd.consecutiveErrors = 0
	}
	return cd
}

func (ps *poolState) markSuccess(secret string, acct resolvedAccount) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if cd := ps.cooldowns[poolKey{secret: secret, account: acct.name}]; cd != nil {
		cd.until = time.Time{}
		cd.reason = ""
		cd.status = 0
		cd.consecutiveErrors = 0
	}
}

func (ps *poolState) markFailure(secret string, acct resolvedAccount, status int, hdr http.Header) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := ps.now()
	k := poolKey{secret: secret, account: acct.name}
	cd := ps.cooldowns[k]
	if cd == nil {
		cd = &accountCooldown{}
		ps.cooldowns[k] = cd
	}
	cd.consecutiveErrors++
	cd.failovers++
	cd.status = status
	cd.tokenHash = acct.tokenHash
	cd.reason = failoverReason(status)
	cd.until = now.Add(ps.cooldownDuration(now, status, cd.consecutiveErrors, hdr))
}

func (ps *poolState) cooldownDuration(now time.Time, status, consecutive int, hdr http.Header) time.Duration {
	if status == http.StatusUnauthorized {
		return ps.maxCooldown
	}
	if d, ok := parseRetryCooldown(now, hdr); ok {
		return d
	}
	d := ps.defaultCooldown
	for i := 1; i < consecutive && d < ps.maxCooldown; i++ {
		d *= 2
	}
	return min(d, ps.maxCooldown)
}

func resetHeaderKeys() []string {
	return []string{
		"Anthropic-Ratelimit-Unified-Reset",
		"Anthropic-Ratelimit-Unified-5h-Reset",
		"Anthropic-Ratelimit-Requests-Reset",
		"Anthropic-Ratelimit-Tokens-Reset",
		"X-Codex-Primary-Reset-At",
		"X-Ratelimit-Reset",
	}
}

func parseRetryCooldown(now time.Time, hdr http.Header) (time.Duration, bool) {
	if hdr == nil {
		return 0, false
	}
	if d, ok := parseRetryAfter(now, hdr.Get("Retry-After")); ok {
		return d, true
	}
	for _, key := range resetHeaderKeys() {
		if d, ok := parseHeaderResetDuration(now, hdr.Get(key)); ok {
			return d, true
		}
	}
	return 0, false
}

func parseRetryAfter(now time.Time, raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs > 0 {
		return clampHeaderCooldown(time.Duration(secs) * time.Second), true
	}
	if t, err := http.ParseTime(raw); err == nil && t.After(now) {
		return clampHeaderCooldown(t.Sub(now)), true
	}
	return 0, false
}

func parseHeaderResetDuration(now time.Time, raw string) (time.Duration, bool) {
	t := parseResetTime(now, raw)
	if t.IsZero() || !t.After(now) {
		return 0, false
	}
	return clampHeaderCooldown(t.Sub(now)), true
}

func clampHeaderCooldown(d time.Duration) time.Duration {
	if d < minHeaderCooldown {
		return minHeaderCooldown
	}
	if d > maxHeaderCooldown {
		return maxHeaderCooldown
	}
	return d
}

// onReload clears 401 auth cooldowns for accounts whose token rotated.
func (ps *poolState) onReload(eng *SecretEngine) {
	if eng == nil {
		return
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, rs := range eng.all {
		for _, acct := range rs.accounts {
			ps.activeCooldownLocked(rs.name, acct)
		}
	}
}

// snapshot reports the live chain position and cooldown state of every loaded
// secret's accounts.
func (ps *poolState) snapshot(eng *SecretEngine) []PoolAccountStatus {
	if eng == nil {
		return []PoolAccountStatus{}
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := ps.now()
	var out []PoolAccountStatus
	for _, rs := range eng.all {
		activeIdx := ps.findActiveIndexLocked(rs, now)
		for i, acct := range rs.accounts {
			out = append(out, ps.statusForAccountLocked(rs.name, acct, i, activeIdx, now))
		}
	}
	return out
}

func (ps *poolState) findActiveIndexLocked(rs resolvedSecret, now time.Time) int {
	earliestIdx := 0
	var earliest time.Time
	for i, acct := range rs.accounts {
		cd := ps.cooldowns[poolKey{secret: rs.name, account: acct.name}]
		if cd == nil || !cd.until.After(now) {
			return i
		}
		if earliest.IsZero() || cd.until.Before(earliest) {
			earliest = cd.until
			earliestIdx = i
		}
	}
	return earliestIdx
}

func (ps *poolState) statusForAccountLocked(secret string, acct resolvedAccount, idx, activeIdx int, now time.Time) PoolAccountStatus {
	st := PoolAccountStatus{
		Binding:   secret,
		Name:      acct.name,
		Label:     acct.label,
		Priority:  idx + 1,
		ActiveNow: idx == activeIdx,
	}
	cd := ps.cooldowns[poolKey{secret: secret, account: acct.name}]
	if cd == nil {
		return st
	}
	st.Failovers = cd.failovers
	if cd.until.After(now) {
		st.CooldownUntil = cd.until.UTC()
		st.CooldownReason = cd.reason
	}
	return st
}
