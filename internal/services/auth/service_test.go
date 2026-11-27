package auth

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tmih06/nightshift/internal/services/auth/providers"
)

// fakeCluster records what publishing writes.
type fakeCluster struct {
	mu      sync.Mutex
	secrets map[string]map[string][]byte // "ns/name" -> data
	pools   map[string]int
	fail    error
}

func newFakeCluster(pools ...string) *fakeCluster {
	c := &fakeCluster{secrets: map[string]map[string][]byte{}, pools: map[string]int{}}
	for _, p := range pools {
		c.pools[p] = 1
	}
	return c
}

func (c *fakeCluster) MergeSecret(_ context.Context, ns, name string, data map[string][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail != nil {
		return c.fail
	}
	key := ns + "/" + name
	if c.secrets[key] == nil {
		c.secrets[key] = map[string][]byte{}
	}
	for k, v := range data {
		if v == nil {
			delete(c.secrets[key], k)
			continue
		}
		c.secrets[key][k] = v
	}
	return nil
}

func (c *fakeCluster) WarmPools(context.Context, string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.pools))
	for p := range c.pools {
		out = append(out, p)
	}
	return out, nil
}

func (c *fakeCluster) ScalePool(_ context.Context, _, name string, replicas int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pools[name] = replicas
	return nil
}

func (c *fakeCluster) egress(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.secrets["nightshift-egress/egress-secrets"][key])
}

// files is the agent-creds tar as path -> body.
func (c *fakeCluster) files(t *testing.T) map[string]string {
	t.Helper()
	c.mu.Lock()
	raw := c.secrets["nightshift/agent-creds"]["creds.tar"]
	c.mu.Unlock()
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Mode != 0o600 {
			t.Errorf("%s has mode %o, want 0600", h.Name, h.Mode)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[h.Name] = string(body)
	}
}

// fakeProvider is an agent whose sign-in takes the pasted text as its token
// ("bad" is refused) and whose renewal appends "+". Its device sign-in is
// approved on the second ask, as TOKEN-D.
type fakeProvider struct {
	id      string
	renewal error
	asks    int
}

func (f *fakeProvider) Info() providers.Info {
	return providers.Info{
		ID: f.id, Name: f.id, Secret: strings.ToUpper(f.id) + "_TOKEN", Pool: f.id + "-pool", LoginFile: "." + f.id + "/login",
		Methods: []providers.Method{
			{ID: "browser", Label: "browser", Kind: providers.KindBrowser},
			{ID: "device", Label: "device", Kind: providers.KindDevice},
		},
	}
}

func (f *fakeProvider) Begin(_ context.Context, method string) (providers.Pending, error) {
	m := f.Info().Methods[0]
	if method == "device" {
		m = f.Info().Methods[1]
	}
	return providers.Pending{Provider: f.id, Method: m, URL: "https://example.com/" + f.id, State: "s"}, nil
}

func (f *fakeProvider) Finish(_ context.Context, p providers.Pending, input string) (providers.Credential, error) {
	if p.Method.Kind == providers.KindDevice {
		f.asks++
		if f.asks < 2 {
			return providers.Credential{}, providers.ErrPending
		}
		input = "TOKEN-D d@example.com"
	}
	if input == "bad" {
		return providers.Credential{}, errors.New("not a code")
	}
	token, email, _ := strings.Cut(input, " ")
	return providers.Credential{Access: token, Refresh: "r-" + token, Email: email, Expires: time.Now().Add(time.Hour)}, nil
}

func (f *fakeProvider) Refresh(_ context.Context, c providers.Credential) (providers.Credential, error) {
	if f.renewal != nil {
		return providers.Credential{}, f.renewal
	}
	c.Access += "+"
	c.Expires = time.Now().Add(time.Hour)
	return c, nil
}

func (f *fakeProvider) SandboxLogin(_ providers.Credential, placeholder string) ([]byte, error) {
	return []byte("token=" + placeholder), nil
}

func (f *fakeProvider) Companions(providers.Credential) map[string]string { return nil }

func newTestService(t *testing.T, cluster Cluster, list ...providers.Provider) *Service {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(store, list, cluster, Config{Namespaces: Namespaces{Sandboxes: "nightshift", Egress: "nightshift-egress"}})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// signIn signs an alpha account in with input.
func signIn(t *testing.T, svc *Service, input string) string {
	t.Helper()
	_, id, err := svc.StartLogin(t.Context(), "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	name, err := svc.CompleteLogin(t.Context(), id, input)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// A mistyped paste must not end the sign-in: the person fixes it and tries
// again with the same link, and only a finished sign-in is gone.
func TestAFailedAnswerLeavesTheSignInOpen(t *testing.T) {
	svc := newTestService(t, newFakeCluster("alpha-pool"), &fakeProvider{id: "alpha"})
	_, id, err := svc.StartLogin(t.Context(), "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteLogin(t.Context(), id, "bad"); err == nil {
		t.Fatal("a bad paste signed in")
	}
	if _, err := svc.CompleteLogin(t.Context(), id, "TOKEN-1 a@example.com"); err != nil {
		t.Fatalf("the second try: %v", err)
	}
	if _, err := svc.CompleteLogin(t.Context(), id, "TOKEN-1 a@example.com"); !errors.Is(err, ErrUnknownLogin) {
		t.Errorf("a finished sign-in answered again: %v", err)
	}
}

func TestADeviceSignInWaitsForApproval(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	svc := newTestService(t, cluster, &fakeProvider{id: "alpha"})
	pend, id, err := svc.StartLogin(t.Context(), "alpha", "device")
	if err != nil || pend.Method.Kind != providers.KindDevice {
		t.Fatalf("pending = %+v, %v", pend, err)
	}
	if _, err := svc.CompleteLogin(t.Context(), id, ""); !errors.Is(err, providers.ErrPending) {
		t.Fatalf("an unapproved device sign-in answered %v", err)
	}
	name, err := svc.CompleteLogin(t.Context(), id, "")
	if err != nil || name != "alpha-d@example.com" {
		t.Fatalf("approved: %q, %v", name, err)
	}
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "TOKEN-D" {
		t.Errorf("REAL_ALPHA_TOKEN = %q", got)
	}
}

// A sign-in reaches the cluster at once: the real token only to the egress
// proxy, a placeholder login to the sandboxes, and a warm pool for the agent.
func TestASignInIsPublishedToTheProxyAndTheSandboxes(t *testing.T) {
	cluster := newFakeCluster("alpha-pool", "beta-pool", "pi-pool")
	svc := newTestService(t, cluster, &fakeProvider{id: "alpha"}, &fakeProvider{id: "beta"})
	name := signIn(t, svc, "TOKEN-1 a@example.com")
	if name != "alpha-a@example.com" {
		t.Errorf("account name = %q", name)
	}
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "TOKEN-1" {
		t.Errorf("REAL_ALPHA_TOKEN = %q", got)
	}
	if got := cluster.egress("LABEL_ALPHA_TOKEN"); got != "a@example.com" {
		t.Errorf("LABEL_ALPHA_TOKEN = %q", got)
	}
	var pool []poolEntry
	if err := json.Unmarshal([]byte(cluster.egress("POOL_ALPHA_TOKEN")), &pool); err != nil || len(pool) != 1 || pool[0].Token != "TOKEN-1" {
		t.Errorf("POOL_ALPHA_TOKEN = %s (%v)", cluster.egress("POOL_ALPHA_TOKEN"), err)
	}
	files := cluster.files(t)
	if files["./.alpha/login"] != "token=nightshift:secret:ALPHA_TOKEN" || len(files) != 1 {
		t.Errorf("agent-creds = %v", files)
	}
	for blob, body := range files {
		if strings.Contains(body, "TOKEN-1") || strings.Contains(body, "example.com") {
			t.Errorf("%s carries the real login: %s", blob, body)
		}
	}
	if cluster.pools["alpha-pool"] != 1 || cluster.pools["beta-pool"] != 0 || cluster.pools["pi-pool"] != 0 {
		t.Errorf("pools = %v: only an agent with an account stays warm", cluster.pools)
	}
	for _, a := range svc.View().Accounts {
		if a.Credential.Access != "" || a.Credential.Refresh != "" {
			t.Errorf("the view carries tokens: %+v", a)
		}
	}
}

func TestTheChainOrdersThePoolAndSignInAgainReplacesTokens(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	svc := newTestService(t, cluster, &fakeProvider{id: "alpha"})
	a := signIn(t, svc, "T-A a@example.com")
	b := signIn(t, svc, "T-B b@example.com")
	if err := svc.SetChain(t.Context(), "alpha", []string{b, a}); err != nil {
		t.Fatal(err)
	}
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "T-B" {
		t.Errorf("primary token = %q, want the chain's first", got)
	}
	if again := signIn(t, svc, "T-A2 a@example.com"); again != a || len(svc.View().Accounts) != 2 {
		t.Errorf("signing a in again made %q and %d accounts", again, len(svc.View().Accounts))
	}
	if err := svc.SetDisabled(t.Context(), b, true); err != nil {
		t.Fatal(err)
	}
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "T-A2" {
		t.Errorf("primary after disabling b = %q", got)
	}
	if err := svc.Remove(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "" || cluster.pools["alpha-pool"] != 0 {
		t.Errorf("with no enabled account the token is %q and the pool %d", got, cluster.pools["alpha-pool"])
	}
}

// A token near its expiry is renewed, the new refresh token saved before it
// is used, and the new access token published.
func TestThePassRenewsATokenBeforeItLapsesAndSavesIt(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	prov := &fakeProvider{id: "alpha"}
	svc := newTestService(t, cluster, prov)
	name := signIn(t, svc, "T a@example.com")
	svc.mu.Lock()
	svc.find(name).Credential.Expires = time.Now().Add(time.Minute)
	svc.mu.Unlock()
	svc.pass(t.Context())
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "T+" {
		t.Errorf("published token = %q, want the renewed one", got)
	}
	reloaded, err := svc.store.Load()
	if err != nil || reloaded.Accounts[0].Credential.Access != "T+" {
		t.Errorf("the vault holds %+v (%v)", reloaded.Accounts, err)
	}
}

// A login that can no longer be renewed is taken out of the chain, so the
// proxy stops sending a dead token, and asks to be signed in again.
func TestALoginThatCannotRenewAsksForSignIn(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	prov := &fakeProvider{id: "alpha"}
	svc := newTestService(t, cluster, prov)
	name := signIn(t, svc, "T a@example.com")
	prov.renewal = providers.ErrNoRefresh
	if err := svc.Refresh(t.Context(), name); !errors.Is(err, providers.ErrNoRefresh) {
		t.Fatalf("refresh error = %v", err)
	}
	if a := svc.View().Accounts[0]; !a.NeedsSignIn {
		t.Errorf("account = %+v, want it to need a sign-in", a)
	}
	if cluster.pools["alpha-pool"] != 0 || cluster.egress("REAL_ALPHA_TOKEN") != "" {
		t.Errorf("the dead login is still published: pool %d", cluster.pools["alpha-pool"])
	}
}

// A failed renewal waits before trying again, instead of asking the provider
// every minute.
func TestAFailedRenewalWaitsBeforeTryingAgain(t *testing.T) {
	cluster := newFakeCluster()
	prov := &fakeProvider{id: "alpha", renewal: errors.New("provider down")}
	svc := newTestService(t, cluster, prov)
	name := signIn(t, svc, "T a@example.com")
	svc.mu.Lock()
	svc.find(name).Credential.Expires = time.Now().Add(time.Minute)
	svc.mu.Unlock()
	svc.pass(t.Context())
	svc.pass(t.Context())
	if a := svc.View().Accounts[0]; a.Failures != 1 || a.RefreshError == "" {
		t.Errorf("account = %+v, want one failed try", a)
	}
}

func TestAPublishThatFailsIsRetriedOnTheNextPass(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	cluster.fail = errors.New("api server away")
	svc := newTestService(t, cluster, &fakeProvider{id: "alpha"})
	signIn(t, svc, "T a@example.com")
	cluster.mu.Lock()
	cluster.fail = nil
	cluster.mu.Unlock()
	svc.pass(t.Context())
	if got := cluster.egress("REAL_ALPHA_TOKEN"); got != "T" {
		t.Errorf("token after the retry = %q", got)
	}
}

func TestTheAPINeedsTheKeyAndNeverReturnsATokenAndSignsIn(t *testing.T) {
	cluster := newFakeCluster("alpha-pool")
	svc := newTestService(t, cluster, &fakeProvider{id: "alpha"})
	srv := httptest.NewServer(svc.Handler("the-key"))
	defer srv.Close()
	call := func(method, path, key, body string) (int, string) {
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(raw)
	}
	if status, _ := call(http.MethodGet, "/v1/accounts", "wrong", ""); status != http.StatusUnauthorized {
		t.Errorf("a wrong key got %d", status)
	}
	status, body := call(http.MethodPost, "/v1/logins", "the-key", `{"provider":"alpha"}`)
	var login LoginJSON
	if status != http.StatusCreated || json.Unmarshal([]byte(body), &login) != nil || login.URL == "" {
		t.Fatalf("start login: %d %s", status, body)
	}
	if status, body := call(http.MethodPost, "/v1/logins/"+login.ID, "the-key", `{"input":"SECRET-TOKEN a@example.com"}`); status != http.StatusOK {
		t.Fatalf("complete login: %d %s", status, body)
	}
	status, body = call(http.MethodGet, "/v1/accounts", "the-key", "")
	if status != http.StatusOK || !strings.Contains(body, "a@example.com") || strings.Contains(body, "SECRET-TOKEN") {
		t.Errorf("accounts: %d %s", status, body)
	}
	if status, _ := call(http.MethodPost, "/v1/logins/"+login.ID, "the-key", `{"input":"x"}`); status != http.StatusNotFound {
		t.Errorf("a finished sign-in can be finished again: %d", status)
	}
	if status, _ := call(http.MethodGet, "/healthz", "", ""); status != http.StatusNoContent {
		t.Errorf("/healthz = %d", status)
	}
}
