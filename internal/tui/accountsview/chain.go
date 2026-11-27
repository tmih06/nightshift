package accountsview

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	v1 "github.com/tmih06/nightshift/api/v1"
)

// publishWindow bounds a change to the accounts and the publish after it.
const publishWindow = 2 * time.Minute

// enabledChain is one agent's accounts that traffic can go to, first in the
// fallback order first.
func enabledChain(s v1.Accounts, provider string) []v1.Account {
	all := byProvider(s, provider)
	out := make([]v1.Account, 0, len(all))
	for i := range all {
		if !all[i].Disabled {
			out = append(out, all[i])
		}
	}
	return out
}

// inUse is the account the egress proxy says it sends an agent's requests
// with. Only the proxy knows: the order alone does not, while one cools down.
func inUse(s v1.Accounts, provider string) (v1.Account, bool) {
	chain := enabledChain(s, provider)
	for i := range chain {
		if chain[i].ActiveNow {
			return chain[i], true
		}
	}
	return v1.Account{}, false
}

// wouldCarry is the account the proxy would pick from chain: the first that is
// not cooling down, else the one that recovers soonest.
func wouldCarry(chain []v1.Account, now time.Time) v1.Account {
	var soonest v1.Account
	for i := range chain {
		if !chain[i].CooldownUntil.After(now) {
			return chain[i]
		}
		if soonest.Name == "" || chain[i].CooldownUntil.Before(soonest.CooldownUntil) {
			soonest = chain[i]
		}
	}
	return soonest
}

// pending is whether a is first in its chain and the proxy has yet to move to
// it.
func (m Model) pending(a v1.Account) bool {
	_, known := inUse(m.snap, a.Provider)
	return known && a.Priority == 1 && !a.ActiveNow && !a.CooldownUntil.After(m.now())
}

// reorder is a change to a fallback chain, before it is made.
type reorder struct {
	provider      string
	before, after []v1.Account
}

// carriers is who sends the agent's requests before and after r.
func (m Model) carriers(r reorder) (from, to v1.Account) {
	from, known := inUse(m.snap, r.provider)
	if !known {
		from = wouldCarry(r.before, m.now())
	}
	return from, wouldCarry(r.after, m.now())
}

// movesTraffic is whether r changes which account requests go to, now or once
// a cooldown ends. Only such a change is worth asking about.
func (m Model) movesTraffic(r reorder) bool {
	from, to := m.carriers(r)
	return r.before[0].Name != r.after[0].Name || from.Name != to.Name
}

// warning says what r does to running sandboxes.
func (m Model) warning(r reorder) []string {
	from, to := m.carriers(r)
	head := r.after[0]
	var lines []string
	if from.Name != to.Name {
		lines = append(lines,
			"All "+providerByID(m.snap, r.provider).Name+" traffic moves from "+label(from)+" to "+label(to)+", running sessions included.",
			"",
			"Cache miss: prompt caches belong to one account, so each running session resends its whole context uncached. Its next requests are slower and use more quota.")
	} else {
		lines = append(lines, label(from)+" keeps carrying the traffic for now, so no prompt cache is lost yet.")
	}
	if head.Name != to.Name {
		lines = append(lines, "", label(head)+" is cooling down for "+until(m.now(), head.CooldownUntil)+
			". Traffic moves to it when that ends, with a cache miss.")
	}
	return append(lines, "", "The proxy picks this up within about 2 minutes, and still falls back down the chain on 429, 401, 403, or 529.")
}

// change runs one change to the accounts, publishes it so the proxy gets it,
// and reads the accounts again.
func (m Model) change(ok string, apply func(ctx context.Context) error) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), publishWindow)
		defer cancel()
		if err := apply(ctx); err != nil {
			return doneMsg{err: err, refetch: true}
		}
		if _, err := svc.Publish(ctx); err != nil {
			return doneMsg{err: err, refetch: true}
		}
		return doneMsg{text: ok, refetch: true}
	}
}

// confirm asks before running cmd; progress shows while it runs.
func (m Model) confirm(title string, body []string, verb, progress string, cmd tea.Cmd) Model {
	m.modal = modal{kind: modalConfirm, title: title, body: body, verb: verb, progress: progress, run: cmd}
	return m
}

// start runs cmd with progress on screen until it ends.
func (m Model) start(progress string, cmd tea.Cmd) (Model, tea.Cmd) {
	m.notice, m.noticeBad, m.noticeAt = progress, false, m.now()
	return m, cmd
}
