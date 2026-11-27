package accounts

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tmih06/nightshift/internal/services/cluster"
)

// Where nightshift-auth's key lives.
const (
	keySecret     = "nightshift-auth-key"
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

// AuthKey reads the key to nightshift-auth's API.
func (k Kube) AuthKey(ctx context.Context) (string, error) {
	out, err := k.run(ctx, "get", "secret", keySecret, "-o", "jsonpath={.data.key}")
	if err != nil {
		return "", fmt.Errorf("read the nightshift-auth key (is k8s/auth-setup.sh installed?): %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(raw) == 0 {
		return "", errors.New("the nightshift-auth-key secret has no key")
	}
	return string(raw), nil
}
