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
	// Published says, for each provider ID, which account the sandboxes got at
	// the last publish.
	Published map[string]Published `json:"published,omitempty"`
	// PublishedAt is when the last publish ran.
	PublishedAt time.Time `json:"publishedAt,omitzero"`
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
	// Active marks the account the provider's new sandboxes get.
	Active bool `json:"active"`

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
}

// AccountUsage is the egress proxy's count of requests that carried one
// account's token. It is the proxy's own count, not the provider's quota, and
// it starts over when the proxy restarts.
type AccountUsage struct {
	Requests    int       `json:"requests"`
	OK          int       `json:"ok"`
	ClientError int       `json:"clientError"`
	RateLimited int       `json:"rateLimited"`
	ServerError int       `json:"serverError"`
	LastAt      time.Time `json:"lastAt,omitzero"`
	// Recent is the request count of each ten-minute window of the last day,
	// oldest first.
	Recent []int `json:"recent,omitempty"`
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

// ActiveAccount picks the account a provider's new sandboxes get.
type ActiveAccount struct {
	Account string `json:"account"`
}

// PublishReport is the answer to POST /v1/accounts/publish.
type PublishReport struct {
	// Report is what the publisher printed.
	Report string `json:"report"`
}
