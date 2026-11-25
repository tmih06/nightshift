package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tmih06/nightshift/api"
	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
)

const token = "s3cret-token"

// fake stands behind every interface.
type fake struct {
	mu       sync.Mutex
	err      error
	include  Include
	wait     time.Duration
	screenQ  ScreenQuery
	keys     []v1.Key
	deleted  string
	dropVol  bool
	patched  string
	events   []v1.CheckoutEvent
	fleetsAt int
}

func (f *fake) Fleet(_ context.Context, inc Include) (v1.Fleet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.include = inc
	f.fleetsAt++
	if f.err != nil {
		return v1.Fleet{}, f.err
	}
	return v1.Fleet{ObservedAt: time.Now().UTC(), Pods: []v1.Pod{{Name: "pi-1", Namespace: "nightshift", Role: v1.RoleWarm, Phase: "Running"}}, Pools: []v1.Pool{}}, nil
}

func (f *fake) PodLogs(_ context.Context, _, pod string, _ int) ([]string, error) {
	return []string{"line"}, f.err
}

func (f *fake) Claim(_ context.Context, pool string) (v1.Claim, error) {
	return v1.Claim{Name: "dash-claim-1", Pool: pool}, f.err
}

func (f *fake) Claimed(_ context.Context, name string, wait time.Duration) (v1.Claim, error) {
	f.mu.Lock()
	f.wait = wait
	f.mu.Unlock()
	return v1.Claim{Name: name, Pool: "pi-pool", Sandbox: "pi-abc"}, f.err
}

func (f *fake) DeleteClaim(_ context.Context, name string) error { f.deleted = name; return f.err }

func (f *fake) Start(req v1.CheckoutRequest) (v1.Checkout, error) {
	if why := req.Problem(); why != "" {
		return v1.Checkout{}, &checkout.InvalidError{Reason: why}
	}
	return v1.Checkout{ID: "co-1", Target: req.Target, Pool: req.Pool, Volume: "pr-acme-rocket-7", State: v1.CheckoutRunning, StartedAt: time.Now().UTC()}, f.err
}

func (f *fake) Get(id string) (v1.Checkout, error) {
	if id != "co-1" {
		return v1.Checkout{}, checkout.ErrNotFound
	}
	return v1.Checkout{ID: id, State: v1.CheckoutDone, StartedAt: time.Now().UTC()}, nil
}

func (f *fake) Follow(_ context.Context, _ string, fn func(v1.CheckoutEvent) error) error {
	for _, ev := range f.events {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return nil
}

func (f *fake) Volumes(context.Context) ([]v1.Volume, error) { return nil, f.err }
func (f *fake) DeleteVolume(_ context.Context, name string) error {
	f.deleted = name
	return f.err
}

func (f *fake) DeleteSandbox(_ context.Context, name string, drop bool) error {
	f.deleted, f.dropVol = name, drop
	return f.err
}

func (f *fake) Terminals(context.Context, string) ([]v1.Terminal, error) { return nil, f.err }
func (f *fake) OpenTerminal(_ context.Context, _ string, req v1.TerminalRequest) (v1.Terminal, error) {
	return v1.Terminal{Address: "t1", Name: req.Name, CreatedAt: time.Now().UTC()}, f.err
}
func (f *fake) CloseTerminal(context.Context, string, string) error { return f.err }
func (f *fake) Screen(_ context.Context, _, _ string, q ScreenQuery) (v1.Screen, error) {
	f.mu.Lock()
	f.screenQ = q
	f.mu.Unlock()
	return v1.Screen{Changed: true, Cols: q.Cols, Rows: q.Rows, Lines: []string{"$ "}}, f.err
}

func (f *fake) Input(_ context.Context, _, _ string, keys []v1.Key) error {
	f.keys = keys
	return f.err
}

func (f *fake) Accounts(context.Context) (v1.Accounts, error) {
	return v1.Accounts{ObservedAt: time.Now().UTC(), Providers: []v1.Provider{{ID: "claude", Name: "Claude Code"}}, Accounts: []v1.Account{}}, f.err
}

func (f *fake) StartLogin(_ context.Context, p string) (v1.Login, error) {
	return v1.Login{ID: "st1", Provider: p, URL: "https://example.com/auth"}, f.err
}
func (f *fake) CompleteLogin(context.Context, string, v1.LoginCompletion) error { return f.err }
func (f *fake) CancelLogin(context.Context, string, string) error               { return f.err }
func (f *fake) SetDisabled(_ context.Context, name string, _ bool) error {
	f.patched = name
	return f.err
}
func (f *fake) Remove(context.Context, string) error            { return f.err }
func (f *fake) Refresh(context.Context, string) error           { return f.err }
func (f *fake) SetActive(context.Context, string, string) error { return f.err }
func (f *fake) Publish(context.Context) (string, error)         { return "published", f.err }

func newTestServer(t *testing.T, f *fake, mutate ...func(*Config)) *httptest.Server {
	t.Helper()
	cfg := Config{
		Token: token, Version: "test", WatchInterval: 10 * time.Millisecond,
		Cluster: v1.ClusterInfo{Context: "kind-test", Namespace: "nightshift"},
		Fleet:   f, Claims: f, Checkouts: f, Volumes: f, Terminals: f, Accounts: f,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func call(t *testing.T, ts *httptest.Server, method, path, body string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			req.Host = hdr[i+1] // net/http sends this, not the header
			continue
		}
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

// strict decodes raw into v and fails on any field v does not have: a response
// that carries something the contract does not name is a contract change.
func strict(t *testing.T, raw []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("response does not fit the contract: %v\n%s", err, raw)
	}
}

func problem(t *testing.T, resp *http.Response, raw []byte, status int, code string) {
	t.Helper()
	var p v1.Problem
	strict(t, raw, &p)
	if resp.StatusCode != status || p.Code != code || p.Status != status || p.Title == "" {
		t.Fatalf("got %d %+v, want %d %s", resp.StatusCode, p, status, code)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content type = %q", ct)
	}
}

func TestEveryRouteIsInTheContractAndTheOtherWayRound(t *testing.T) {
	s, err := New(Config{Token: token})
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	var want []string
	for path, item := range spec.Paths {
		for method := range item {
			if method != "parameters" {
				want = append(want, strings.ToUpper(method)+" "+path)
			}
		}
	}
	got := s.Routes()
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the server and api/openapi.yaml disagree.\nserver:\n%s\ncontract:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestAServerWithoutATokenDoesNotExist(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a server with no token would be open to anyone who can reach it")
	}
}

func TestTheTokenIsRequiredEverywhereButHealth(t *testing.T) {
	ts := newTestServer(t, &fake{})
	for _, auth := range []string{"", "Bearer wrong", token, "Basic " + token, "Bearer "} {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+"/v1/fleet", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		problem(t, resp, raw, 401, v1.CodeUnauthorized)
		if resp.Header.Get("WWW-Authenticate") != "Bearer" {
			t.Errorf("no WWW-Authenticate for %q", auth)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), "GET", ts.URL+"/v1/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("healthz needs no token, got %d", resp.StatusCode)
	}
}

func TestOnlyKnownHostsAreServed(t *testing.T) {
	ts := newTestServer(t, &fake{})
	resp, raw := call(t, ts, "GET", "/v1", "", "Host", "evil.example.com")
	_ = raw
	if resp.StatusCode != 403 {
		t.Errorf("a rebinding Host got %d", resp.StatusCode)
	}
	ts = newTestServer(t, &fake{}, func(c *Config) { c.Hosts = []string{"nightshift.example.com"} })
	if resp, _ := call(t, ts, "GET", "/v1", "", "Host", "nightshift.example.com:7777"); resp.StatusCode != 200 {
		t.Errorf("a configured Host got %d", resp.StatusCode)
	}
	ts = newTestServer(t, &fake{}, func(c *Config) { c.AnyHost = true })
	if resp, _ := call(t, ts, "GET", "/v1", "", "Host", "anything.test"); resp.StatusCode != 200 {
		t.Errorf("AnyHost got %d", resp.StatusCode)
	}
}

func TestInfoListsWhatTheServerOffers(t *testing.T) {
	ts := newTestServer(t, &fake{})
	resp, raw := call(t, ts, "GET", "/v1", "")
	var info v1.Info
	strict(t, raw, &info)
	if resp.StatusCode != 200 || info.APIVersion != "v1" || info.Cluster.Context != "kind-test" || !info.Has(v1.CapTerminals) || !info.Has(v1.CapAccounts) {
		t.Errorf("info = %+v", info)
	}
	ts = newTestServer(t, &fake{}, func(c *Config) { c.Accounts, c.Terminals = nil, nil })
	_, raw = call(t, ts, "GET", "/v1", "")
	info = v1.Info{}
	strict(t, raw, &info)
	if info.Has(v1.CapAccounts) || info.Has(v1.CapTerminals) || !info.Has(v1.CapCheckouts) {
		t.Errorf("capabilities = %v", info.Capabilities)
	}
	resp, raw = call(t, ts, "GET", "/v1/accounts", "")
	problem(t, resp, raw, 501, v1.CodeUnavailable)
}

func TestTheContractIsServedAsRunning(t *testing.T) {
	ts := newTestServer(t, &fake{})
	resp, raw := call(t, ts, "GET", "/v1/openapi.yaml", "")
	if resp.StatusCode != 200 || string(raw) != string(api.OpenAPI) {
		t.Errorf("status %d, %d bytes", resp.StatusCode, len(raw))
	}
}

func TestFleetPassesWhatToGatherAndRefusesWhatItDoesNotKnow(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	resp, raw := call(t, ts, "GET", "/v1/fleet?include=live,versions", "")
	var fleet v1.Fleet
	strict(t, raw, &fleet)
	if resp.StatusCode != 200 || len(fleet.Pods) != 1 || !f.include.Live || !f.include.Versions || f.include.Configs {
		t.Errorf("status %d include %+v", resp.StatusCode, f.include)
	}
	resp, raw = call(t, ts, "GET", "/v1/fleet?include=secrets", "")
	problem(t, resp, raw, 400, v1.CodeInvalid)
}

func TestBackendErrorsKeepTheContractsCodes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{errors.New("kubectl get: exit 1: Error from server (NotFound): pods \"x\" not found"), 404, v1.CodeNotFound},
		{errors.New("Error from server (AlreadyExists): already there"), 409, v1.CodeConflict},
		{errors.New("Unable to connect to the server: dial tcp: connection refused"), 503, v1.CodeUnavailable},
		{context.DeadlineExceeded, 504, v1.CodeTimeout},
		{errors.New("clone job j failed: boom"), 500, v1.CodeInternal},
	} {
		ts := newTestServer(t, &fake{err: tc.err})
		resp, raw := call(t, ts, "GET", "/v1/fleet", "")
		problem(t, resp, raw, tc.status, tc.code)
	}
}

func TestFleetWatchSendsTheFleetOnceAndAgainOnlyWhenItChanges(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/v1/fleet/watch", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	var events []string
	for sc.Scan() && len(events) < 1 {
		if name, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			events = append(events, name)
		}
	}
	time.Sleep(100 * time.Millisecond) // several ticks with nothing new
	f.mu.Lock()
	looks := f.fleetsAt
	f.mu.Unlock()
	cancel()
	if len(events) != 1 || events[0] != "fleet" {
		t.Errorf("events = %v", events)
	}
	if looks < 3 {
		t.Errorf("the server looked %d times; it should keep looking", looks)
	}
}

func TestClaimsWaitIsBoundedAndPassedOn(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	resp, raw := call(t, ts, "POST", "/v1/claims", `{"pool":"codex-pool","unknown":1}`)
	var c v1.Claim
	strict(t, raw, &c)
	if resp.StatusCode != 202 || c.Pool != "codex-pool" {
		t.Errorf("create = %d %+v", resp.StatusCode, c)
	}
	resp, raw = call(t, ts, "POST", "/v1/claims", `{}`)
	problem(t, resp, raw, 400, v1.CodeInvalid)
	resp, raw = call(t, ts, "GET", "/v1/claims/c1?wait=30", "")
	strict(t, raw, &c)
	if resp.StatusCode != 200 || !c.Bound() || f.wait != 30*time.Second {
		t.Errorf("get = %d %+v wait %v", resp.StatusCode, c, f.wait)
	}
	resp, raw = call(t, ts, "GET", "/v1/claims/c1?wait=9999", "")
	problem(t, resp, raw, 400, v1.CodeInvalid)
	if resp, _ := call(t, ts, "DELETE", "/v1/claims/c1", ""); resp.StatusCode != 204 || f.deleted != "c1" {
		t.Errorf("delete = %d, deleted %q", resp.StatusCode, f.deleted)
	}
}

func TestCheckoutsAreValidatedAndFollowedAsAStream(t *testing.T) {
	f := &fake{events: []v1.CheckoutEvent{
		{Step: v1.StepVolume, Detail: "creating", At: time.Now().UTC()},
		{Step: v1.StepShell, Sandbox: "pi-1", Done: true, At: time.Now().UTC()},
	}}
	ts := newTestServer(t, f)
	resp, raw := call(t, ts, "POST", "/v1/checkouts", `{"target":{"kind":"pull_request","repo":"acme/rocket","number":7},"pool":"pi-pool"}`)
	var co v1.Checkout
	strict(t, raw, &co)
	if resp.StatusCode != 202 || co.ID != "co-1" {
		t.Errorf("start = %d %+v", resp.StatusCode, co)
	}
	resp, raw = call(t, ts, "POST", "/v1/checkouts", `{"target":{"kind":"pull_request","repo":"acme/rocket"},"pool":"pi-pool"}`)
	problem(t, resp, raw, 400, v1.CodeInvalid)
	resp, raw = call(t, ts, "GET", "/v1/checkouts/nope/events", "")
	problem(t, resp, raw, 404, v1.CodeNotFound)

	resp, raw = call(t, ts, "GET", "/v1/checkouts/co-1/events", "")
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
	}
	var got []v1.CheckoutEvent
	for _, block := range strings.Split(strings.TrimSpace(string(raw)), "\n\n") {
		name, data, ok := strings.Cut(block, "\ndata: ")
		if !ok || name != "event: checkout" {
			t.Fatalf("block %q", block)
		}
		var ev v1.CheckoutEvent
		strict(t, []byte(data), &ev)
		got = append(got, ev)
	}
	if len(got) != 2 || !got[1].Done || got[1].Sandbox != "pi-1" {
		t.Errorf("events = %+v", got)
	}
}

func TestSandboxDeletionSaysWhetherTheVolumeGoes(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	if resp, _ := call(t, ts, "DELETE", "/v1/sandboxes/pi-pr-1", ""); resp.StatusCode != 204 || f.dropVol {
		t.Errorf("the volume is kept by default: status %d drop %v", resp.StatusCode, f.dropVol)
	}
	if resp, _ := call(t, ts, "DELETE", "/v1/sandboxes/pi-pr-1?deleteVolume=true", ""); resp.StatusCode != 204 || !f.dropVol {
		t.Errorf("status %d drop %v", resp.StatusCode, f.dropVol)
	}
	resp, raw := call(t, ts, "DELETE", "/v1/sandboxes/pi-pr-1?deleteVolume=maybe", "")
	problem(t, resp, raw, 400, v1.CodeInvalid)
	resp, raw = call(t, ts, "GET", "/v1/volumes", "")
	var vols v1.Volumes
	strict(t, raw, &vols)
	if resp.StatusCode != 200 || vols.Volumes == nil {
		t.Errorf("an empty list must be [], not null: %s", raw)
	}
}

func TestScreensNeedASizeAndAWellFormedViewer(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	base := "/v1/sandboxes/pi-1/terminals/t1/screen"
	resp, raw := call(t, ts, "GET", base+"?cols=80&rows=24&wait=500&viewer=tui-1&resize=true", "")
	var sc v1.Screen
	strict(t, raw, &sc)
	q := f.screenQ
	if resp.StatusCode != 200 || !sc.Changed || q.Cols != 80 || q.Rows != 24 || q.Wait != 500*time.Millisecond || q.Viewer != "tui-1" || !q.Resize {
		t.Errorf("status %d query %+v", resp.StatusCode, q)
	}
	for _, bad := range []string{"", "?cols=80", "?cols=0&rows=1", "?cols=80&rows=24&wait=99999", "?cols=80&rows=24&viewer=a/b", "?cols=80&rows=24&resize=sure"} {
		resp, raw := call(t, ts, "GET", base+bad, "")
		problem(t, resp, raw, 400, v1.CodeInvalid)
	}
}

func TestInputArrivesInOrder(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	resp, _ := call(t, ts, "POST", "/v1/sandboxes/pi-1/terminals/t1/input", `{"keys":[{"text":"ls"},{"name":"Enter"}]}`)
	if resp.StatusCode != 204 || len(f.keys) != 2 || f.keys[0].Text != "ls" || f.keys[1].Name != "Enter" {
		t.Errorf("status %d keys %+v", resp.StatusCode, f.keys)
	}
	resp, raw := call(t, ts, "POST", "/v1/sandboxes/pi-1/terminals/t1/input", "not json")
	problem(t, resp, raw, 400, v1.CodeInvalid)
	resp, raw = call(t, ts, "POST", "/v1/sandboxes/pi-1/terminals", "")
	var term v1.Terminal
	strict(t, raw, &term)
	if resp.StatusCode != 201 || term.Address != "t1" {
		t.Errorf("a terminal opens without a body: %d %s", resp.StatusCode, raw)
	}
}

func TestAccountsActionsAndTheirChecks(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	resp, raw := call(t, ts, "POST", "/v1/accounts/logins", `{"provider":"claude"}`)
	var login v1.Login
	strict(t, raw, &login)
	if resp.StatusCode != 201 || login.ID != "st1" {
		t.Errorf("login = %d %s", resp.StatusCode, raw)
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/v1/accounts/logins/st1/complete", `{"provider":"claude"}`},
		{"DELETE", "/v1/accounts/logins/st1", ""},
		{"PATCH", "/v1/accounts/a.json", `{}`},
		{"PUT", "/v1/accounts/active/claude", `{}`},
	} {
		resp, raw := call(t, ts, tc.method, tc.path, tc.body)
		problem(t, resp, raw, 400, v1.CodeInvalid)
	}
	if resp, _ := call(t, ts, "PATCH", "/v1/accounts/a.json", `{"disabled":true}`); resp.StatusCode != 204 || f.patched != "a.json" {
		t.Errorf("patch = %d %q", resp.StatusCode, f.patched)
	}
	resp, raw = call(t, ts, "POST", "/v1/accounts/publish", "")
	var rep v1.PublishReport
	strict(t, raw, &rep)
	if resp.StatusCode != 200 || rep.Report != "published" {
		t.Errorf("publish = %d %s", resp.StatusCode, raw)
	}
}

func TestNoResponseEchoesTheToken(t *testing.T) {
	ts := newTestServer(t, &fake{err: errors.New("boom")})
	for _, p := range []string{"/v1", "/v1/fleet", "/v1/accounts", "/v1/nope"} {
		_, raw := call(t, ts, "GET", p, "")
		if strings.Contains(string(raw), token) {
			t.Errorf("%s echoed the token", p)
		}
	}
}

func TestAnUnknownPathIsAProblemNotAnHTMLPage(t *testing.T) {
	ts := newTestServer(t, &fake{})
	resp, raw := call(t, ts, "GET", "/v1/nope", "")
	problem(t, resp, raw, 404, v1.CodeNotFound)
}
