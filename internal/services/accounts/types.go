// Package accounts is the data behind the Accounts tab: the coding-agent
// accounts signed in to nightshift-auth, the order each agent's are tried in,
// how much each has been used through the egress proxy, and the actions that
// change them. It talks to nightshift-auth and the proxy over HTTP (through
// port-forwards when run outside the cluster), and imports nothing above
// internal/cluster.
package accounts

import (
	"strings"
	"time"
)

// Provider is an agent whose accounts Nightshift manages, as nightshift-auth
// lists it.
type Provider struct {
	ID      string   `json:"id"`      // the name in every request
	Name    string   `json:"name"`    // what people call it
	Secret  string   `json:"secret"`  // the egress secret its access token travels as
	Methods []Method `json:"methods"` // the ways to sign in, the usual one first
}

// Method is one way to sign an account in, as nightshift-auth lists it.
type Method struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // browser, device, or paste
	Hint  string `json:"hint,omitempty"`
}

// The kinds of Method.
const (
	KindBrowser = "browser"
	KindDevice  = "device"
	KindPaste   = "paste"
)

// providerByID finds a provider in list.
func providerByID(list []Provider, id string) (Provider, bool) {
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// State is how healthy an account is, worst first.
type State int

const (
	StateOK State = iota
	StateExpiring
	StateFailing
	StateSignIn
	StateDisabled
)

func (s State) String() string {
	switch s {
	case StateExpiring:
		return "expiring"
	case StateFailing:
		return "failing"
	case StateSignIn:
		return "sign in"
	case StateDisabled:
		return "disabled"
	default:
		return "ok"
	}
}

// Account is one signed-in login.
type Account struct {
	Name     string // nightshift-auth's name for it, the key for every action
	Provider string
	Email    string
	Disabled bool

	Expires     time.Time // when the current access token stops working
	LastRefresh time.Time
	NextRefresh time.Time

	Success      int
	Failed       int
	RenewalState string
	RefreshError string
	StatusNote   string
	QuotaBlocked bool
	Cooldowns    int
	NeedsSignIn  bool

	// Active marks the primary account this agent's sandboxes start with.
	Active bool
	// Priority is the 1-based position in the provider's fallback chain
	// (0 when disabled).
	Priority int
	// ActiveNow is true when the egress proxy is currently routing traffic
	// to this account (may differ from Active during a failover cooldown).
	ActiveNow bool
	// CooldownUntil is when the egress proxy stops skipping this account.
	CooldownUntil  time.Time
	CooldownReason string
	Failovers      int
	// Usage is what the egress proxy counted for this account; nil when it
	// has not carried a recorded request.
	Usage *Usage
	// History is the account's daily usage, oldest day first, which the proxy
	// derives from its durable usage database.
	History []DayUsage
	// Quota holds the latest fetched or header-observed provider quota windows
	// and next reset time for this account.
	Quota *Quota
}

// Label is how the account is named on screen and in the proxy's usage.
func (a Account) Label() string {
	if a.Email != "" {
		return a.Email
	}
	return strings.TrimSuffix(a.Name, ".json")
}

// State classifies the account at now.
func (a Account) State(now time.Time) State {
	switch {
	case a.Disabled:
		return StateDisabled
	case a.NeedsSignIn:
		return StateSignIn
	case a.RefreshError != "" || a.QuotaBlocked || a.RenewalState == "retry_scheduled":
		return StateFailing
	case !a.Expires.IsZero() && a.Expires.Before(now.Add(2*time.Minute)):
		return StateExpiring
	default:
		return StateOK
	}
}

// ModelUsage counts requests and tokens for one model.
type ModelUsage struct {
	Requests   int
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Reasoning  int64
	Total      int64
}

// Usage is the egress proxy's count of requests and tokens that carried one
// account's credential. In the cluster it is stored in SQLite and survives
// proxy restarts.
type Usage struct {
	Requests        int
	OK              int
	ClientError     int
	RateLimited     int
	ServerError     int
	Input           int64
	Output          int64
	CacheRead       int64
	CacheWrite      int64
	Reasoning       int64
	Total           int64
	CacheHitPercent float64
	AvgLatencyMS    int64
	Last            time.Time
	ByModel         map[string]ModelUsage
	// Recent is the request count of each ten-minute window of the last day,
	// oldest first.
	Recent []int
	// RecentVolume is the total token count of each ten-minute window of the
	// last day, oldest first.
	RecentVolume []int64
}

// DayUsage counts the requests and tokens of one UTC day.
type DayUsage struct {
	Date     string // YYYY-MM-DD
	Requests int
	Total    int64
}

// QuotaWindow is one provider quota window (5h, 7d, RPM/TPM, or model).
type QuotaWindow struct {
	Name          string
	Label         string
	Group         string
	UsedPercent   float64
	Limit         int64
	Used          int64
	Remaining     int64
	ResetsAt      time.Time
	WindowMinutes int
	Status        string
}

// Quota holds fetched or header-observed quota windows and next reset time.
type Quota struct {
	Plan           string
	CreditsBalance string
	Source         string
	FetchedAt      time.Time
	NextResetAt    time.Time
	Windows        []QuotaWindow
	Error          string
	// Models are what the account's CLI offers; ModelsError says why they
	// could not be listed.
	Models      []Model
	ModelsError string
}

// Model is one model an account can be asked for.
type Model struct {
	ID   string
	Name string
}

// Analytics aggregates requests, tokens, latency, and per-model usage across
// all accounts.
type Analytics struct {
	Requests        int
	OK              int
	ClientError     int
	RateLimited     int
	ServerError     int
	Input           int64
	Output          int64
	CacheRead       int64
	CacheWrite      int64
	Reasoning       int64
	Total           int64
	CacheHitPercent float64
	AvgLatencyMS    int64
	ByModel         map[string]ModelUsage
}

// Published is what nightshift-auth last put into the cluster for one agent.
type Published struct {
	Name    string    `json:"name"`
	Label   string    `json:"label"`
	Expires time.Time `json:"expires"`
}

// Snapshot is everything the tab draws.
type Snapshot struct {
	At          time.Time
	Providers   []Provider
	Accounts    []Account
	Chains      map[string][]string // by provider ID: ordered enabled account names
	PublishedAt time.Time
	Published   map[string]Published // by provider ID
	Analytics   *Analytics
	// UsageErr says why proxy usage is missing; the accounts still show.
	UsageErr error
}
