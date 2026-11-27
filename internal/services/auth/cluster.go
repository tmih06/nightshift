package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Cluster is what publishing writes to. InCluster is the real one.
type Cluster interface {
	// MergeSecret sets keys of a Secret, creating it when missing, and
	// leaves its other keys alone. A nil value removes its key.
	MergeSecret(ctx context.Context, namespace, name string, data map[string][]byte) error
	// WarmPools names the SandboxWarmPools in namespace.
	WarmPools(ctx context.Context, namespace string) ([]string, error)
	// ScalePool sets a SandboxWarmPool's replicas.
	ScalePool(ctx context.Context, namespace, name string, replicas int) error
}

const (
	// apiServer is the API server's name inside every cluster; its CA, which
	// the service account carries, names it.
	apiServer         = "https://kubernetes.default.svc"
	serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	tokenFile         = serviceAccountDir + "/token"
	warmPoolsAPI      = "/apis/extensions.agents.x-k8s.io/v1beta1/namespaces/"
)

// InCluster talks to the API server of the cluster the pod runs in, as its
// service account. It needs no kubectl.
type InCluster struct {
	hc *http.Client
}

// NewInCluster reads the pod's service account.
func NewInCluster() (*InCluster, error) {
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		return nil, errors.New("not running in a cluster: KUBERNETES_SERVICE_HOST is unset")
	}
	ca, err := os.ReadFile(serviceAccountDir + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read the cluster CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("the cluster CA is not PEM")
	}
	return &InCluster{
		hc: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		}},
	}, nil
}

// request sends one API request; the token is reread each time, as the
// kubelet rotates it.
func (c *InCluster) request(ctx context.Context, method, path, contentType string, body any) (status int, reply []byte, err error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode %s: %w", path, err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiServer+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s: %w", path, err)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return 0, nil, fmt.Errorf("read the service account token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if cerr := resp.Body.Close(); rerr == nil {
		rerr = cerr
	}
	if rerr != nil {
		return 0, nil, fmt.Errorf("read %s: %w", path, rerr)
	}
	return resp.StatusCode, raw, nil
}

func apiError(what string, status int, raw []byte) error {
	var st struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &st) == nil && st.Message != "" {
		return fmt.Errorf("%s: %s", what, st.Message)
	}
	return fmt.Errorf("%s: HTTP %d", what, status)
}

// MergeSecret sets keys of a Secret, creating it when missing.
func (c *InCluster) MergeSecret(ctx context.Context, namespace, name string, data map[string][]byte) error {
	path := "/api/v1/namespaces/" + namespace + "/secrets/" + name
	status, raw, err := c.request(ctx, http.MethodPatch, path, "application/merge-patch+json", map[string]any{"data": data})
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		present := map[string][]byte{}
		for k, v := range data {
			if v != nil {
				present[k] = v
			}
		}
		secret := map[string]any{
			"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
			"metadata": map[string]string{"name": name, "namespace": namespace},
			"data":     present,
		}
		status, raw, err = c.request(ctx, http.MethodPost, "/api/v1/namespaces/"+namespace+"/secrets", "application/json", secret)
		if err != nil {
			return err
		}
	}
	if status >= 300 {
		return apiError("write secret "+namespace+"/"+name, status, raw)
	}
	return nil
}

// WarmPools names the SandboxWarmPools in namespace.
func (c *InCluster) WarmPools(ctx context.Context, namespace string) ([]string, error) {
	status, raw, err := c.request(ctx, http.MethodGet, warmPoolsAPI+namespace+"/sandboxwarmpools", "", nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, apiError("list warm pools", status, raw)
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse warm pools: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		names = append(names, it.Metadata.Name)
	}
	return names, nil
}

// ScalePool sets a SandboxWarmPool's replicas.
func (c *InCluster) ScalePool(ctx context.Context, namespace, name string, replicas int) error {
	patch := map[string]any{"spec": map[string]int{"replicas": replicas}}
	status, raw, err := c.request(ctx, http.MethodPatch, warmPoolsAPI+namespace+"/sandboxwarmpools/"+name, "application/merge-patch+json", patch)
	if err != nil {
		return err
	}
	if status >= 300 {
		return apiError("scale warm pool "+name, status, raw)
	}
	return nil
}
