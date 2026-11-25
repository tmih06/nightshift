package cluster

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// stubKubectl puts a kubectl that records its arguments first on PATH.
func stubKubectl(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := root.WriteFile("kubectl", []byte("#!/bin/sh\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	chmod := exec.CommandContext(ctx, "sh", "-c", `chmod 700 "$STUB_DIR/kubectl"`)
	chmod.Env = append(os.Environ(), "STUB_DIR="+dir)
	if out, err := chmod.CombinedOutput(); err != nil {
		t.Fatalf("make the stub executable: %v\n%s", err, out)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestKubectlTargetsTheNightshiftContextAndNamespace(t *testing.T) {
	t.Setenv(ContextEnv, "eks-prod")
	stubKubectl(t, `echo "$*"`)
	out, err := Kubectl(t.Context(), "nightshift-egress", "get", "secret", "x y")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(out)), "--context eks-prod -n nightshift-egress get secret x y"; got != want {
		t.Errorf("kubectl ran %q, want %q", got, want)
	}
}

func TestKubectlErrorsCarryKubectlsMessage(t *testing.T) {
	stubKubectl(t, `echo "Error from server (NotFound): nope" >&2; exit 1`)
	_, err := Kubectl(t.Context(), "nightshift", "get", "pod")
	if err == nil || !strings.Contains(err.Error(), "NotFound") {
		t.Errorf("error = %v, want kubectl's message in it", err)
	}
}

func TestPortForwardAsksForAFreeLocalPort(t *testing.T) {
	t.Setenv(ContextEnv, "")
	cmd := PortForward(t.Context(), "nightshift", "svc/cpa", 8317)
	got := strings.Join(cmd.Args[1:], " ")
	if want := "--context " + DefaultKubeContext + " -n nightshift port-forward svc/cpa :8317"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
}
