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
	// Methods are the ways to sign one of its accounts in, the usual one
	// first.
	Methods []LoginMethod `json:"methods,omitempty"`
}

// The kinds of sign-in, the values of LoginMethod.Kind.
const (
	// LoginBrowser: open Login.URL, approve, and complete the sign-in with
	// the code the page shows or the address the browser ends on.
	LoginBrowser = "browser"
	// LoginDevice: open Login.URL and enter Login.UserCode. Complete the
	// sign-in with no input every Login.PollSeconds until it is done.
	LoginDevice = "device"
	// LoginPaste: complete the sign-in with something made elsewhere, such
	// as a token or a CLI's login file. There is no URL.
	LoginPaste = "paste"
)

// LoginMethod is one way to sign an agent's account in.
type LoginMethod struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind is one of the Login kinds.
	Kind string `json:"kind"`
	// Hint says what to do, or where the pasted value comes from.
	Hint string `json:"hint,omitempty"`
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
	// has carried no recorded request.
	Usage *AccountUsage `json:"usage,omitempty"`
	// History is the account's daily usage for up to the last 53 weeks, oldest
	// day first, without the days that saw no request. Both Usage and History
	// survive a restart of the egress proxy.
	History []DayUsage `json:"history,omitempty"`
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
// one account's credential. In the cluster it is stored in SQLite and survives
// proxy restarts.
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

// DayUsage counts the requests and tokens of one UTC day.
type DayUsage struct {
	Date     string `json:"date"` // YYYY-MM-DD
	Requests int    `json:"requests"`
	// Total is the day's token count.
	Total int64 `json:"total,omitempty"`
}

// QuotaWindow is one provider quota window (5h, 7d, RPM/TPM, or model).
type QuotaWindow struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	Group         string    `json:"group,omitempty"`
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
	// Models are what the account's CLI offers, in its order; ModelsError says
	// why they could not be listed.
	Models      []AccountModel `json:"models,omitempty"`
	ModelsError string         `json:"modelsError,omitempty"`
}

// AccountModel is one model an account can be asked for.
type AccountModel struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
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
	// Method is one of the provider's LoginMethod IDs; empty is its first.
	Method string `json:"method,omitempty"`
}

// Login is a sign-in in progress; what to do with it depends on
// Method.Kind. For a browser sign-in the person opens URL and approves, and
// the browser then lands on an address that does not load, or the page shows
// a code: either goes to CompleteLogin.
type Login struct {
	ID       string      `json:"id"`
	Provider string      `json:"provider"`
	Method   LoginMethod `json:"method"`
	// URL is the page to open; empty for a paste.
	URL string `json:"url,omitempty"`
	// UserCode is what to enter on URL's page, and PollSeconds how often to
	// ask whether it was, for a device sign-in.
	UserCode    string `json:"userCode,omitempty"`
	PollSeconds int    `json:"pollSeconds,omitempty"`
}

// LoginCompletion finishes a sign-in.
type LoginCompletion struct {
	// Provider is Login.Provider.
	Provider string `json:"provider"`
	// Input is what the person pasted: the address the browser ended on, the
	// code the page showed, or a token or login file. It is empty when asking
	// after a device sign-in.
	Input string `json:"input,omitempty"`
	// RedirectURL is Input's older name, read when Input is empty.
	RedirectURL string `json:"redirectUrl,omitempty"`
}

// LoginProgress answers a completion. A device sign-in that is not approved
// yet is not Done: ask again after Login.PollSeconds.
type LoginProgress struct {
	Done bool `json:"done"`
	// Account is the signed-in account's name once Done.
	Account string `json:"account,omitempty"`
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
