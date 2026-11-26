package v1

import "time"

// Account states, the values of Account.State, best first.
const (
	AccountOK       = "ok"
	AccountExpiring = "expiring" // the token is about to lapse
	AccountFailing  = "failing"  // renewal is failing or the quota is used up
	AccountSignIn   = "sign_in"  // the account must be signed in again
	AccountDisabled = "disabled"
)

// Provider is a coding agent whose accounts Nightshift manages.
type Provider struct {
	// ID is the name used in every request, such as claude, codex, or
	// antigravity.
	ID string `json:"id"`
	// Name is what people call it.
	Name string `json:"name"`
}

// Accounts is everything the accounts screen shows: GET /v1/accounts.
type Accounts struct {
	ObservedAt time.Time  `json:"observedAt"`
	Providers  []Provider `json:"providers"`
	Accounts   []Account  `json:"accounts"`
	// Chains maps each provider ID to its ordered fallback chain of enabled
	// account names.
	Chains map[string][]string `json:"chains,omitempty"`
	// Published says, for each provider ID, which account the sandboxes got at
	// the last publish.
	Published map[string]Published `json:"published,omitempty"`
	// PublishedAt is when the last publish ran.
	PublishedAt time.Time `json:"publishedAt,omitzero"`
	// Analytics aggregates requests, tokens, latency, and models across all
	// accounts observed by the egress proxy.
	Analytics *AnalyticsSummary `json:"analytics,omitempty"`
	// UsageError says why Usage is missing from every account; the accounts
	// themselves are still good.
	UsageError string `json:"usageError,omitempty"`
}

// Account is one signed-in login. It carries no token.
type Account struct {
	// Name is the account's id in every request.
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Email    string `json:"email,omitempty"`
	// State is one of the Account constants.
	State    string `json:"state"`
	Disabled bool   `json:"disabled"`
	// Active marks the primary account at the head of the provider's fallback chain.
	Active bool `json:"active"`
	// Priority is the 1-based position in the provider's fallback chain (0 when disabled).
	Priority int `json:"priority,omitempty"`
	// ActiveNow is true when the egress proxy is currently routing traffic to
	// this account (may differ from Active while the primary is in cooldown).
	ActiveNow bool `json:"activeNow,omitempty"`
	// CooldownUntil is when the egress proxy will try this account again.
	CooldownUntil  time.Time `json:"cooldownUntil,omitzero"`
	CooldownReason string    `json:"cooldownReason,omitempty"`
	Failovers      int       `json:"failovers,omitempty"`

	// ExpiresAt is when the current access token stops working.
	ExpiresAt     time.Time `json:"expiresAt,omitzero"`
	LastRefreshAt time.Time `json:"lastRefreshAt,omitzero"`
	NextRefreshAt time.Time `json:"nextRefreshAt,omitzero"`

	RefreshOK     int    `json:"refreshOk"`
	RefreshFailed int    `json:"refreshFailed"`
	RefreshError  string `json:"refreshError,omitempty"`
	StatusNote    string `json:"statusNote,omitempty"`
	QuotaBlocked  bool   `json:"quotaBlocked,omitempty"`
	Cooldowns     int    `json:"cooldowns,omitempty"`

	// Usage is what the egress proxy counted for this account; absent when it
	// has carried no request since the proxy last started.
	Usage *AccountUsage `json:"usage,omitempty"`
	// Quota holds the latest fetched or header-observed provider quota windows
	// and next reset times for this account.
	Quota *AccountQuota `json:"quota,omitempty"`
}

// ModelUsage counts requests and tokens for one model.
type ModelUsage struct {
	Requests   int   `json:"requests"`
	Input      int64 `json:"input,omitempty"`
	Output     int64 `json:"output,omitempty"`
	CacheRead  int64 `json:"cacheRead,omitempty"`
	CacheWrite int64 `json:"cacheWrite,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
	Total      int64 `json:"total,omitempty"`
}

// AccountUsage is the egress proxy's count of requests and tokens that carried
// one account's credential. It lives in proxy memory and resets on restart.
type AccountUsage struct {
	Requests        int                   `json:"requests"`
	OK              int                   `json:"ok"`
	ClientError     int                   `json:"clientError"`
	RateLimited     int                   `json:"rateLimited"`
	ServerError     int                   `json:"serverError"`
	Input           int64                 `json:"input,omitempty"`
	Output          int64                 `json:"output,omitempty"`
	CacheRead       int64                 `json:"cacheRead,omitempty"`
	CacheWrite      int64                 `json:"cacheWrite,omitempty"`
	Reasoning       int64                 `json:"reasoning,omitempty"`
	Total           int64                 `json:"total,omitempty"`
	CacheHitPercent float64               `json:"cacheHitPercent,omitempty"`
	AvgLatencyMS    int64                 `json:"avgLatencyMs,omitempty"`
	LastAt          time.Time             `json:"lastAt,omitzero"`
	ByModel         map[string]ModelUsage `json:"byModel,omitempty"`
	// Recent is the request count of each ten-minute window of the last day,
	// oldest first.
	Recent []int `json:"recent,omitempty"`
	// RecentVolume is the total token count of each ten-minute window of the
	// last day, oldest first.
	RecentVolume []int64 `json:"recentVolume,omitempty"`
}

// QuotaWindow is one provider quota window (5h, 7d, RPM/TPM, or model).
type QuotaWindow struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	UsedPercent   float64   `json:"usedPercent"`
	Limit         int64     `json:"limit,omitempty"`
	Used          int64     `json:"used,omitempty"`
	Remaining     int64     `json:"remaining,omitempty"`
	ResetsAt      time.Time `json:"resetsAt,omitzero"`
	WindowMinutes int       `json:"windowMinutes,omitempty"`
	Status        string    `json:"status,omitempty"`
}

// AccountQuota holds provider quota windows and next reset time for one account.
type AccountQuota struct {
	Plan           string        `json:"plan,omitempty"`
	CreditsBalance string        `json:"creditsBalance,omitempty"`
	Source         string        `json:"source,omitempty"`
	FetchedAt      time.Time     `json:"fetchedAt,omitzero"`
	NextResetAt    time.Time     `json:"nextResetAt,omitzero"`
	Windows        []QuotaWindow `json:"windows,omitempty"`
	Error          string        `json:"error,omitempty"`
}

// AnalyticsSummary aggregates requests, tokens, latency, and per-model usage
// across all accounts.
type AnalyticsSummary struct {
	Requests        int                   `json:"requests"`
	OK              int                   `json:"ok"`
	ClientError     int                   `json:"clientError"`
	RateLimited     int                   `json:"rateLimited"`
	ServerError     int                   `json:"serverError"`
	Input           int64                 `json:"input,omitempty"`
	Output          int64                 `json:"output,omitempty"`
	CacheRead       int64                 `json:"cacheRead,omitempty"`
	CacheWrite      int64                 `json:"cacheWrite,omitempty"`
	Reasoning       int64                 `json:"reasoning,omitempty"`
	Total           int64                 `json:"total,omitempty"`
	CacheHitPercent float64               `json:"cacheHitPercent,omitempty"`
	AvgLatencyMS    int64                 `json:"avgLatencyMs,omitempty"`
	ByModel         map[string]ModelUsage `json:"byModel,omitempty"`
}

// Published is what the last publish put into the cluster for one provider.
type Published struct {
	Account   string    `json:"account"`
	Label     string    `json:"label,omitempty"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
}

// LoginRequest starts signing an account in.
type LoginRequest struct {
	Provider string `json:"provider"`
}

// Login is a sign-in in progress. The person opens URL in a browser and
// approves; the browser then lands on an address that does not load, and that
// address goes to CompleteLogin.
type Login struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

// LoginCompletion finishes a sign-in.
type LoginCompletion struct {
	// Provider is Login.Provider.
	Provider string `json:"provider"`
	// RedirectURL is the address the browser ended on after approval.
	RedirectURL string `json:"redirectUrl"`
}

// AccountPatch changes an account. Only the fields present change.
type AccountPatch struct {
	// Disabled turns the account off or on without deleting it.
	Disabled *bool `json:"disabled,omitempty"`
}

// ActiveAccount picks the primary account at the head of a provider's chain.
type ActiveAccount struct {
	Account string `json:"account"`
}

// AccountChain sets the ordered fallback chain for a provider's accounts.
type AccountChain struct {
	Accounts []string `json:"accounts"`
}

// PublishReport is the answer to POST /v1/accounts/publish.
type PublishReport struct {
	// Report is what the publisher printed.
	Report string `json:"report"`
}
