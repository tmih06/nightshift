package manifest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// runEgressSetup runs k8s/egress-setup.sh against stub kubectl, docker, and
// kind binaries and returns what kubectl applied and every call it saw.
func runEgressSetup(t *testing.T, env ...string) (applied, calls string) {
	t.Helper()
	binDir, logDir := t.TempDir(), t.TempDir()
	bin, logs := openRoot(t, binDir), openRoot(t, logDir)
	stub := `#!/bin/sh
echo "$(basename "$0") $*" >> "$CALLS"
case "$*" in
  *"get service egress-proxy"*clusterIP*) printf '172.20.7.9' ;;
  *"get secret egress-ca"*jsonpath*) printf 'Zm9v' ;;
  *apply*) cat >> "$APPLIED" ;;
esac
exit 0
`
	for _, name := range []string{"kubectl", "docker", "kind"} {
		if err := bin.WriteFile(name, []byte(stub), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `chmod 700 "$BIN"/* && bash ../../../k8s/egress-setup.sh`)
	cmd.Env = append(os.Environ(),
		"BIN="+binDir,
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"APPLIED="+logDir+"/applied",
		"CALLS="+logDir+"/calls",
		"GITHUB_TOKEN=test-token",
		"NIGHTSHIFT_CONTEXT=",
		"KUBE_CONTEXT=",
	)
	cmd.Env = append(cmd.Env, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("egress-setup.sh: %v\n%s", err, out)
	}
	a, err := logs.ReadFile("applied")
	if err != nil {
		t.Fatal(err)
	}
	c, err := logs.ReadFile("calls")
	if err != nil {
		t.Fatal(err)
	}
	return string(a), string(c)
}

// Service CIDRs differ per Kubernetes provider, so a pinned clusterIP can be
// rejected outright (EKS uses 172.20.0.0/16, kind 10.96.0.0/16). The manifest
// pins none, and the script writes the live address into sandbox-egress-env.
func TestEgressSetupPinsNoClusterIP(t *testing.T) {
	applied, calls := runEgressSetup(t, "NIGHTSHIFT_CONTEXT=eks-prod")
	if strings.Contains(applied, "clusterIP:") {
		t.Error("the applied manifest pins a clusterIP, which another provider's service range can reject")
	}
	if !strings.Contains(calls, "http://172.20.7.9:8080") {
		t.Errorf("sandbox-egress-env was not pointed at the live address:\n%s", calls)
	}
}

// The proxy address is cluster-specific and sandboxes cannot resolve service
// names, so only egress-setup.sh may write it. A value in the manifest would be
// reset by every kubectl apply and break each sandbox started afterwards.
func TestSandboxEnvManifestLeavesTheProxyAddressToSetup(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/egress.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var doc struct {
			Kind     string `yaml:"kind"`
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Data map[string]string `yaml:"data"`
		}
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "ConfigMap" || doc.Metadata.Name != "sandbox-egress-env" {
			continue
		}
		for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			if _, ok := doc.Data[key]; ok {
				t.Errorf("sandbox-egress-env sets %s; egress-setup.sh must own it", key)
			}
		}
		return
	}
	t.Fatal("k8s/egress.yaml has no sandbox-egress-env ConfigMap")
}

func TestEgressSetupImageFollowsTheProvider(t *testing.T) {
	applied, calls := runEgressSetup(t, "NIGHTSHIFT_CONTEXT=eks-prod")
	if !strings.Contains(applied, "image: ghcr.io/tmih06/nightshift-egress-proxy:latest") {
		t.Error("a non-kind cluster must pull the published proxy image")
	}
	if strings.Contains(calls, "kind load") || strings.Contains(calls, "docker build") {
		t.Errorf("a non-kind cluster must not build or kind-load an image:\n%s", calls)
	}

	if strings.Contains(calls, "rollout restart") {
		t.Errorf("a pulled image is replaced by a new tag, not a restart:\n%s", calls)
	}

	applied, calls = runEgressSetup(t, "NIGHTSHIFT_CONTEXT=kind-dev")
	if !strings.Contains(calls, "rollout restart deployment/egress-proxy") {
		t.Errorf("a rebuilt local image keeps its tag, so the proxy must be restarted to run it:\n%s", calls)
	}
	if !strings.Contains(applied, "image: nightshift-egress-proxy:local") {
		t.Error("a kind cluster must use the locally built image")
	}
	if !strings.Contains(calls, "kind load docker-image nightshift-egress-proxy:local --name dev") {
		t.Errorf("kind must load the image into the cluster named after the context:\n%s", calls)
	}

	applied, _ = runEgressSetup(t, "NIGHTSHIFT_CONTEXT=eks-prod", "EGRESS_IMAGE=registry.example/egress:1")
	if !strings.Contains(applied, "image: registry.example/egress:1") {
		t.Error("EGRESS_IMAGE must override the proxy image")
	}
}
