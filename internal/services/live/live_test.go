package live

import (
	"errors"
	"strings"
	"testing"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/accounts"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

func boxes() []cluster.Sandbox {
	return []cluster.Sandbox{
		{Name: "pi-warm", OwnerKind: "SandboxWarmPool", Owner: "pi-pool", Phase: "Running"},
		{Name: "pi-claimed", OwnerKind: "SandboxClaim", Owner: "c1", Phase: "Running"},
		{Name: "pi-starting", OwnerKind: "SandboxClaim", Owner: "c2", Phase: "Pending"},
		{Name: "pi-on-volume", Volume: "pr-acme-rocket-7", Phase: "Running"},
		{Name: "refresher", Namespace: "nightshift", OwnerKind: "ReplicaSet", Phase: "Running"},
		{Name: "egress-proxy-1", Namespace: cluster.EgressNamespace, OwnerKind: "ReplicaSet", Phase: "Running"},
	}
}

// Terminals open only in a sandbox someone holds. Without it, the API would
// open a shell in any pod of the namespace, such as the credential refresher.
func TestOnlyARunningClaimedSandboxCanHaveATerminal(t *testing.T) {
	snap := snapshot{boxes: boxes()}
	for pod, want := range map[string]bool{
		"pi-claimed": true, "pi-on-volume": true,
		"pi-warm": false, "pi-starting": false, "refresher": false, "egress-proxy-1": false, "nope": false,
	} {
		if got := has(snap, pod); got != want {
			t.Errorf("has(%s) = %v, want %v", pod, got, want)
		}
	}
}

func TestThePodsOfTheClusterAreDescribedInTheContractsWords(t *testing.T) {
	list := boxes()
	want := map[string]string{
		"pi-warm": v1.RoleWarm, "pi-claimed": v1.RoleClaimed, "pi-starting": v1.RoleClaimed, "pi-on-volume": v1.RoleClaimed,
		"refresher": v1.RoleOther, "egress-proxy-1": v1.RoleEgress,
	}
	for i := range list {
		p := podOf(&list[i], false)
		if p.Role != want[p.Name] {
			t.Errorf("%s has role %q, want %q", p.Name, p.Role, want[p.Name])
		}
		if p.Configs != nil {
			t.Errorf("%s carries manifests nobody asked for", p.Name)
		}
	}
	starting := cluster.Sandbox{
		Name: "p", Phase: "…", ManifestYAML: "kind: Sandbox",
		Startup: &cluster.Startup{Stage: cluster.StagePulling, Err: "ImagePullBackOff", PullFails: 3},
	}
	p := podOf(&starting, true)
	if p.Phase != "" || p.Startup == nil || p.Startup.Stage != v1.StagePulling || p.Startup.PullFailures != 3 || p.Configs == nil || p.Configs.Sandbox != "kind: Sandbox" {
		t.Errorf("a starting pod = %+v", p)
	}
}

func TestVolumesKeepTheirTargets(t *testing.T) {
	for _, tc := range []struct {
		in   cluster.VolumeFor
		want v1.Target
	}{
		{cluster.VolumeFor{Repo: "a/b", Number: 7, Kind: cluster.VolumePR}, v1.Target{Kind: v1.TargetPullRequest, Repo: "a/b", Number: 7}},
		{cluster.VolumeFor{Repo: "a/b", Number: 9, Branch: "x", Kind: cluster.VolumeIssue}, v1.Target{Kind: v1.TargetIssue, Repo: "a/b", Number: 9, Branch: "x"}},
		{cluster.VolumeFor{Repo: "a/b", Branch: "dev", Kind: cluster.VolumeRepo}, v1.Target{Kind: v1.TargetBranch, Repo: "a/b", Branch: "dev"}},
	} {
		if got := targetOf(tc.in); got != tc.want {
			t.Errorf("targetOf(%+v) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// An account is shown without a token, in the contract's words.
func TestAnAccountIsDescribedWithoutItsToken(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a := accounts.Account{
		Name: "claude-a.json", Provider: "claude", Email: "a@example.com", Active: true,
		Expires: now.Add(time.Hour), Success: 4, Failed: 1,
		Usage: &accounts.Usage{Requests: 5, OK: 4, RateLimited: 1, Last: now, Recent: []int{0, 5}},
	}
	got := accountOf(&a, now)
	if got.State != v1.AccountOK || !got.Active || got.RefreshOK != 4 || got.Usage == nil || got.Usage.RateLimited != 1 || len(got.Usage.Recent) != 2 {
		t.Errorf("account = %+v", got)
	}
	a.RefreshError = "invalid_grant"
	if got := accountOf(&a, now); got.State != v1.AccountFailing {
		t.Errorf("an account that cannot renew is %q, want failing", got.State)
	}
	a.NeedsSignIn = true
	if got := accountOf(&a, now); got.State != v1.AccountSignIn {
		t.Errorf("an account that must sign in again is %q, want sign_in", got.State)
	}
}

func TestWrapErrKeepsNilAndTheCause(t *testing.T) {
	if wrapErr("do it", nil) != nil {
		t.Error("a nil error must stay nil, or every success would look like a failure")
	}
	cause := errors.New("boom")
	err := wrapErr("do it", cause)
	if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), "do it: ") {
		t.Errorf("err = %v", err)
	}
}

func TestLogLinesAreSplitWithoutEmptyOnes(t *testing.T) {
	if got := splitLines("a\nb\n"); len(got) != 2 || got[1] != "b" {
		t.Errorf("splitLines = %q", got)
	}
	if got := splitLines(""); got == nil || len(got) != 0 {
		t.Errorf("an empty log is [] not null: %#v", got)
	}
}
