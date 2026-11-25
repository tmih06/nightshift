package accounts

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestChosenPrefersTheActiveEnabledAccount(t *testing.T) {
	accounts := []Account{
		{Name: "claude-b.json", Provider: "claude"},
		{Name: "claude-a.json", Provider: "claude"},
		{Name: "claude-c.json", Provider: "claude", Disabled: true},
		{Name: "codex-a.json", Provider: "codex"},
	}
	cases := []struct {
		provider, active, want string
		ok                     bool
	}{
		{"claude", "claude-b.json", "claude-b.json", true},
		{"claude", "claude-c.json", "claude-a.json", true}, // disabled: fall back to the first by name
		{"claude", "gone.json", "claude-a.json", true},
		{"claude", "", "claude-a.json", true},
		{"codex", "claude-b.json", "codex-a.json", true}, // another agent's pick is ignored
		{"antigravity", "", "", false},
	}
	for _, c := range cases {
		got, ok := Chosen(accounts, c.provider, c.active)
		if got != c.want || ok != c.ok {
			t.Errorf("Chosen(%s, active=%q) = (%q, %v), want (%q, %v)", c.provider, c.active, got, ok, c.want, c.ok)
		}
	}
}

func TestAccountStateIsTheWorstProblem(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	cases := []struct {
		name string
		acct Account
		want State
	}{
		{"healthy", Account{Expires: later}, StateOK},
		{"about to expire", Account{Expires: now.Add(time.Minute)}, StateExpiring},
		{"refresh failed", Account{Expires: later, RefreshError: "invalid_grant"}, StateFailing},
		{"quota blocked", Account{Expires: later, QuotaBlocked: true}, StateFailing},
		{"must sign in again", Account{Expires: later, NeedsSignIn: true, RefreshError: "x"}, StateSignIn},
		{"disabled beats everything", Account{Disabled: true, NeedsSignIn: true}, StateDisabled},
	}
	for _, c := range cases {
		if got := c.acct.State(now); got != c.want {
			t.Errorf("%s: State = %v, want %v", c.name, got, c.want)
		}
	}
}

// The tab names each agent's egress secret; the publish script and the proxy
// policy name it through the registry. They must agree.
func TestProvidersMatchTheRegistry(t *testing.T) {
	raw, err := os.ReadFile("../../k8s/creds-providers.json")
	if err != nil {
		t.Fatal(err)
	}
	var reg struct {
		Providers []struct {
			Secret string `json:"secret"`
			CPA    struct {
				Type string `json:"type"`
			} `json:"cpa"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, p := range reg.Providers {
		want[p.CPA.Type] = p.Secret
	}
	got := map[string]string{}
	for _, p := range Providers() {
		got[p.ID] = p.Secret
	}
	if len(got) != len(want) {
		t.Fatalf("the tab lists providers %v, the registry %v", got, want)
	}
	for id, secret := range want {
		if got[id] != secret {
			t.Errorf("provider %s: tab secret %q, registry secret %q", id, got[id], secret)
		}
	}
}
