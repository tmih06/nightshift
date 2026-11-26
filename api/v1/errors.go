package v1

// Problem is the body of every error response, after RFC 9457. Its media type
// is application/problem+json.
type Problem struct {
	// Type is a URI naming the kind of problem; "about:blank" when Code says it.
	Type string `json:"type,omitempty"`
	// Title is a short summary that does not change between occurrences.
	Title string `json:"title"`
	// Status repeats the HTTP status code.
	Status int `json:"status"`
	// Detail says what went wrong this time, for a person.
	Detail string `json:"detail,omitempty"`
	// Code is the stable, machine-readable reason. Frontends branch on this,
	// never on Detail.
	Code string `json:"code"`
}

// Error codes. A client treats a code it does not know by its HTTP status.
const (
	// CodeInvalid is a request the server cannot act on (400, 422).
	CodeInvalid = "invalid"
	// CodeUnauthorized is a missing or wrong bearer token (401).
	CodeUnauthorized = "unauthorized"
	// CodeNotFound is a resource that does not exist (404).
	CodeNotFound = "not_found"
	// CodeConflict is a request the resource's state forbids (409).
	CodeConflict = "conflict"
	// CodeUnavailable is a cluster, or something in it, that cannot be reached
	// or has not been set up (503). The request may succeed later.
	CodeUnavailable = "unavailable"
	// CodeTimeout is an operation that did not finish in time (504).
	CodeTimeout = "timeout"
	// CodeInternal is a bug in the server (500).
	CodeInternal = "internal"
)

// Error makes a Problem usable as a Go error on the client side.
func (p Problem) Error() string {
	if p.Detail != "" {
		return p.Title + ": " + p.Detail
	}
	return p.Title
}
