package manifest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

// k8s/warmpool.yaml is k8s/warmpool.sh's output, so an agent's template comes
// from its directory under images/agent/agents and nowhere else.
func TestWarmPoolIsWhatTheAgentsRender(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	got, err := exec.CommandContext(ctx, "bash", "../../../k8s/warmpool.sh").Output()
	if err != nil {
		t.Fatalf("k8s/warmpool.sh: %v", err)
	}
	want, err := os.ReadFile("../../../k8s/warmpool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("k8s/warmpool.yaml differs from what k8s/warmpool.sh renders; run make warmpool")
	}
}

// A Devin sandbox starts acting without asking, as agy's does, and keeps the
// version its image was built and tested with.
func TestDevinSandboxBootsReadyToRun(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required to test the devin template: %v", err)
	}
	homeDir := t.TempDir()
	home := openRoot(t, homeDir)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	script := templateScript(t, "devin-template")
	cmd := exec.CommandContext(ctx, "sh", "-c", `eval "$SCRIPT"`)
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "SCRIPT="+script[:len(script)-len("exec sleep infinity\n")])
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("template script: %v\n%s", err, out)
	}
	raw, err := home.ReadFile(".config/devin/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Mode   string `json:"permission_mode"`
		Update *bool  `json:"auto_update"`
		Proxy  struct {
			Gzip *bool `json:"gzip"`
		} `json:"proxy"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "dangerous" || cfg.Update == nil || *cfg.Update || cfg.Proxy.Gzip == nil || *cfg.Proxy.Gzip {
		t.Errorf("devin config = %s, want permission_mode dangerous, auto_update false, and plain bodies", raw)
	}
}
