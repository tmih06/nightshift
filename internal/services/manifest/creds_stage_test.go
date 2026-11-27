package manifest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"
)

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func writeFixture(t *testing.T, home *os.Root, rel, body string) {
	t.Helper()
	if err := home.MkdirAll(path.Dir(rel), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := home.WriteFile(rel, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// object returns m[key] as a JSON object, failing the test when it is not one.
func object(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not an object in %v", key, m)
	}
	return v
}

func readJSON(t *testing.T, root *os.Root, rel string) map[string]any {
	t.Helper()
	raw, err := root.ReadFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return out
}

func TestCredsStageSwapsEveryAgentLogin(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required to test creds-stage.sh: %v", err)
	}
	homeDir, stageDir, tokenDir := t.TempDir(), t.TempDir(), t.TempDir()
	home := openRoot(t, homeDir)
	writeFixture(t, home, ".claude/.credentials.json", `{"claudeAiOauth":{"accessToken":"sk-ant-oat-REAL-access","refreshToken":"sk-ant-ort-REAL-refresh","expiresAt":1,"scopes":["a"],"accountUuid":"uuid-REAL-account","organizationUuid":"uuid-REAL-org","emailAddress":"REAL@claude.example"},"mcpOAuth":{"server|1":{"accessToken":"mcp-REAL-access","refreshToken":"mcp-REAL-refresh"}}}`)
	writeFixture(t, home, ".codex/auth.json", `{"auth_mode":"chatgpt","OPENAI_API_KEY":"sk-REAL-openai-key","tokens":{"id_token":"idtok-REAL-identity","access_token":"codex-REAL-access","refresh_token":"codex-REAL-refresh","account_id":"acct","email":"REAL@codex.example"},"last_refresh":"2026-01-01T00:00:00Z"}`)
	writeFixture(t, home, ".gemini/antigravity-cli/antigravity-oauth-token", `{"token":{"access_token":"ya29.REAL-agy-access","token_type":"Bearer","refresh_token":"1//REAL-agy-refresh","expiry":"2026-01-01T00:00:00Z","expires_in":3599,"id_token":"idtok-REAL-agy"},"auth_method":"oauth","id_token_email":"a@b.c"}`)
	writeFixture(t, home, ".config/gh/hosts.yml", "github.com:\n    users:\n        u:\n            oauth_token: gho_REAL-github\n    oauth_token: gho_REAL-github\n    user: u\n")
	writeFixture(t, home, ".local/share/opencode/auth.json", `{"x":{"type":"oauth","access":"opencode-REAL"}}`)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `sh ../../../k8s/creds-stage.sh "$SRC" "$STAGE" "$TOKENS"`)
	cmd.Env = append(os.Environ(), "SRC="+homeDir, "STAGE="+stageDir, "TOKENS="+tokenDir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("creds-stage.sh: %v\n%s", err, out.String())
	}
	stage, tokens := openRoot(t, stageDir), openRoot(t, tokenDir)

	// No sandbox-bound file may contain a real token.
	realValues := []string{
		"sk-ant-oat-REAL-access", "sk-ant-ort-REAL-refresh",
		"codex-REAL-access", "codex-REAL-refresh",
		"ya29.REAL-agy-access", "1//REAL-agy-refresh",
		"gho_REAL-github", "opencode-REAL",
		"mcp-REAL-access", "mcp-REAL-refresh", "sk-REAL-openai-key",
		// Nothing that names the account either: which one carries a request
		// is the proxy's choice, so a sandbox is told none.
		"a@b.c", "uuid-REAL-account", "uuid-REAL-org", "REAL@claude.example",
		"idtok-REAL-identity", "REAL@codex.example", "idtok-REAL-agy",
	}
	err := fs.WalkDir(stage.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, rerr := fs.ReadFile(stage.FS(), p)
		if rerr != nil {
			return rerr
		}
		for _, real := range realValues {
			if strings.Contains(string(raw), real) {
				t.Errorf("%s leaks %q", p, real)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []struct{ file, value string }{
		{"CLAUDE_ACCESS_TOKEN", "sk-ant-oat-REAL-access"},
		{"CODEX_ACCESS_TOKEN", "codex-REAL-access"},
		{"AGY_ACCESS_TOKEN", "ya29.REAL-agy-access"},
	} {
		got, err := tokens.ReadFile(want.file)
		if err != nil || string(got) != want.value {
			t.Errorf("%s = %q (%v), want %q", want.file, got, err, want.value)
		}
	}

	codex := object(t, readJSON(t, stage, ".codex/auth.json"), "tokens")
	if codex["access_token"] != "nightshift:secret:CODEX_ACCESS_TOKEN" {
		t.Errorf("codex access_token = %v", codex["access_token"])
	}
	if codex["account_id"] != "nightshift:secret:CODEX_ACCOUNT_ID" {
		t.Errorf("codex account_id = %v, want placeholder so fallback swaps it with the token", codex["account_id"])
	}
	if got, err := tokens.ReadFile("CODEX_ACCESS_TOKEN.companions.json"); err != nil || !strings.Contains(string(got), `"CODEX_ACCOUNT_ID":"acct"`) {
		t.Errorf("CODEX_ACCESS_TOKEN.companions.json = %q (%v)", got, err)
	}
	if codex["refresh_token"] == "" || codex["refresh_token"] == nil {
		t.Error("codex needs a non-empty inert refresh_token")
	}
	// Codex refuses to start on a config.toml that overrides a built-in
	// provider, and releases have disagreed about which overrides are
	// allowed, so no config is shipped: Codex retries its WebSocket against
	// the proxy and falls back to HTTPS by itself.
	if _, err := stage.Stat(".codex/config.toml"); err == nil {
		t.Error("a config.toml can stop a Codex release from starting; none may be distributed")
	}
	agy := object(t, readJSON(t, stage, ".gemini/antigravity-cli/antigravity-oauth-token"), "token")
	if agy["access_token"] != "nightshift:secret:AGY_ACCESS_TOKEN" {
		t.Errorf("agy access_token = %v", agy["access_token"])
	}
	if _, ok := agy["refresh_token"]; ok {
		t.Error("agy refresh_token must be removed")
	}
	if agy["expiry"] != "2099-01-01T00:00:00Z" {
		t.Errorf("agy expiry = %v, want far future", agy["expiry"])
	}
	if got := readJSON(t, stage, ".gemini/antigravity-cli/antigravity-oauth-token")["id_token_email"]; got != "nightshift@proxy.local" {
		t.Errorf("agy id_token_email = %v, want the fixed sandbox identity, not the account's", got)
	}
	assertAnonymousIDToken(t, codex["id_token"])
	claude := object(t, readJSON(t, stage, ".claude/.credentials.json"), "claudeAiOauth")
	if _, ok := claude["refreshToken"]; ok {
		t.Error("claude refreshToken must be removed")
	}
	gh, err := stage.ReadFile(".config/gh/hosts.yml")
	if err != nil || !strings.Contains(string(gh), "oauth_token: nightshift:secret:GITHUB_TOKEN") {
		t.Errorf("gh hosts.yml not swapped: %q (%v)", gh, err)
	}
	if _, err := stage.Stat(".local/share/opencode/auth.json"); err == nil {
		t.Error("a login with no swap must not be distributed")
	}
}

// assertAnonymousIDToken checks the id_token Codex needs to start: a JWT whose
// claims name a plan and the fixed sandbox identity, no account.
func assertAnonymousIDToken(t *testing.T, token any) {
	t.Helper()
	s, ok := token.(string)
	if !ok {
		t.Fatalf("codex id_token = %v, want a JWT", token)
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		t.Fatalf("codex id_token has %d parts, want 3", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("codex id_token claims: %v", err)
	}
	const want = `{"email":"nightshift@proxy.local","https://api.openai.com/auth":{"chatgpt_plan_type":"pro"}}`
	if string(raw) != want {
		t.Errorf("codex id_token claims = %s, want %s", raw, want)
	}
}
