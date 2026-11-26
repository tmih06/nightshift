package manifest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The refresher image runs creds-publish through a symlink in /usr/local/bin.
// The script once looked for its stage script and registry beside the symlink
// instead of beside itself, and published nothing.
// exercise runs each agent in a loop over the registry. An agent that reads
// stdin (claude -p does) once swallowed the rest of the loop, so the agents
// after it were never exercised and their expired tokens were published.
func TestCredsPublishExercisesEveryAgentEvenWhenOneReadsStdin(t *testing.T) {
	binDir, homeDir, logDir := t.TempDir(), t.TempDir(), t.TempDir()
	bin, home := openRoot(t, binDir), openRoot(t, homeDir)
	writeFixture(t, home, ".claude/.credentials.json", `{"claudeAiOauth":{"accessToken":"a","refreshToken":"b"}}`)
	writeFixture(t, home, ".gemini/antigravity-cli/antigravity-oauth-token", `{"token":{"access_token":"c","refresh_token":"d"}}`)
	stubs := map[string]string{
		"claude":  "#!/bin/sh\ncat >/dev/null\necho claude >> \"$RAN\"\n",
		"agy":     "#!/bin/sh\necho agy >> \"$RAN\"\n",
		"kubectl": "#!/bin/sh\ncat >/dev/null\nexit 0\n",
	}
	for name, body := range stubs {
		if err := bin.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `chmod 700 "$BIN"/* && bash ../../../k8s/creds-publish.sh --exercise`)
	cmd.Env = append(os.Environ(), "BIN="+binDir, "PATH="+binDir+":"+os.Getenv("PATH"),
		"HOME="+homeDir, "RAN="+logDir+"/ran", "NIGHTSHIFT_CONTEXT=test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creds-publish.sh --exercise: %v\n%s", err, out)
	}
	ran, err := openRoot(t, logDir).ReadFile("ran")
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"claude", "agy"} {
		if !strings.Contains(string(ran), agent) {
			t.Errorf("%s was not exercised; ran:\n%s", agent, ran)
		}
	}
}

func mustAbs(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestCredsPublishFindsItsFilesThroughASymlink(t *testing.T) {
	dir, homeDir := t.TempDir(), t.TempDir()
	link := dir + "/creds-publish"
	if err := os.Symlink(mustAbs(t, "../../../k8s/creds-publish.sh"), link); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `"$LINK"`)
	cmd.Env = append(os.Environ(), "LINK="+link, "HOME="+homeDir, "NIGHTSHIFT_CONTEXT=test")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an empty home must publish nothing and fail:\n%s", out)
	}
	if !strings.Contains(string(out), "nothing published") || strings.Contains(string(out), "No such file") {
		t.Errorf("the script did not find its own files through the symlink:\n%s", out)
	}
}
