// Package providers holds the sign-in flows of the coding agents whose
// accounts nightshift-auth keeps. Each agent is one Provider, in a file of its
// own, listed in All: how to start a sign-in, finish it from what the browser
// ends on, renew the token, and write the login a sandbox gets, which names no
// account and holds only a placeholder the egress proxy swaps for the token.
//
// The flows are ported from ompig-slim's PiG (MIT); see README.md.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Info is what the rest of Nightshift knows an agent by.
type Info struct {
	// ID names the agent in every request, such as claude.
	ID string
	// Name is what people call it.
	Name string
	// Secret is the egress secret its access token travels as; the
	// placeholder in a sandbox is nightshift:secret:<Secret>.
	Secret string
	// Pool is the warm pool kept while the agent has an account.
	Pool string
	// LoginFile is where the agent's CLI keeps its login, under $HOME.
	LoginFile string
	// Hosts are the API hosts where the egress proxy swaps Secret in.
	Hosts []string
	// Methods are the ways to sign an account in, the usual one first.
	Methods []Method
}

// Kind says what a person does to finish a sign-in.
type Kind string

// The kinds of sign-in.
const (
	// KindBrowser: open the link, approve, and paste the code the page shows
	// or the address the browser ends on.
	KindBrowser Kind = "browser"
	// KindDevice: open the link and enter the code shown; the sign-in
	// finishes by itself once approved.
	KindDevice Kind = "device"
	// KindPaste: paste something made elsewhere, such as a token or a CLI's
	// login file. There is no link.
	KindPaste Kind = "paste"
)

// Method is one way to sign an account in.
type Method struct {
	ID    string
	Label string
	Kind  Kind
	// Hint says what to do, or where the pasted value comes from.
	Hint string
}

// Credential is one signed-in login as nightshift-auth stores it. It never
// leaves the service except as the access token, to the egress proxy.
type Credential struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
	// Email and AccountID name the account. They label it in the Accounts
	// tab and never reach a sandbox.
	Email     string `json:"email,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	// Plan and Tier describe the subscription, where a CLI wants them.
	Plan string `json:"plan,omitempty"`
	Tier string `json:"tier,omitempty"`
}

// Pending is a sign-in started and not yet finished.
type Pending struct {
	Provider string
	Method   Method
	// URL is the page to open; empty for KindPaste.
	URL         string
	State       string
	Verifier    string
	RedirectURI string
	// UserCode is what to enter on URL's page, for KindDevice.
	UserCode string
	// Device names the sign-in to the provider while it waits, for
	// KindDevice; Interval is how often to ask whether it was approved.
	Device   string
	Interval time.Duration
}

// Provider is one agent's sign-in flow.
type Provider interface {
	Info() Info
	// Begin starts a sign-in by one of Info().Methods, the first when method
	// is empty.
	Begin(ctx context.Context, method string) (Pending, error)
	// Finish completes it from what the person pastes: the address the
	// browser ended on, the code the provider showed, or a token or login
	// file. A device sign-in takes no input and answers ErrPending until it
	// is approved.
	Finish(ctx context.Context, p Pending, input string) (Credential, error)
	// Refresh renews the access token; ErrNoRefresh when the login cannot be
	// renewed and must be signed in again.
	Refresh(ctx context.Context, c Credential) (Credential, error)
	// SandboxLogin is the login file a sandbox gets: built from a named list
	// of fields, with placeholder for the token and nothing that names the
	// account.
	SandboxLogin(c Credential, placeholder string) ([]byte, error)
	// Companions are other account-bound values the proxy swaps in, by
	// secret name, such as Codex's account id.
	Companions(c Credential) map[string]string
}

// ErrNoRefresh says a login cannot be renewed and must be signed in again.
var ErrNoRefresh = errors.New("this login cannot be renewed: sign it in again")

// ErrPending says a device sign-in has not been approved yet: ask again after
// Pending.Interval.
var ErrPending = errors.New("waiting for the sign-in to be approved")

// method is the one of p's methods named id, the first when id is empty.
func method(p Provider, id string) (Method, error) {
	ms := p.Info().Methods
	if id == "" && len(ms) > 0 {
		return ms[0], nil
	}
	for _, m := range ms {
		if m.ID == id {
			return m, nil
		}
	}
	return Method{}, fmt.Errorf("%s has no sign-in method %q", p.Info().Name, id)
}

// All is every agent nightshift-auth signs in, in the order the Accounts tab
// shows them. Adding one is a file with its Provider and a line here.
func All(hc *http.Client) []Provider {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return []Provider{newClaude(hc), newCodex(hc), newAntigravity(hc), newDevin(hc)}
}

// ByID finds the agent named id in list.
func ByID(list []Provider, id string) (Provider, bool) {
	for _, p := range list {
		if p.Info().ID == id {
			return p, true
		}
	}
	return nil, false
}
