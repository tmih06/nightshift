package providers

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"
)

// What a person pastes when they sign in with something made elsewhere.

// errNotLoginFile is the answer to a paste that holds no tokens.
var errNotLoginFile = errors.New("that is not a login file: paste the whole file, from { to }")

// staleWithin is how close to lapsing a pasted access token may be before it
// is renewed at once.
const staleWithin = 5 * time.Minute

// loginFields are the tokens and labels found in a pasted login file.
type loginFields struct {
	Credential
	IDToken string
}

// loginFile reads a pasted login file: a CLI's own, such as Codex's
// auth.json, Claude Code's .credentials.json, or agy's token file, or another
// tool's export of one. It looks for the usual token fields at the top level
// and one object down, and is false when input holds no token.
func loginFile(input string) (loginFields, bool) {
	var root map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(input)), &root) != nil {
		return loginFields{}, false
	}
	objs := []map[string]any{root}
	for _, k := range slices.Sorted(maps.Keys(root)) {
		if m, ok := root[k].(map[string]any); ok {
			objs = append(objs, m)
		}
	}
	f := loginFields{IDToken: field(objs, "id_token", "idToken")}
	f.Access = field(objs, "access_token", "accessToken")
	f.Refresh = field(objs, "refresh_token", "refreshToken")
	f.Email = field(objs, "email", "id_token_email")
	f.AccountID = field(objs, "account_id", "accountId")
	f.Expires = expiry(objs)
	return f, f.Access != "" || f.Refresh != ""
}

// field is the first non-empty string under any of keys, in objs' order.
func field(objs []map[string]any, keys ...string) string {
	for _, o := range objs {
		for _, k := range keys {
			if s, ok := o[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// expiry is when a login file says its access token lapses, or zero.
func expiry(objs []map[string]any) time.Time {
	for _, o := range objs {
		for _, k := range []string{"expiry", "expires_at", "expiresAt", "expired"} {
			if t := asTime(o[k]); !t.IsZero() {
				return t
			}
		}
	}
	return time.Time{}
}

// asTime reads an RFC 3339 time, or Unix seconds or milliseconds.
func asTime(v any) time.Time {
	switch t := v.(type) {
	case string:
		if p, err := time.Parse(time.RFC3339, t); err == nil {
			return p
		}
	case float64:
		if t > 1e12 {
			return time.UnixMilli(int64(t))
		}
		if t > 0 {
			return time.Unix(int64(t), 0)
		}
	}
	return time.Time{}
}

// stale is whether c's access token is missing or about to lapse, so a pasted
// login must be renewed before it is used.
func stale(c Credential) bool {
	return c.Access == "" || (!c.Expires.IsZero() && time.Until(c.Expires) < staleWithin)
}
