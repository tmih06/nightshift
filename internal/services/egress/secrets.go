package egress

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const redactedMarker = "[REDACTED]"

type poolEntry struct {
	Name      string            `json:"name"`
	Label     string            `json:"label"`
	Token     string            `json:"token"`
	Companion map[string]string `json:"companion,omitempty"`
}

type resolvedAccount struct {
	name       string
	label      string
	value      string
	tokenHash  string
	companions map[string]string // placeholder -> real value
}

type resolvedSecret struct {
	name        string
	placeholder string
	accounts    []resolvedAccount
	scrubForms  [][]byte
}

type injectedBinding struct {
	secret  string
	account string
	label   string
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
		for _, acct := range rs.accounts {
			hashAccount(h, acct)
		}
		h.Write([]byte{1})
	}
	return string(h.Sum(nil))
}

func hashAccount(h hash.Hash, acct resolvedAccount) {
	h.Write([]byte(acct.name))
	h.Write([]byte{0})
	h.Write([]byte(acct.value))
	h.Write([]byte{0})
	h.Write([]byte(acct.label))
	h.Write([]byte{0})
	if len(acct.companions) == 0 {
		return
	}
	keys := make([]string, 0, len(acct.companions))
	for k := range acct.companions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(acct.companions[k]))
		h.Write([]byte{0})
	}
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
		accounts := loadBindingAccounts(name, b, getenv)
		if len(accounts) == 0 {
			continue
		}
		ph := b.Placeholder
		if ph == "" {
			ph = DefaultPlaceholderPrefix + name
		}
		vals := make([]string, 0, len(accounts))
		for _, acct := range accounts {
			vals = append(vals, acct.value)
		}
		rs := resolvedSecret{
			name:        name,
			placeholder: ph,
			accounts:    accounts,
			scrubForms:  buildScrubForms(vals...),
		}
		eng.byName[name] = rs
		eng.all = append(eng.all, rs)
	}
	return eng, nil
}

func loadBindingAccounts(name string, b SecretBinding, getenv func(string) string) []resolvedAccount {
	if b.PoolFile != "" {
		if pool := readPoolFile(name, b.PoolFile); len(pool) > 0 {
			return pool
		}
	}
	val := b.Value
	if val == "" && b.File != "" {
		raw, err := os.ReadFile(filepath.Clean(b.File))
		if err == nil {
			val = strings.TrimSpace(string(raw))
		}
	}
	if val == "" && b.Env != "" {
		val = strings.TrimSpace(getenv(b.Env))
	}
	if val == "" {
		return nil
	}
	return []resolvedAccount{{
		name:      name,
		label:     readLabel(b.LabelFile),
		value:     val,
		tokenHash: hashToken(val),
	}}
}

func readPoolFile(secretName, path string) []resolvedAccount {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil
	}
	var entries []poolEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	accounts := make([]resolvedAccount, 0, len(entries))
	for _, e := range entries {
		if acct, ok := parsePoolEntry(secretName, e); ok {
			accounts = append(accounts, acct)
		}
	}
	return accounts
}

func parsePoolEntry(secretName string, e poolEntry) (resolvedAccount, bool) {
	tok := strings.TrimSpace(e.Token)
	if tok == "" {
		return resolvedAccount{}, false
	}
	acctName := firstNonEmpty(strings.TrimSpace(e.Name), strings.TrimSpace(e.Label), secretName)
	label := strings.TrimSpace(e.Label)
	if label == "" {
		label = strings.TrimSuffix(acctName, ".json")
	}
	return resolvedAccount{
		name:       acctName,
		label:      label,
		value:      tok,
		tokenHash:  hashToken(tok),
		companions: parseCompanions(e.Companion),
	}, true
}

func parseCompanions(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		ph := k
		if !strings.HasPrefix(ph, DefaultPlaceholderPrefix) {
			ph = DefaultPlaceholderPrefix + k
		}
		out[ph] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// Label is the primary account behind the named secret, or "" when unlabeled.
func (e *SecretEngine) Label(name string) string {
	if e == nil {
		return ""
	}
	rs, ok := e.byName[name]
	if !ok || len(rs.accounts) == 0 {
		return ""
	}
	return rs.accounts[0].label
}

// PooledSecretFor returns the first pooled secret (more than one account) in
// allowedSecrets whose placeholder appears in req.
func (e *SecretEngine) PooledSecretFor(req *http.Request, allowedSecrets []string) (resolvedSecret, bool) {
	if e == nil || req == nil || len(allowedSecrets) == 0 {
		return resolvedSecret{}, false
	}
	for _, name := range allowedSecrets {
		rs, ok := e.byName[name]
		if ok && len(rs.accounts) > 1 && requestHasPlaceholder(req, rs.placeholder) {
			return rs, true
		}
	}
	return resolvedSecret{}, false
}

func requestHasPlaceholder(req *http.Request, placeholder string) bool {
	return headersHavePlaceholder(req.Header, placeholder) || queryHasPlaceholder(req.URL, placeholder)
}

func headersHavePlaceholder(hdr http.Header, placeholder string) bool {
	for k, vals := range hdr {
		isAuth := strings.EqualFold(k, "Authorization")
		for _, v := range vals {
			if headerValueHasPlaceholder(v, placeholder, isAuth) {
				return true
			}
		}
	}
	return false
}

func headerValueHasPlaceholder(v, placeholder string, isAuth bool) bool {
	if strings.Contains(v, placeholder) {
		return true
	}
	if !isAuth || !strings.HasPrefix(v, "Basic ") {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(v, "Basic ")))
	return err == nil && strings.Contains(string(raw), placeholder)
}

func queryHasPlaceholder(u *url.URL, placeholder string) bool {
	if u == nil || u.RawQuery == "" {
		return false
	}
	if strings.Contains(u.RawQuery, placeholder) {
		return true
	}
	escPH := url.QueryEscape(placeholder)
	return escPH != placeholder && strings.Contains(u.RawQuery, escPH)
}

func buildScrubForms(vals ...string) [][]byte {
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
	for _, val := range vals {
		add(val)
		add(base64.StdEncoding.EncodeToString([]byte(val)))
		add(base64.RawStdEncoding.EncodeToString([]byte(val)))
		add(base64.URLEncoding.EncodeToString([]byte(val)))
		add(base64.RawURLEncoding.EncodeToString([]byte(val)))
		add(url.QueryEscape(val))
		if j, err := json.Marshal(val); err == nil && len(j) >= 2 {
			add(string(j[1 : len(j)-1]))
		}
	}
	// Replace longest forms first so partial substrings do not leave fragments.
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

type activeSecret struct {
	name        string
	placeholder string
	account     resolvedAccount
}

// InjectRequest replaces placeholders for allowedSecrets in req headers
// (including decoded HTTP Basic Auth used by git Smart-HTTP) and URL query
// parameters. Placeholders for secrets not in allowedSecrets are untouched.
func (e *SecretEngine) InjectRequest(req *http.Request, allowedSecrets []string) []string {
	bindings := e.injectRequestSelected(req, allowedSecrets, nil)
	if len(bindings) == 0 {
		return nil
	}
	out := make([]string, len(bindings))
	for i, b := range bindings {
		out[i] = b.secret
	}
	return out
}

func (e *SecretEngine) injectRequestSelected(req *http.Request, allowedSecrets []string, chosen map[string]resolvedAccount) []injectedBinding {
	return e.injectRequest(req, allowedSecrets, chosen, 0)
}

// injectRequest swaps placeholders in req's headers and query and, when
// protoLimit is positive, inside a protobuf body of at most that many bytes.
func (e *SecretEngine) injectRequest(req *http.Request, allowedSecrets []string, chosen map[string]resolvedAccount, protoLimit int64) []injectedBinding {
	active := e.selectActiveSecrets(allowedSecrets, chosen)
	if len(active) == 0 {
		return nil
	}
	used := map[string]resolvedAccount{}
	injectHeaders(req.Header, active, used)
	injectQuery(req.URL, active, used)
	if protoLimit > 0 {
		injectProtoBody(req, active, used, protoLimit)
	}
	return sortedBindings(used)
}

// injectProtoBody swaps placeholders inside req's protobuf body. A body over
// limit, or one it cannot read as protobuf, goes on as it was.
func injectProtoBody(req *http.Request, active []activeSecret, used map[string]resolvedAccount, limit int64) {
	if protoFramingOf(req.Header.Get("Content-Type")) == protoNone || req.Body == nil || req.Body == http.NoBody {
		return
	}
	body, overflow, rest, err := readBoundedBody(req.Body, limit)
	if err != nil {
		req.Body = http.NoBody
		return
	}
	if overflow {
		req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), rest))
		return
	}
	hit := map[string]resolvedAccount{}
	out, changed, err := rewriteProtoBody(req.Header, body, func(s []byte) ([]byte, bool) {
		return swapPlaceholders(s, active, hit)
	})
	if err != nil || !changed {
		out = body
	} else {
		for name, acct := range hit {
			used[name] = acct
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(out))
	req.ContentLength = int64(len(out))
}

// swapPlaceholders replaces every placeholder (and companion placeholder) of
// active in s, noting which secrets it used.
func swapPlaceholders(s []byte, active []activeSecret, used map[string]resolvedAccount) ([]byte, bool) {
	out, changed := s, false
	for _, as := range active {
		if ph := []byte(as.placeholder); bytes.Contains(out, ph) {
			out = bytes.ReplaceAll(out, ph, []byte(as.account.value))
			used[as.name], changed = as.account, true
		}
		for compPH, compVal := range as.account.companions {
			if ph := []byte(compPH); bytes.Contains(out, ph) {
				out = bytes.ReplaceAll(out, ph, []byte(compVal))
				used[as.name], changed = as.account, true
			}
		}
	}
	return out, changed
}

func (e *SecretEngine) selectActiveSecrets(allowedSecrets []string, chosen map[string]resolvedAccount) []activeSecret {
	if e == nil || len(allowedSecrets) == 0 {
		return nil
	}
	active := make([]activeSecret, 0, len(allowedSecrets))
	for _, name := range allowedSecrets {
		rs, ok := e.byName[name]
		if !ok || len(rs.accounts) == 0 {
			continue
		}
		acct := rs.accounts[0]
		if pick, ok := chosen[name]; ok {
			acct = pick
		}
		active = append(active, activeSecret{name: name, placeholder: rs.placeholder, account: acct})
	}
	return active
}

func injectHeaders(hdr http.Header, active []activeSecret, used map[string]resolvedAccount) {
	for k, vals := range hdr {
		isAuth := strings.EqualFold(k, "Authorization")
		for i, v := range vals {
			vals[i] = replaceHeaderValue(v, isAuth, active, used)
		}
	}
}

func replaceHeaderValue(v string, isAuth bool, active []activeSecret, used map[string]resolvedAccount) string {
	if isAuth && strings.HasPrefix(v, "Basic ") {
		if swapped, hit, ok := swapBasicAuth(v, active); ok {
			for name, acct := range hit {
				used[name] = acct
			}
			return swapped
		}
	}
	next := v
	for _, as := range active {
		if strings.Contains(next, as.placeholder) {
			next = strings.ReplaceAll(next, as.placeholder, as.account.value)
			used[as.name] = as.account
		}
		for compPH, compVal := range as.account.companions {
			if strings.Contains(next, compPH) {
				next = strings.ReplaceAll(next, compPH, compVal)
				used[as.name] = as.account
			}
		}
	}
	return next
}

func injectQuery(u *url.URL, active []activeSecret, used map[string]resolvedAccount) {
	if u == nil || u.RawQuery == "" {
		return
	}
	raw := u.RawQuery
	for _, as := range active {
		var hit bool
		raw, hit = replaceQueryPlaceholder(raw, as.placeholder, as.account.value)
		if hit {
			used[as.name] = as.account
		}
		for compPH, compVal := range as.account.companions {
			var ch bool
			raw, ch = replaceQueryPlaceholder(raw, compPH, compVal)
			if ch {
				used[as.name] = as.account
			}
		}
	}
	u.RawQuery = raw
}

func sortedBindings(used map[string]resolvedAccount) []injectedBinding {
	if len(used) == 0 {
		return nil
	}
	names := make([]string, 0, len(used))
	for n := range used {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]injectedBinding, 0, len(names))
	for _, n := range names {
		acct := used[n]
		out = append(out, injectedBinding{secret: n, account: acct.name, label: acct.label})
	}
	return out
}

func replaceQueryPlaceholder(raw, placeholder, value string) (updated string, hit bool) {
	updated = raw
	escPH := url.QueryEscape(placeholder)
	if strings.Contains(updated, placeholder) {
		updated = strings.ReplaceAll(updated, placeholder, url.QueryEscape(value))
		hit = true
	}
	if escPH != placeholder && strings.Contains(updated, escPH) {
		updated = strings.ReplaceAll(updated, escPH, url.QueryEscape(value))
		hit = true
	}
	return updated, hit
}

func swapBasicAuth(headerVal string, active []activeSecret) (swapped string, used map[string]resolvedAccount, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(headerVal, "Basic ")))
	if err != nil {
		return "", nil, false
	}
	next := string(raw)
	used = map[string]resolvedAccount{}
	for _, as := range active {
		if strings.Contains(next, as.placeholder) {
			next = strings.ReplaceAll(next, as.placeholder, as.account.value)
			used[as.name] = as.account
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
	_, scrubbed, err := e.scrubAndCaptureResponse(resp, true)
	return scrubbed, err
}

func (e *SecretEngine) scrubAndCaptureResponse(resp *http.Response, scrub bool) (body []byte, scrubbed bool, err error) {
	return e.captureResponse(resp, scrub, nil)
}

// protoReply is how a proto rule's protobuf replies are cleaned: their real
// secrets scrubbed field by field, and with email set, every email address
// replaced by it.
type protoReply struct{ email string }

// captureResponse reads resp's body for inspection, scrubbing real secrets
// from it and its headers when scrub is set. With proto, a protobuf reply is
// cleaned field by field so its lengths stay true.
func (e *SecretEngine) captureResponse(resp *http.Response, scrub bool, proto *protoReply) (body []byte, scrubbed bool, err error) {
	if resp == nil {
		return nil, false, nil
	}
	canScrub := scrub && e != nil && len(e.all) > 0
	if canScrub {
		scrubbed = e.scrubHeaders(resp.Header)
	}
	if resp.Body == nil || resp.Body == http.NoBody {
		return nil, scrubbed, nil
	}
	out, rerr := readAndCloseBody(resp.Body)
	if rerr != nil {
		return nil, scrubbed, rerr
	}
	if cleaned, ok := e.cleanBody(resp.Header, out, canScrub, proto); ok {
		out, scrubbed = cleaned, true
	}
	resp.Body = io.NopCloser(bytes.NewReader(out))
	resp.ContentLength = int64(len(out))
	resp.Header.Del("Content-Length")
	resp.Header.Del("Transfer-Encoding")
	return out, scrubbed, nil
}

func readAndCloseBody(rc io.ReadCloser) ([]byte, error) {
	raw, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read response body: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close response body: %w", closeErr)
	}
	return raw, nil
}

func (e *SecretEngine) scrubHeaders(hdr http.Header) bool {
	scrubbed := false
	for _, vals := range hdr {
		for i, v := range vals {
			if out, ok := e.ScrubBytes([]byte(v)); ok {
				vals[i] = string(out)
				scrubbed = true
			}
		}
	}
	return scrubbed
}

// cleanBody scrubs a reply body: a protobuf one field by field when proto is
// set, so its lengths stay true, and any other byte for byte.
func (e *SecretEngine) cleanBody(hdr http.Header, body []byte, scrub bool, proto *protoReply) ([]byte, bool) {
	if proto != nil && protoFramingOf(hdr.Get("Content-Type")) != protoNone {
		if !scrub && proto.email == "" {
			return body, false
		}
		cleaned, hit, err := rewriteProtoBody(hdr, body, func(s []byte) ([]byte, bool) {
			return e.scrubProtoString(s, scrub, proto.email)
		})
		return cleaned, err == nil && hit
	}
	if !scrub {
		return body, false
	}
	return e.ScrubBytes(body)
}

// emailAddress is an email address inside a string.
var emailAddress = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// scrubProtoString cleans one string field of a protobuf reply.
func (e *SecretEngine) scrubProtoString(s []byte, scrub bool, email string) ([]byte, bool) {
	out, changed := s, false
	if scrub {
		if cleaned, ok := e.ScrubBytes(out); ok {
			out, changed = cleaned, true
		}
	}
	if email != "" && emailAddress.Match(out) {
		next := emailAddress.ReplaceAllLiteral(out, []byte(email))
		if !bytes.Equal(next, out) {
			out, changed = next, true
		}
	}
	return out, changed
}
