package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// templateScript is the shell script a sandbox template starts with.
func templateScript(t *testing.T, name string) string {
	t.Helper()
	f, err := os.Open("../../../k8s/warmpool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	dec := yaml.NewDecoder(f)
	for {
		var doc document
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			t.Fatalf("no template %s", name)
		} else if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "SandboxTemplate" || doc.Metadata.Name != name {
			continue
		}
		command := doc.Spec.PodTemplate.Spec.Containers[0].Command
		return command[len(command)-1]
	}
}

// An agy sandbox must be usable the moment it boots: no first-run screens, no
// approval prompts, and no trust question in home or any mounted checkout.
func TestAgySandboxBootsReadyToRun(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatalf("jq is required to test the agy template: %v", err)
	}
	homeDir := t.TempDir()
	home := openRoot(t, homeDir)
	if err := home.MkdirAll("workspace/some-repo", 0o750); err != nil {
		t.Fatal(err)
	}
	script, found := strings.CutSuffix(strings.TrimSpace(templateScript(t, "agy-template")), "exec sleep infinity")
	if !found {
		t.Fatal("the template must end by sleeping")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `eval "$SCRIPT"`)
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "SCRIPT="+script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("template script: %v\n%s", err, out)
	}

	onboarding := readJSON(t, home, ".gemini/antigravity-cli/cache/onboarding.json")
	if onboarding["onboardingComplete"] != true || onboarding["consumerOnboardingComplete"] != true {
		t.Errorf("onboarding = %v, want it done", onboarding)
	}
	raw, err := home.ReadFile(".gemini/antigravity-cli/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Trusted  []string `json:"trustedWorkspaces"`
		Tools    string   `json:"toolPermission"`
		Artifact string   `json:"artifactReviewPolicy"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Tools != "always-proceed" || settings.Artifact != "always-proceed" {
		t.Errorf("permissions = %q, %q, want always-proceed", settings.Tools, settings.Artifact)
	}
	for _, dir := range []string{homeDir, homeDir + "/workspace", homeDir + "/workspace/some-repo"} {
		if !slices.Contains(settings.Trusted, dir) {
			t.Errorf("agy would ask to trust %s; trusted: %v", dir, settings.Trusted)
		}
	}
}
