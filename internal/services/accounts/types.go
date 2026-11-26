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

	// Active marks the account this agent's sandboxes get.
	Active bool
	// Usage is what the egress proxy counted for this account; nil when it
	// has not carried a request since the proxy last started.
	Usage *Usage
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

// Usage is the egress proxy's count of requests that carried one account's
// token. It is kept in the proxy's memory and starts over when it restarts.
type Usage struct {
	Requests    int
	OK          int
	ClientError int
	RateLimited int
	ServerError int
	Last        time.Time
	// Recent is the request count of each ten-minute window of the last day,
	// oldest first.
	Recent []int
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
	PublishedAt time.Time
	Published   map[string]Published // by provider ID
	// UsageErr says why proxy usage is missing; the accounts still show.
	UsageErr error
}

// Chosen is the credential an agent's sandboxes get: the one picked as active
// if it is still enabled, else the first enabled one by name. k8s/creds-publish.sh
// applies the same rule, so the tab shows what will be published.
func Chosen(accounts []Account, provider, active string) (string, bool) {
	var enabled []string
	for i := range accounts {
		if accounts[i].Provider == provider && !accounts[i].Disabled {
			enabled = append(enabled, accounts[i].Name)
		}
	}
	if len(enabled) == 0 {
		return "", false
	}
	sort.Strings(enabled)
	for _, name := range enabled {
		if name == active {
			return name, true
		}
	}
	return enabled[0], true
}
