package accounts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tmih06/nightshift/internal/services/cluster"
)

// Where the pieces live in the cluster.
const (
	accountsMap   = "nightshift-accounts"
	keysSecret    = "cpa-keys"
	refresherPod  = "deploy/creds-refresher"
	cpaInCluster  = "http://cpa.nightshift.svc:8317"
	kubectlWindow = 60 * time.Second
)

// Kube runs kubectl against the cluster the rest of Nightshift uses.
type Kube struct{}

func (Kube) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, kubectlWindow)
	defer cancel()
	out, err := cluster.Kubectl(ctx, cluster.Namespace, args...)
	if err != nil {
		return nil, fmt.Errorf("cluster: %w", err)
	}
	return out, nil
}

// ManagementKey reads CLIProxyAPI's management key.
func (k Kube) ManagementKey(ctx context.Context) (string, error) {
	out, err := k.run(ctx, "get", "secret", keysSecret, "-o", "jsonpath={.data.management}")
	if err != nil {
		return "", fmt.Errorf("read the CLIProxyAPI key (is k8s/creds-setup.sh installed?): %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(raw) == 0 {
		return "", errors.New("the cpa-keys secret has no management key")
	}
	return string(raw), nil
}

// accountsData is what the refresher and the tab keep in the nightshift-accounts
// ConfigMap: the chosen account per agent, and what was last published.
type accountsData struct {
	Active      map[string]string
	PublishedAt time.Time
	Published   map[string]Published
}

// ReadAccounts reads the nightshift-accounts ConfigMap; a missing one is empty.
func (k Kube) ReadAccounts(ctx context.Context) (accountsData, error) {
	out, err := k.run(ctx, "get", "configmap", accountsMap, "--ignore-not-found", "-o", "json")
	if err != nil {
		return accountsData{}, err
	}
	d := accountsData{Active: map[string]string{}, Published: map[string]Published{}}
	if len(bytes.TrimSpace(out)) == 0 {
		return d, nil
	}
	var cm struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &cm); err != nil {
		return d, fmt.Errorf("parse %s: %w", accountsMap, err)
	}
	if raw := cm.Data["active"]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &d.Active); err != nil {
			return d, fmt.Errorf("parse active accounts: %w", err)
		}
	}
	if raw := cm.Data["status"]; raw != "" {
		var st struct {
			PublishedAt string `json:"published_at"`
			Accounts    map[string]struct {
				Name    string `json:"name"`
				Label   string `json:"label"`
				Expires string `json:"expires"`
			} `json:"accounts"`
		}
		if err := json.Unmarshal([]byte(raw), &st); err != nil {
			return d, fmt.Errorf("parse publish status: %w", err)
		}
		d.PublishedAt = parseTime(st.PublishedAt)
		for id, a := range st.Accounts {
			d.Published[id] = Published{Name: a.Name, Label: a.Label, Expires: parseTime(a.Expires)}
		}
	}
	return d, nil
}

// SetActive records which account an agent's sandboxes should get, keeping the
// choices for the other agents.
func (k Kube) SetActive(ctx context.Context, provider, name string) error {
	d, err := k.ReadAccounts(ctx)
	if err != nil {
		return err
	}
	d.Active[provider] = name
	raw, err := json.Marshal(d.Active)
	if err != nil {
		return fmt.Errorf("encode active accounts: %w", err)
	}
	patch, err := json.Marshal(map[string]any{"data": map[string]string{"active": string(raw)}})
	if err != nil {
		return fmt.Errorf("encode patch: %w", err)
	}
	exists, err := k.run(ctx, "get", "configmap", accountsMap, "--ignore-not-found", "-o", "name")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(exists)) == 0 {
		_, err = k.run(ctx, "create", "configmap", accountsMap, "--from-literal=active="+string(raw))
		return err
	}
	_, err = k.run(ctx, "patch", "configmap", accountsMap, "--type=merge", "-p", string(patch))
	return err
}

// PublishNow has the refresher read CLIProxyAPI and publish straight away,
// instead of at its next five-minute pass, and returns its report.
func (k Kube) PublishNow(ctx context.Context) (string, error) {
	out, err := k.run(ctx, "exec", refresherPod, "--", "creds-publish", "--cpa", cpaInCluster)
	return string(out), err
}
