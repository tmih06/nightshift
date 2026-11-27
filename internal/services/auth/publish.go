package auth

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/tmih06/nightshift/internal/services/auth/providers"
)

// placeholderPrefix starts every placeholder a sandbox holds.
const placeholderPrefix = "nightshift:secret:"

// Chain is the ordered fallback chain of provider's enabled accounts: the
// configured ones still enabled, then the rest by name. The egress proxy
// tries them in this order.
func Chain(accounts []Account, provider string, configured []string) []string {
	enabled := map[string]bool{}
	var names []string
	for i := range accounts {
		a := &accounts[i]
		if a.Provider == provider && !a.Disabled && !a.NeedsSignIn {
			enabled[a.Name] = true
			names = append(names, a.Name)
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range append(append([]string(nil), configured...), names...) {
		if enabled[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// poolEntry is one account of an egress secret's fallback chain, as the proxy
// reads POOL_<secret>.
type poolEntry struct {
	Name      string            `json:"name"`
	Label     string            `json:"label"`
	Token     string            `json:"token"`
	Companion map[string]string `json:"companion,omitempty"`
}

// plan is everything one publish writes.
type plan struct {
	creds  []byte            // the agent-creds tar
	egress map[string][]byte // egress-secrets keys
	warm   map[string]bool   // pools with an account to hand out
	status Published
}

// buildPlan works out a publish from st. It reads nothing from the cluster.
func buildPlan(st *State, list []providers.Provider, now time.Time) (plan, error) {
	p := plan{egress: map[string][]byte{}, warm: map[string]bool{}, status: Published{At: now, Accounts: map[string]PublishedAccount{}}}
	byName := map[string]*Account{}
	for i := range st.Accounts {
		byName[st.Accounts[i].Name] = &st.Accounts[i]
	}
	files := map[string][]byte{}
	for _, prov := range list {
		info := prov.Info()
		chain := Chain(st.Accounts, info.ID, st.Chains[info.ID])
		entries := make([]poolEntry, 0, len(chain))
		for _, name := range chain {
			a := byName[name]
			entries = append(entries, poolEntry{Name: a.Name, Label: a.Label(), Token: a.Credential.Access, Companion: prov.Companions(a.Credential)})
		}
		pool, err := json.Marshal(entries)
		if err != nil {
			return plan{}, fmt.Errorf("encode the %s chain: %w", info.ID, err)
		}
		p.egress["POOL_"+info.Secret] = pool
		p.egress["REAL_"+info.Secret], p.egress["LABEL_"+info.Secret] = nil, nil
		if len(chain) == 0 {
			continue
		}
		primary := byName[chain[0]]
		login, err := prov.SandboxLogin(primary.Credential, placeholderPrefix+info.Secret)
		if err != nil {
			return plan{}, fmt.Errorf("build the %s sandbox login: %w", info.ID, err)
		}
		files[info.LoginFile] = login
		p.egress["REAL_"+info.Secret] = []byte(primary.Credential.Access)
		p.egress["LABEL_"+info.Secret] = []byte(primary.Label())
		p.warm[info.Pool] = true
		p.status.Accounts[info.ID] = PublishedAccount{Name: primary.Name, Label: primary.Label(), Expires: primary.Credential.Expires, Chain: chain}
	}
	tarball, err := tarFiles(files)
	if err != nil {
		return plan{}, err
	}
	p.creds = tarball
	return p, nil
}

// tarFiles packs files, by path under $HOME, as the tar a sandbox untars into
// its home at boot: private directories and files, in a stable order.
func tarFiles(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	dirs := map[string]bool{}
	epoch := time.Unix(0, 0)
	for _, name := range names {
		for _, dir := range parents(name) {
			if dirs[dir] {
				continue
			}
			dirs[dir] = true
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./" + dir + "/", Mode: 0o700, ModTime: epoch}); err != nil {
				return nil, fmt.Errorf("pack %s: %w", dir, err)
			}
		}
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + name, Mode: 0o600, Size: int64(len(body)), ModTime: epoch}); err != nil {
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("pack the logins: %w", err)
	}
	return buf.Bytes(), nil
}

// parents are name's directories, outermost first.
func parents(name string) []string {
	var out []string
	for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
		out = append([]string{dir}, out...)
	}
	return out
}

// Namespaces says where publishing writes.
type Namespaces struct {
	Sandboxes string // agent-creds and the warm pools
	Egress    string // egress-secrets
}

// apply writes p into the cluster and reports what it did.
func apply(ctx context.Context, c Cluster, ns Namespaces, p *plan) (string, error) {
	var report strings.Builder
	if err := c.MergeSecret(ctx, ns.Sandboxes, "agent-creds", map[string][]byte{"creds.tar": p.creds}); err != nil {
		return "", fmt.Errorf("write the sandbox logins: %w", err)
	}
	if err := c.MergeSecret(ctx, ns.Egress, "egress-secrets", p.egress); err != nil {
		return "", fmt.Errorf("write the proxy's tokens: %w", err)
	}
	ids := make([]string, 0, len(p.status.Accounts))
	for id := range p.status.Accounts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a := p.status.Accounts[id]
		fmt.Fprintf(&report, "%s: %s (chain of %d)\n", id, a.Label, len(a.Chain))
	}
	pools, err := c.WarmPools(ctx, ns.Sandboxes)
	if err != nil {
		return report.String(), fmt.Errorf("list the warm pools: %w", err)
	}
	var errs []error
	for _, pool := range pools {
		want := 0
		if p.warm[pool] {
			want = 1
		}
		if err := c.ScalePool(ctx, ns.Sandboxes, pool, want); err != nil {
			errs = append(errs, fmt.Errorf("scale %s: %w", pool, err))
			continue
		}
		fmt.Fprintf(&report, "%s: %d warm\n", pool, want)
	}
	return report.String(), errors.Join(errs...)
}
