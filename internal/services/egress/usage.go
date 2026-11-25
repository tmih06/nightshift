package egress

import (
	"sort"
	"sync"
	"time"
)

const (
	usageBucket  = 10 * time.Minute
	usageBuckets = 144 // 24 hours
)

// AccountUsage counts the requests that carried one secret for one account.
// The counts live in memory and start over when the proxy restarts.
type AccountUsage struct {
	Binding     string    `json:"binding"` // the secret binding's name
	Label       string    `json:"label"`
	Requests    int       `json:"requests"`
	OK          int       `json:"ok"`
	ClientError int       `json:"client_error"`
	RateLimited int       `json:"rate_limited"`
	ServerError int       `json:"server_error"`
	LastRequest time.Time `json:"last_request"`
	// Recent holds the request count of each ten-minute window, oldest first,
	// ending with the window now in progress.
	Recent []int `json:"recent"`
}

type usageKey struct{ secret, label string }

type usageSeries struct {
	AccountUsage
	windows map[int64]int // window start (unix / bucket) -> requests
}

type usageStore struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[usageKey]*usageSeries
}

func newUsageStore() *usageStore {
	return &usageStore{now: time.Now, m: map[usageKey]*usageSeries{}}
}

func (u *usageStore) window(t time.Time) int64 { return t.Unix() / int64(usageBucket.Seconds()) }

// record counts one request that carried secret, tagged with its account label.
func (u *usageStore) record(secret, label string, status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	k := usageKey{secret, label}
	s := u.m[k]
	if s == nil {
		s = &usageSeries{AccountUsage: AccountUsage{Binding: secret, Label: label}, windows: map[int64]int{}}
		u.m[k] = s
	}
	now := u.now()
	s.Requests++
	s.LastRequest = now.UTC()
	switch {
	case status == 429:
		s.RateLimited++
	case status >= 500:
		s.ServerError++
	case status >= 400:
		s.ClientError++
	default:
		s.OK++
	}
	w := u.window(now)
	s.windows[w]++
	for old := range s.windows {
		if old <= w-usageBuckets {
			delete(s.windows, old)
		}
	}
}

// snapshot returns every series, most recently used first.
func (u *usageStore) snapshot() []AccountUsage {
	u.mu.Lock()
	defer u.mu.Unlock()
	now := u.window(u.now())
	out := make([]AccountUsage, 0, len(u.m))
	for _, s := range u.m {
		cp := s.AccountUsage
		cp.Recent = make([]int, usageBuckets)
		for i := range cp.Recent {
			cp.Recent[i] = s.windows[now-int64(usageBuckets-1-i)]
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastRequest.After(out[j].LastRequest) })
	return out
}

// Usage reports, per secret and account, how many requests carried it.
func (p *Proxy) Usage() []AccountUsage { return p.usage.snapshot() }
