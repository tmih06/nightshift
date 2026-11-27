package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The OAuth parameters every flow sends.
const (
	paramCode         = "code"
	paramVerifier     = "code_verifier"
	paramRedirect     = "redirect_uri"
	paramRefreshToken = "refresh_token"
	paramGrant        = "grant_type"
	paramClientID     = "client_id"
	paramState        = "state"
	grantCode         = "authorization_code"
	grantRefresh      = "refresh_token"
)

// The ids of the sign-in methods the agents share.
const (
	methodBrowser = "browser" // approve in a browser, paste what it ends on
	methodDevice  = "device"  // enter a code on the provider's page
	methodFile    = "file"    // paste a CLI's login file
	methodToken   = "token"   // paste a long-lived token or key
)

// SandboxEmail is the identity a sandbox login names where its CLI wants an
// email.
const SandboxEmail = "nightshift@proxy.local"

// farFuture is the expiry a sandbox login carries, so its CLI never tries to
// renew the placeholder.
const farFuture = "2099-01-01T00:00:00Z"

// pkce is a PKCE verifier and its S256 challenge.
func pkce() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate PKCE: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// randomHex is n random bytes as hex.
func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// codeAndState reads an authorization code and state from what a person
// pasted: the address the browser ended on, a query string, code#state, or a
// bare code.
func codeAndState(input string) (code, state string) {
	v := strings.TrimSpace(input)
	if u, err := url.Parse(v); err == nil && u.Scheme != "" && u.Host != "" {
		q := u.Query()
		if q.Get(paramCode) == "" && u.Fragment != "" {
			if frag, err := url.ParseQuery(u.Fragment); err == nil {
				q = frag
			}
		}
		return q.Get(paramCode), q.Get(paramState)
	}
	if c, s, ok := strings.Cut(v, "#"); ok {
		return c, s
	}
	if strings.Contains(v, "code=") {
		q, err := url.ParseQuery(strings.TrimPrefix(v, "?"))
		if err != nil {
			return "", ""
		}
		return q.Get(paramCode), q.Get(paramState)
	}
	return v, ""
}

// checkCode refuses a paste with no code, or one from another sign-in.
func checkCode(code, state, want string) error {
	if code == "" {
		return errors.New("paste the whole address the browser ended on, or the code the page showed")
	}
	if state != "" && state != want {
		return errors.New("that address belongs to another sign-in; start again")
	}
	return nil
}

// jwtClaims decodes a JWT's payload without checking it: the token came
// straight from the provider over TLS, and only its labels are read.
func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return nil
	}
	return claims
}

// claim is claims[key] as a string.
func claim(claims map[string]any, key string) string {
	if s, ok := claims[key].(string); ok {
		return s
	}
	return ""
}

// jwtExpiry is the exp claim of a JWT, or zero.
func jwtExpiry(token string) time.Time {
	exp, ok := jwtClaims(token)["exp"].(float64)
	if !ok || exp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(exp), 0)
}

// expiresIn is now plus a token reply's expires_in seconds, or zero.
func expiresIn(seconds float64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds * float64(time.Second)))
}

// call is one request to a provider's OAuth endpoint.
type call struct {
	method, url string
	form        url.Values
	json        any
	bearer      string
	headers     map[string]string
}

// newRequest builds c's request.
func newRequest(ctx context.Context, c *call) (*http.Request, error) {
	var body io.Reader
	contentType := ""
	switch {
	case c.form != nil:
		body, contentType = strings.NewReader(c.form.Encode()), "application/x-www-form-urlencoded"
	case c.json != nil:
		raw, err := json.Marshal(c.json)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body, contentType = bytes.NewReader(raw), "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, c.method, c.url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// do sends c and decodes a 2xx JSON reply into out. An error reply names the
// status and the start of its body, which carries no token.
func do(ctx context.Context, hc *http.Client, c call, out any) error {
	req, err := newRequest(ctx, &c)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("reach %s: %w", req.URL.Host, err)
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if cerr := resp.Body.Close(); rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		return fmt.Errorf("read %s: %w", req.URL.Host, rerr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, firstChars(string(raw), 200))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("read %s's reply: %w", req.URL.Host, err)
	}
	return nil
}

// post sends body as JSON and returns the status and reply, whatever the
// status: a device sign-in reads "not yet" from an error status.
func post(ctx context.Context, hc *http.Client, u string, body any) (status int, reply []byte, err error) {
	req, err := newRequest(ctx, &call{method: http.MethodPost, url: u, json: body})
	if err != nil {
		return 0, nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("reach %s: %w", req.URL.Host, err)
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if cerr := resp.Body.Close(); rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		return 0, nil, fmt.Errorf("read %s: %w", req.URL.Host, rerr)
	}
	return resp.StatusCode, raw, nil
}

// deviceWaiting is whether a device sign-in's error reply means "not yet":
// {"error": "authorization_pending"} or {"error": {"code": ...}}, or a
// request to slow down.
func deviceWaiting(raw []byte) bool {
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &body) != nil || len(body.Error) == 0 {
		return false
	}
	var code string
	if json.Unmarshal(body.Error, &code) != nil {
		var obj struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(body.Error, &obj) != nil {
			return false
		}
		code = obj.Code
	}
	switch code {
	case "authorization_pending", "deviceauth_authorization_pending", "slow_down":
		return true
	}
	return false
}

// seconds reads a JSON number of seconds, or a string holding one, falling
// back to def.
func seconds(v any, def time.Duration) time.Duration {
	var n float64
	switch t := v.(type) {
	case float64:
		n = t
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return def
		}
		n = f
	}
	if n <= 0 || n > 600 {
		return def
	}
	return time.Duration(n * float64(time.Second))
}

// firstTime is the first of ts that is set.
func firstTime(ts ...time.Time) time.Time {
	for _, t := range ts {
		if !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func firstChars(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// tokenReply is the usual OAuth token endpoint reply.
type tokenReply struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	IDToken      string  `json:"id_token"`
	ExpiresIn    float64 `json:"expires_in"`
}

// marshal is v as indented JSON with a trailing newline, as the CLIs write
// their own login files.
func marshal(v any) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode login: %w", err)
	}
	return append(raw, '\n'), nil
}
