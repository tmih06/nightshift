// Package accounts is the data behind the Accounts tab: the coding-agent
// accounts signed in to CLIProxyAPI, which one each agent's sandboxes use, how
// much each has been used through the egress proxy, and the actions that
// change them. It talks to the cluster through kubectl and to CLIProxyAPI over
// a port-forward, and imports nothing above internal/cluster.
package accounts

import (
	"sort"
	"strings"
	"time"
)

// Provider is an agent whose accounts Nightshift manages.
type Provider struct {
	ID     string // CLIProxyAPI's provider name
	Name   string // what people call it
	Secret string // the egress secret its access token travels as
}

// Providers lists the agents in the order the tab shows them. The secrets
// must match k8s/creds-providers.json; TestProvidersMatchTheRegistry checks.
func Providers() []Provider {
	return []Provider{
		{ID: "claude", Name: "Claude Code", Secret: "CLAUDE_ACCESS_TOKEN"},
		{ID: "codex", Name: "Codex", Secret: "CODEX_ACCESS_TOKEN"},
		{ID: "antigravity", Name: "Antigravity", Secret: "AGY_ACCESS_TOKEN"},
	}
}

// ProviderByID finds a provider by CLIProxyAPI's name for it.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers() {
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
	Name     string // CLIProxyAPI's credential file name, the key for every action
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

// Published is what the refresher last put into the cluster for one agent.
type Published struct {
	Name    string
	Label   string
	Expires time.Time
}

// Snapshot is everything the tab draws.
type Snapshot struct {
	At          time.Time
	Accounts    []Account
	Chains      map[string][]string // by provider ID: ordered enabled account names
	PublishedAt time.Time
	Published   map[string]Published // by provider ID
	Analytics   *Analytics
	// UsageErr says why proxy usage is missing; the accounts still show.
	UsageErr error
}

// Chain returns the ordered enabled accounts for provider: configured entries
// that are still enabled (or active first when configured is empty), followed
// by any remaining enabled accounts sorted by name. k8s/creds-publish.sh
// applies the same rule, so the tab shows the exact fallback order published.
func Chain(accounts []Account, provider string, configured []string, active string) []string {
	enabled := make([]string, 0, len(accounts))
	allowed := map[string]bool{}
	for i := range accounts {
		if accounts[i].Provider == provider && !accounts[i].Disabled {
			enabled = append(enabled, accounts[i].Name)
			allowed[accounts[i].Name] = true
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	sort.Strings(enabled)
	pref := configured
	if len(pref) == 0 && active != "" {
		pref = []string{active}
	}
	seen := make(map[string]bool, len(enabled))
	out := make([]string, 0, len(enabled))
	for _, name := range pref {
		if allowed[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, name := range enabled {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// Chosen is the primary credential an agent's sandboxes get: the one picked as
// active if it is still enabled, else the first enabled one by name.
func Chosen(accounts []Account, provider, active string) (string, bool) {
	chain := Chain(accounts, provider, nil, active)
	if len(chain) == 0 {
		return "", false
	}
	return chain[0], true
}
