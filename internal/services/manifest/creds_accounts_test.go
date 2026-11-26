package manifest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// runCPAPublish runs creds-publish.sh --cpa against the fake CLIProxyAPI with
// the nightshift-accounts ConfigMap holding active and optional chains, and
// returns every kubectl call it made.
func runCPAPublish(t *testing.T, active string) string {
	t.Helper()
	return runCPAPublishWithChains(t, active, "")
}

func runCPAPublishWithChains(t *testing.T, active, chains string) string {
	t.Helper()
	srv := httptest.NewServer(&fakeCPA{})
	defer srv.Close()
	binDir, logDir, homeDir := t.TempDir(), t.TempDir(), t.TempDir()
	bin, logs := openRoot(t, binDir), openRoot(t, logDir)
	stub := `#!/bin/sh
echo "$*" >> "$LOGS/calls"
case "$*" in
  *"get configmap nightshift-accounts"*data.chains*) printf '%s' "$CHAINS" ;;
  *"get configmap nightshift-accounts"*jsonpath*) printf '%s' "$ACTIVE" ;;
  *"get sandboxwarmpool"*) printf 'claude-code-pool\n' ;;
  *"get secret egress-secrets"*|*"get configmap nightshift-accounts"*) exit 0 ;;
esac
cat >/dev/null 2>&1 </dev/null || true
exit 0
`
	if err := bin.WriteFile("kubectl", []byte(stub), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `chmod 700 "$BIN"/* && bash ../../../k8s/creds-publish.sh --cpa "$CPA_URL"`)
	cmd.Env = append(os.Environ(),
		"BIN="+binDir, "PATH="+binDir+":"+os.Getenv("PATH"), "HOME="+homeDir, "LOGS="+logDir,
		"CPA_URL="+srv.URL, "CPA_MANAGEMENT_KEY=test-management-key",
		"NIGHTSHIFT_CONTEXT=test", "ACTIVE="+active, "CHAINS="+chains,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creds-publish.sh --cpa: %v\n%s", err, out)
	}
	raw, err := logs.ReadFile("calls")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// With several accounts per agent, the one picked in the Accounts tab is the
// one sandboxes get, its label travels with the token so usage can be counted
// per account, and what was published is recorded for the tab to show.
func TestCredsPublishUsesTheChosenAccount(t *testing.T) {
	calls := runCPAPublish(t, `{"claude":"claude-z@example.com.json"}`)
	if !strings.Contains(calls, b64("sk-ant-oat-CPA-claude-z")) {
		t.Errorf("the chosen account's token was not published:\n%s", calls)
	}
	if strings.Contains(calls, b64("sk-ant-oat-CPA-claude")+"\"") {
		t.Errorf("the first account's token was published although another is active")
	}
	if !strings.Contains(calls, b64("z@example.com")) {
		t.Errorf("the active account's label was not published:\n%s", calls)
	}
	for _, want := range []string{"patch configmap nightshift-accounts", "published_at", "claude-z@example.com.json"} {
		if !strings.Contains(calls, want) {
			t.Errorf("the publish status lacks %q:\n%s", want, calls)
		}
	}
}

func TestCredsPublishFallsBackWhenTheChosenAccountIsGone(t *testing.T) {
	calls := runCPAPublish(t, `{"claude":"claude-deleted@example.com.json"}`)
	if !strings.Contains(calls, b64("sk-ant-oat-CPA-claude")) {
		t.Errorf("with the chosen account gone, the first enabled one must be used:\n%s", calls)
	}
}

func TestCredsPublishPublishesOrderedFallbackPoolAndCompanions(t *testing.T) {
	calls := runCPAPublishWithChains(t, "", `{"claude":["claude-z@example.com.json","claude-a@example.com.json"]}`)

	extractPool := func(key string) []struct {
		Name      string            `json:"name"`
		Label     string            `json:"label"`
		Token     string            `json:"token"`
		Companion map[string]string `json:"companion"`
	} {
		t.Helper()
		re := regexp.MustCompile(`"` + key + `":"([^"]+)"`)
		m := re.FindStringSubmatch(calls)
		if len(m) != 2 {
			t.Fatalf("%s not found in calls:\n%s", key, calls)
		}
		raw, err := base64.StdEncoding.DecodeString(m[1])
		if err != nil {
			t.Fatalf("decode %s: %v", key, err)
		}
		var out []struct {
			Name      string            `json:"name"`
			Label     string            `json:"label"`
			Token     string            `json:"token"`
			Companion map[string]string `json:"companion"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal %s (%s): %v", key, raw, err)
		}
		return out
	}

	claude := extractPool("POOL_CLAUDE_ACCESS_TOKEN")
	if len(claude) != 2 || claude[0].Name != "claude-z@example.com.json" || claude[1].Name != "claude-a@example.com.json" {
		t.Errorf("claude pool = %+v, want [claude-z, claude-a]", claude)
	}
	codex := extractPool("POOL_CODEX_ACCESS_TOKEN")
	if len(codex) != 1 || codex[0].Companion["CODEX_ACCOUNT_ID"] != "acct" {
		t.Errorf("codex pool = %+v, want companion CODEX_ACCOUNT_ID=acct", codex)
	}
}
