package accounts

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// stubKubectl puts a kubectl that runs body first on PATH for the test. The
// stub is made executable by chmod, so the test writes nothing but 0600 files.
func stubKubectl(t *testing.T, body string) string {
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
	return dir
}

// readFile reads name from dir without following paths out of it.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := root.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// reply writes body to w, failing the test when the write fails.
func reply(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write reply: %v", err)
	}
}
