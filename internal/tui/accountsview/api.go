package accountsview

import (
	"context"
	"sort"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// API is what the Accounts tab asks of the Nightshift services. *client.Client
// satisfies it, and a test can stand in for it.
type API interface {
	Accounts(ctx context.Context) (v1.Accounts, error)
	StartLogin(ctx context.Context, provider, method string) (v1.Login, error)
	CompleteLogin(ctx context.Context, l v1.Login, input string) (v1.LoginProgress, error)
	CancelLogin(ctx context.Context, l v1.Login) error
	SetDisabled(ctx context.Context, name string, disabled bool) error
	RemoveAccount(ctx context.Context, name string) error
	RefreshAccount(ctx context.Context, name string) error
	SetActive(ctx context.Context, provider, name string) error
	SetChain(ctx context.Context, provider string, names []string) error
	Publish(ctx context.Context) (string, error)
	RefreshQuotas(ctx context.Context) error
}

// byProvider is one agent's accounts in fallback-chain priority order
// (enabled first by Priority, then active, then by name).
func byProvider(s v1.Accounts, id string) []v1.Account {
	var out []v1.Account
	for i := range s.Accounts {
		if s.Accounts[i].Provider == id {
			out = append(out, s.Accounts[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].Priority, out[j].Priority
		if pi > 0 && pj > 0 && pi != pj {
			return pi < pj
		}
		if (pi > 0) != (pj > 0) {
			return pi > 0
		}
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// providerByID finds an agent among the ones the server manages; one it does
// not list is named by its id.
func providerByID(s v1.Accounts, id string) v1.Provider {
	for _, p := range s.Providers {
		if p.ID == id {
			return p
		}
	}
	return v1.Provider{ID: id, Name: id}
}

// label is how an account is named on screen: its email, else its file name
// without the extension.
func label(a v1.Account) string {
	if a.Email != "" {
		return a.Email
	}
	return strings.TrimSuffix(a.Name, ".json")
}

// stateName is an account's state as the screen says it.
func stateName(state string) string {
	return strings.ReplaceAll(state, "_", " ")
}
