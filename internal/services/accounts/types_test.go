package accounts

import (
	"testing"
	"time"
)

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
