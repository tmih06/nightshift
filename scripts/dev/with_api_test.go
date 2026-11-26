package dev

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCluster puts a kubectl and a curl on PATH: the API answers only once
// kubectl has been asked to port-forward it.
func fakeCluster(t *testing.T) (marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), "forwarded")
	fakes, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakes+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MARKER", marker)
	t.Setenv("PORT", "7777")
	t.Setenv("NIGHTSHIFT_TOKEN", "stale")
	return marker
}

func withAPI(t *testing.T) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "./with-api", "sh", "-c", `echo "$NIGHTSHIFT_API $NIGHTSHIFT_TOKEN"`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("with-api: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWithAPIForwardsALeftoverLocalAddress(t *testing.T) {
	for _, api := range []string{"", "http://127.0.0.1:7777", "http://localhost:7777/"} {
		marker := fakeCluster(t)
		t.Setenv("NIGHTSHIFT_API", api)
		got := withAPI(t)
		if want := "http://127.0.0.1:7777 fresh"; got != want {
			t.Errorf("NIGHTSHIFT_API=%q: command saw %q, want %q", api, got, want)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("NIGHTSHIFT_API=%q: no port-forward was started", api)
		}
	}
}

func TestWithAPILeavesAnotherAddressAlone(t *testing.T) {
	marker := fakeCluster(t)
	t.Setenv("NIGHTSHIFT_API", "https://nightshift.example.com")
	got := withAPI(t)
	if want := "https://nightshift.example.com stale"; got != want {
		t.Errorf("command saw %q, want %q", got, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("a port-forward was started for a remote API")
	}
}
