package manifest

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCPA serves the slice of CLIProxyAPI's management API that
// creds-publish.sh --cpa uses, and records the credentials it refreshed.
type fakeCPA struct {
	mu        sync.Mutex
	refreshed []string
}

// fakeCPADownloads returns the credential files the fake proxy serves, keyed
// by file name. The bodies are raw JSON so they read as the proxy's wire format.
func fakeCPADownloads() map[string]string {
	return map[string]string{
		"claude-a@example.com.json": `{"type":"claude","access_token":"sk-ant-oat-CPA-claude","refresh_token":"sk-ant-ort-CPA-claude","email":"a@example.com"}`,
		"claude-z@example.com.json": `{"type":"claude","access_token":"sk-ant-oat-CPA-claude-z","refresh_token":"sk-ant-ort-CPA-claude-z","email":"z@example.com"}`,
		"codex-a@example.com.json":  `{"type":"codex","access_token":"codex-CPA-access","refresh_token":"codex-CPA-refresh","id_token":"idtok","account_id":"acct"}`,
		"agy-b@example.com.json":    `{"type":"antigravity","access_token":"agy-CPA-access","refresh_token":"1//CPA-agy","expired":"2026-01-01T00:00:00Z","expires_in":3599,"email":"b@example.com"}`,
	}
}

// replyJSON writes v as JSON, reporting an encoding or write failure as a 500.
func replyJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// replyText writes body verbatim, reporting a write failure as a 500.
func replyText(w http.ResponseWriter, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *fakeCPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-management-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	soon := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	later := time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339)
	switch r.URL.Path {
	case "/v8/management/credentials":
		replyJSON(w, map[string]any{"files": []map[string]any{
			{"name": "claude-a@example.com.json", "email": "a@example.com", "provider": "claude", "disabled": false, "access_token_expires_at": soon},
			{"name": "claude-z@example.com.json", "email": "z@example.com", "provider": "claude", "disabled": false, "access_token_expires_at": later},
			{"name": "codex-a@example.com.json", "email": "a@example.com", "provider": "codex", "disabled": false, "access_token_expires_at": later},
			{"name": "agy-a@example.com.json", "provider": "antigravity", "disabled": true, "access_token_expires_at": later},
			{"name": "agy-b@example.com.json", "email": "b@example.com", "provider": "antigravity", "disabled": false, "access_token_expires_at": later},
		}})
	case "/v8/management/credentials/refresh":
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.refreshed = append(f.refreshed, body.Name)
		f.mu.Unlock()
		replyText(w, `{"status":"ok"}`)
	case "/v8/management/credentials/download":
		c, ok := fakeCPADownloads()[r.URL.Query().Get("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		replyText(w, c)
	default:
		http.NotFound(w, r)
	}
}

func TestCredsPublishFromCLIProxyAPIAcrossClusters(t *testing.T) {
	cpa := &fakeCPA{}
	srv := httptest.NewServer(cpa)
	defer srv.Close()

	binDir, logDir, homeDir := t.TempDir(), t.TempDir(), t.TempDir()
	bin, logs := openRoot(t, binDir), openRoot(t, logDir)
	stub := `#!/bin/sh
echo "$*" >> "$LOGS/calls"
for a in "$@"; do
  case "$a" in
    --from-file=creds.tar=*) cp "${a#--from-file=creds.tar=}" "$LOGS/creds.tar" ;;
  esac
done
case "$*" in
  *"get sandboxwarmpool"*) printf 'claude-code-pool\ncodex-pool\nagy-pool\npi-pool\n' ;;
  *"get secret egress-secrets"*) exit 0 ;;
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
		"PUBLISH_CONTEXTS=-  eks-prod",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("creds-publish.sh --cpa: %v\n%s", err, out)
	}

	callsRaw, err := logs.ReadFile("calls")
	if err != nil {
		t.Fatal(err)
	}
	calls := string(callsRaw)

	// The first enabled account per agent was used, and only the claude token
	// that was about to expire was refreshed first.
	cpa.mu.Lock()
	refreshed := strings.Join(cpa.refreshed, ",")
	cpa.mu.Unlock()
	if refreshed != "claude-a@example.com.json" {
		t.Errorf("refreshed %q, want only the expiring claude-a@example.com.json", refreshed)
	}

	// The sandbox copies carry placeholders, never the CLIProxyAPI tokens.
	raw, err := logs.ReadFile("creds.tar")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[strings.TrimPrefix(h.Name, "./")] = string(body)
	}
	for _, name := range []string{".claude/.credentials.json", ".codex/auth.json", ".gemini/antigravity-cli/antigravity-oauth-token"} {
		body, ok := files[name]
		if !ok {
			t.Errorf("%s was not published; got %v", name, keys(files))
			continue
		}
		for _, real := range []string{"CPA-claude", "CPA-access", "CPA-refresh", "CPA-agy"} {
			if strings.Contains(body, real) {
				t.Errorf("%s leaks %q", name, real)
			}
		}
		if !strings.Contains(body, "nightshift:secret:") {
			t.Errorf("%s carries no placeholder", name)
		}
	}

	// The real tokens went to the proxy's secrets in every cluster, and the
	// pool of an agent with no login was scaled to zero.
	for _, token := range []string{"sk-ant-oat-CPA-claude", "codex-CPA-access", "agy-CPA-access"} {
		enc := base64.StdEncoding.EncodeToString([]byte(token))
		if strings.Count(calls, enc) != 2 {
			t.Errorf("real token %q reached egress-secrets %d times, want once per cluster", token, strings.Count(calls, enc))
		}
	}
	if !strings.Contains(calls, "--context eks-prod") {
		t.Errorf("nothing was published to the second cluster:\n%s", calls)
	}
	if !strings.Contains(calls, "patch sandboxwarmpool pi-pool") || !strings.Contains(calls, `"replicas":0`) {
		t.Errorf("pi-pool, which has no login, was not scaled to zero:\n%s", calls)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
