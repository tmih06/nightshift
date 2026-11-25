// Package v1 is the wire contract of the Nightshift services API.
//
// Nightshift is two programs. The services (cmd/nightshiftd) own everything
// that touches the cluster: the sandbox fleet, claims, volumes and checkouts,
// terminals inside sandboxes, and the coding-agent accounts. A frontend, such
// as the terminal UI in cmd/nightshift, only talks to them over HTTP. These
// types are the JSON bodies of that conversation, and api/openapi.yaml is
// the same contract in a form any language can read.
//
// The package imports nothing but the standard library, so a Go frontend can
// depend on it alone. The rules that keep v1 stable:
//
//   - A field is only ever added. Removing, renaming, or changing the meaning
//     of one needs v2.
//   - A client ignores fields it does not know, and a server ignores request
//     fields it does not know.
//   - Times are RFC 3339 in UTC. Enumerations are lowercase strings, and a
//     client treats a value it does not know as "other".
//   - No response ever carries a credential: not a token, not a refresh token,
//     not a management key. TestNoCredentialFields keeps that true.
package v1

// Version is the API version this package describes.
const Version = "v1"

// Capabilities a server may list in Info.Capabilities. A frontend checks for
// one before it relies on an optional part of the API.
const (
	// CapFleetWatch means GET /v1/fleet/watch streams fleet changes.
	CapFleetWatch = "fleet.watch"
	// CapAccounts means the accounts endpoints are available.
	CapAccounts = "accounts"
	// CapCheckouts means a pull request, issue, or branch can be checked out
	// onto its own volume.
	CapCheckouts = "checkouts"
	// CapTerminals means terminals inside claimed sandboxes can be driven.
	CapTerminals = "terminals"
)
