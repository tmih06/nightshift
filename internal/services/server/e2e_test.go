package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tmih06/nightshift/api/client"
	v1 "github.com/tmih06/nightshift/api/v1"
)

// These tests run the Go client against the real server, so the SDK and the
// server cannot drift apart.

func newClient(t *testing.T, ts *httptest.Server, tok string) *client.Client {
	t.Helper()
	c, err := client.New(ts.URL, tok)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTheClientDrivesEveryEndpoint(t *testing.T) {
	f := &fake{events: []v1.CheckoutEvent{
		{Step: v1.StepClone, Detail: "Receiving objects:  45% (1/2)", At: time.Now().UTC()},
		{Step: v1.StepShell, Sandbox: "pi-1", Done: true, At: time.Now().UTC()},
	}}
	ts := newTestServer(t, f)
	c := newClient(t, ts, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Healthy(ctx); err != nil {
		t.Fatal(err)
	}
	info, err := c.Info(ctx)
	if err != nil || !info.Has(v1.CapTerminals) {
		t.Fatalf("Info = %+v, %v", info, err)
	}
	if fleet, err := c.Fleet(ctx, v1.IncludeLive, v1.IncludeVersions); err != nil || len(fleet.Pods) != 1 || !f.include.Live || !f.include.Versions {
		t.Fatalf("Fleet = %+v, %v, include %+v", fleet, err, f.include)
	}
	if lines, err := c.PodLogs(ctx, "nightshift", "pi-1", 50); err != nil || len(lines) != 1 {
		t.Fatalf("PodLogs = %v, %v", lines, err)
	}

	claim, err := c.Claim(ctx, "pi-pool")
	if err != nil || claim.Pool != "pi-pool" {
		t.Fatalf("Claim = %+v, %v", claim, err)
	}
	if got, err := c.WaitClaim(ctx, claim.Name, 5*time.Second); err != nil || !got.Bound() || f.wait != 5*time.Second {
		t.Fatalf("WaitClaim = %+v, %v, wait %v", got, err, f.wait)
	}
	if err := c.DeleteClaim(ctx, claim.Name); err != nil || f.deleted != claim.Name {
		t.Fatalf("DeleteClaim: %v, deleted %q", err, f.deleted)
	}

	co, err := c.Checkout(ctx, v1.CheckoutRequest{Target: v1.Target{Kind: v1.TargetPullRequest, Repo: "acme/rocket", Number: 7}, Pool: "pi-pool"})
	if err != nil || co.ID != "co-1" {
		t.Fatalf("Checkout = %+v, %v", co, err)
	}
	var got []v1.CheckoutEvent
	if err := c.FollowCheckout(ctx, co.ID, func(ev v1.CheckoutEvent) error { got = append(got, ev); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[1].Done || got[1].Sandbox != "pi-1" {
		t.Errorf("FollowCheckout events = %+v", got)
	}
	if _, err := c.GetCheckout(ctx, co.ID); err != nil {
		t.Errorf("GetCheckout: %v", err)
	}
	if vols, err := c.Volumes(ctx); err != nil || vols == nil {
		t.Errorf("Volumes = %v, %v", vols, err)
	}
	if err := c.DeleteSandbox(ctx, "pi-1", true); err != nil || !f.dropVol {
		t.Errorf("DeleteSandbox: %v, drop %v", err, f.dropVol)
	}
	if err := c.DeleteVolume(ctx, "pr-acme-rocket-7"); err != nil || f.deleted != "pr-acme-rocket-7" {
		t.Errorf("DeleteVolume: %v", err)
	}
}

func TestTheClientDrivesTerminalsAndAccounts(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	c := newClient(t, ts, token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if terms, err := c.Terminals(ctx, "pi-1"); err != nil || terms != nil && len(terms) != 0 {
		t.Fatalf("Terminals = %v, %v", terms, err)
	}
	term, err := c.OpenTerminal(ctx, "pi-1", v1.TerminalRequest{})
	if err != nil || term.Address != "t1" {
		t.Fatalf("OpenTerminal = %+v, %v", term, err)
	}
	if err := c.Input(ctx, "pi-1", "t1", []v1.Key{{Text: "ls"}, {Name: "Enter"}}); err != nil || len(f.keys) != 2 {
		t.Fatalf("Input: %v, keys %+v", err, f.keys)
	}
	sc, err := c.Screen(ctx, "pi-1", "t1", 80, 24, true, time.Second)
	if err != nil || !sc.Changed || f.screenQ.Viewer == "" || f.screenQ.Cols != 80 || !f.screenQ.Resize {
		t.Fatalf("Screen = %+v, %v, query %+v", sc, err, f.screenQ)
	}
	if err := c.CloseTerminal(ctx, "pi-1", "t1"); err != nil {
		t.Fatal(err)
	}

	snap, err := c.Accounts(ctx)
	if err != nil || len(snap.Providers) != 1 {
		t.Fatalf("Accounts = %+v, %v", snap, err)
	}
	login, err := c.StartLogin(ctx, "claude")
	if err != nil || login.ID != "st1" {
		t.Fatalf("StartLogin = %+v, %v", login, err)
	}
	for name, err := range map[string]error{
		"CompleteLogin": c.CompleteLogin(ctx, login, "http://localhost:1/cb?code=x"),
		"CancelLogin":   c.CancelLogin(ctx, login),
		"SetDisabled":   c.SetDisabled(ctx, "a@b.json", true),
		"RemoveAccount": c.RemoveAccount(ctx, "a@b.json"),
		"Refresh":       c.RefreshAccount(ctx, "a@b.json"),
		"SetActive":     c.SetActive(ctx, "claude", "a@b.json"),
	} {
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if report, err := c.Publish(ctx); err != nil || report != "published" {
		t.Errorf("Publish = %q, %v", report, err)
	}
	if f.patched != "a@b.json" {
		t.Errorf("an account name with @ and . must arrive intact, got %q", f.patched)
	}
}

func TestTheClientTellsErrorsApart(t *testing.T) {
	ts := newTestServer(t, &fake{}, func(c *Config) { c.Accounts = nil })
	ctx := context.Background()

	if _, err := newClient(t, ts, "wrong").Info(ctx); !client.IsUnauthorized(err) {
		t.Errorf("a wrong token: %v", err)
	}
	if _, err := newClient(t, ts, token).GetCheckout(ctx, "nope"); !client.IsNotFound(err) {
		t.Errorf("an unknown checkout: %v", err)
	}
	_, err := newClient(t, ts, token).Accounts(ctx)
	var apiErr *client.Error
	if !client.IsUnavailable(err) || !errors.As(err, &apiErr) || apiErr.Status != 501 {
		t.Errorf("a part the server does not offer: %v", err)
	}
	if _, err := client.New("not a url", token); err == nil {
		t.Error("a bad address was accepted")
	}
}

func TestTheClientWatchesTheFleet(t *testing.T) {
	f := &fake{}
	ts := newTestServer(t, f)
	c := newClient(t, ts, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	seen := 0
	err := c.WatchFleet(ctx, func(fl v1.Fleet) error {
		seen++
		if len(fl.Pods) != 1 {
			t.Errorf("fleet = %+v", fl)
		}
		cancel()
		return nil
	})
	if seen != 1 || !errors.Is(err, context.Canceled) {
		t.Errorf("seen %d, err %v", seen, err)
	}

	f.err = errors.New("Unable to connect to the server")
	err = c.WatchFleet(context.Background(), func(v1.Fleet) error { return nil })
	if !client.IsUnavailable(err) {
		t.Errorf("a failing cluster ends the watch with its Problem: %v", err)
	}
}
