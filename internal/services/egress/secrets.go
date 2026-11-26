package egress

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const redactedMarker = "[REDACTED]"

type resolvedSecret struct {
	name        string
	placeholder string
	value       string
	label       string
	scrubForms  [][]byte
}

// signature identifies the loaded secret values so a reload can tell whether
// anything changed without comparing the values themselves.
func (e *SecretEngine) signature() string {
	if e == nil {
		return ""
	}
	h := sha256.New()
	for _, rs := range e.all {
		h.Write([]byte(rs.name))
		h.Write([]byte{0})
		h.Write([]byte(rs.value))
		h.Write([]byte{0})
		h.Write([]byte(rs.label))
		h.Write([]byte{0})
	}
	return string(h.Sum(nil))
}

// SecretEngine swaps opaque placeholders for real credentials on authorized
// outbound requests and scrubs real secret values from inbound responses.
type SecretEngine struct {
	byName map[string]resolvedSecret
	all    []resolvedSecret
}

// NewSecretEngine loads secret values according to bindings using getenv (or
// os.Getenv when nil). Missing/empty secrets are skipped cleanly so unused
// providers do not prevent proxy startup.
func NewSecretEngine(bindings map[string]SecretBinding, getenv func(string) string) (*SecretEngine, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	eng := &SecretEngine{
		byName: make(map[string]resolvedSecret, len(bindings)),
	}
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b := bindings[name]
		val := b.Value
		if val == "" && b.File != "" {
			raw, err := os.ReadFile(b.File)
			if err == nil {
				val = strings.TrimSpace(string(raw))
			}
		}
		if val == "" && b.Env != "" {
			val = strings.TrimSpace(getenv(b.Env))
		}
		if val == "" {
			continue
		}
		ph := b.Placeholder
		if ph == "" {
			ph = DefaultPlaceholderPrefix + name
		}
		rs := resolvedSecret{
			name:        name,
			placeholder: ph,
			value:       val,
			label:       readLabel(b.LabelFile),
			scrubForms:  buildScrubForms(val),
		}
		eng.byName[name] = rs
		eng.all = append(eng.all, rs)
	}
	return eng, nil
}

// readLabel returns the trimmed account label in path, or "" when there is none.
func readLabel(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// Label is the account behind the named secret, or "" when unlabeled.
func (e *SecretEngine) Label(name string) string {
	if e == nil {
		return ""
	}
	return e.byName[name].label
}

func buildScrubForms(val string) [][]byte {
	seen := map[string]struct{}{}
	var forms [][]byte
	add := func(s string) {
		if len(s) < 4 {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		forms = append(forms, []byte(s))
	}
	add(val)
	add(base64.StdEncoding.EncodeToString([]byte(val)))
	add(base64.RawStdEncoding.EncodeToString([]byte(val)))
	add(base64.URLEncoding.EncodeToString([]byte(val)))
	add(base64.RawURLEncoding.EncodeToString([]byte(val)))
	add(url.QueryEscape(val))
	if j, err := json.Marshal(val); err == nil && len(j) >= 2 {
		add(string(j[1 : len(j)-1]))
	}
	// Replace longest forms first so partial substrings do not leave fragments.
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// InjectRequest replaces placeholders for allowedSecrets in req headers
// (including decoded HTTP Basic Auth used by git Smart-HTTP) and URL query
// parameters. Placeholders for secrets not in allowedSecrets are untouched.
func (e *SecretEngine) InjectRequest(req *http.Request, allowedSecrets []string) []string {
	if e == nil || len(allowedSecrets) == 0 {
		return nil
	}
	active := make([]resolvedSecret, 0, len(allowedSecrets))
	for _, name := range allowedSecrets {
		if rs, ok := e.byName[name]; ok {
			active = append(active, rs)
		}
	}
	if len(active) == 0 {
		return nil
	}

	used := map[string]struct{}{}

	// 1. Headers (including Basic auth decoding for git Smart-HTTP).
	for k, vals := range req.Header {
		isAuth := strings.EqualFold(k, "Authorization")
		for i, v := range vals {
			if isAuth && strings.HasPrefix(v, "Basic ") {
				if swapped, names, ok := swapBasicAuth(v, active); ok {
					vals[i] = swapped
					for _, n := range names {
						used[n] = struct{}{}
					}
					continue
				}
			}
			next := v
			for _, rs := range active {
				if strings.Contains(next, rs.placeholder) {
					next = strings.ReplaceAll(next, rs.placeholder, rs.value)
					used[rs.name] = struct{}{}
				}
			}
			vals[i] = next
		}
	}

	// 2. Query parameters (e.g., ?key=nightshift:secret:GEMINI_API_KEY).
	if req.URL != nil && req.URL.RawQuery != "" {
		raw := req.URL.RawQuery
		for _, rs := range active {
			escPH := url.QueryEscape(rs.placeholder)
			if strings.Contains(raw, rs.placeholder) {
				raw = strings.ReplaceAll(raw, rs.placeholder, url.QueryEscape(rs.value))
				used[rs.name] = struct{}{}
			}
			if escPH != rs.placeholder && strings.Contains(raw, escPH) {
				raw = strings.ReplaceAll(raw, escPH, url.QueryEscape(rs.value))
				used[rs.name] = struct{}{}
			}
		}
		req.URL.RawQuery = raw
	}

	if len(used) == 0 {
		return nil
	}
	out := make([]string, 0, len(used))
	for n := range used {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func swapBasicAuth(headerVal string, active []resolvedSecret) (string, []string, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(headerVal, "Basic ")))
	if err != nil {
		return "", nil, false
	}
	decoded := string(raw)
	next := decoded
	var used []string
	for _, rs := range active {
		if strings.Contains(next, rs.placeholder) {
			next = strings.ReplaceAll(next, rs.placeholder, rs.value)
			used = append(used, rs.name)
		}
	}
	if len(used) == 0 {
		return "", nil, false
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(next)), used, true
}

// ScrubBytes replaces any loaded real secret (raw or encoded) in data with
// [REDACTED]. Returns the original slice when nothing matched.
func (e *SecretEngine) ScrubBytes(data []byte) ([]byte, bool) {
	if e == nil || len(e.all) == 0 || len(data) == 0 {
		return data, false
	}
	changed := false
	out := data
	marker := []byte(redactedMarker)
	for _, rs := range e.all {
		for _, form := range rs.scrubForms {
			if bytes.Contains(out, form) {
				out = bytes.ReplaceAll(out, form, marker)
				changed = true
			}
		}
	}
	return out, changed
}

// ScrubResponse scans and redacts any reflected real secrets from response
// headers and body.
func (e *SecretEngine) ScrubResponse(resp *http.Response) (bool, error) {
	if e == nil || len(e.all) == 0 || resp == nil {
		return false, nil
	}
	scrubbed := false
	for _, vals := range resp.Header {
		for i, v := range vals {
			if out, ok := e.ScrubBytes([]byte(v)); ok {
				vals[i] = string(out)
				scrubbed = true
			}
		}
	}
	if resp.Body == nil || resp.Body == http.NoBody {
		return scrubbed, nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return scrubbed, err
	}
	out, ok := e.ScrubBytes(body)
	if ok {
		scrubbed = true
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Del("Content-Length")
	resp.Header.Del("Transfer-Encoding")
	return scrubbed, nil
}
