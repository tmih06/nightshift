package cluster

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestVolumeNamesAreValidAndStable(t *testing.T) {
	if got := VolumeName("acme/rocket", 7); got != "pr-acme-rocket-7" {
		t.Errorf("short = %s", got)
	}
	if got := VolumeName("Acme_Co/rocket.js", 12); got != "pr-acme-co-rocket-js-12" {
		t.Errorf("odd characters = %s", got)
	}
	long := VolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345)
	if len(long) > 63 || !strings.HasSuffix(long, "-12345") || long != VolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345) {
		t.Errorf("long = %s (%d)", long, len(long))
	}
	if VolumeName("a/b", 1) == VolumeName("a/b", 2) {
		t.Error("two pull requests share a volume")
	}
	if got := IssueVolumeName("acme/rocket", 42); got != "issue-acme-rocket-42" {
		t.Errorf("issue short = %s", got)
	}
	if IssueVolumeName("acme/rocket", 7) == VolumeName("acme/rocket", 7) {
		t.Error("issue and pull request share a volume")
	}
	longIssue := IssueVolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345)
	if len(longIssue) > 63 || !strings.HasSuffix(longIssue, "-12345") || !strings.HasPrefix(longIssue, "issue-") {
		t.Errorf("long issue = %s (%d)", longIssue, len(longIssue))
	}

	if got := RepoVolumeName("acme/rocket", "main"); got != "repo-acme-rocket-main" {
		t.Errorf("repo short = %s", got)
	}
	if got := RepoVolumeName("acme/rocket", ""); got != "repo-acme-rocket-default" {
		t.Errorf("repo empty branch = %s", got)
	}
	if got := RepoVolumeName("Acme_Co/rocket.js", "feat/dark_mode.v2"); got != "repo-acme-co-rocket-js-feat-dark-mode-v2" {
		t.Errorf("repo odd chars = %s", got)
	}
	if RepoVolumeName("acme/rocket", "7") == VolumeName("acme/rocket", 7) ||
		RepoVolumeName("acme/rocket", "7") == IssueVolumeName("acme/rocket", 7) {
		t.Error("repo volume collides with PR or issue volume")
	}
	longRepo1 := RepoVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-alpha")
	longRepo2 := RepoVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-beta")
	for _, got := range []string{longRepo1, longRepo2} {
		if len(got) > 63 || !strings.HasPrefix(got, "repo-") || !dns1123Label.MatchString(got) {
			t.Errorf("invalid long repo volume = %q (%d)", got, len(got))
		}
	}
	if longRepo1 != RepoVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-alpha") {
		t.Error("long repo volume is not deterministic")
	}
	if longRepo1 == longRepo2 {
		t.Errorf("two distinct long repo branches share a volume: %s", longRepo1)
	}
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func dig(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			v = v.(map[string]any)[k]
		case int:
			v = v.([]any)[k]
		}
	}
	return v
}

// The sandbox is the template's pod plus the volume, labelled so the
// dashboard knows it, and the template itself is left alone.
func TestVolumeSandboxMountsTheVolumeOnTheTemplatesPod(t *testing.T) {
	pod := map[string]any{"spec": map[string]any{
		"restartPolicy": "OnFailure",
		"containers":    []any{map[string]any{"name": "agent", "image": "ghcr.io/x/pi:latest"}},
	}}
	raw, err := volumeSandboxManifest("pi-template", pod, "pr-acme-rocket-7", "/home/node/workspace/rocket")
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, raw)
	if m["kind"] != "Sandbox" || dig(m, "metadata", "labels", VolumeLabel) != "pr-acme-rocket-7" ||
		dig(m, "metadata", "annotations", templateRefKey) != "pi-template" {
		t.Errorf("metadata = %v", m["metadata"])
	}
	if g := dig(m, "metadata", "generateName").(string); !strings.HasPrefix(g, "pi-pr-acme-rocket-7") {
		t.Errorf("generateName = %s", g)
	}
	if dig(m, "spec", "podTemplate", "spec", "volumes", 0, "persistentVolumeClaim", "claimName") != "pr-acme-rocket-7" {
		t.Error("the volume is not in the pod")
	}
	mount := dig(m, "spec", "podTemplate", "spec", "containers", 0, "volumeMounts", 0)
	if dig(mount, "mountPath") != "/home/node/workspace/rocket" || dig(mount, "subPath") != VolumeRepoDir {
		t.Errorf("the clone is not mounted: %v", mount)
	}
	if _, touched := pod["spec"].(map[string]any)["volumes"]; touched {
		t.Error("the template's pod was changed")
	}
}

func TestCloneJobRunsOnceOnTheVolume(t *testing.T) {
	m := decode(t, cloneJobManifest("pr-acme-rocket-7", "ghcr.io/x/pi:latest", "git clone x ."))
	spec := dig(m, "spec", "template", "spec")
	if dig(m, "spec", "backoffLimit") != 0.0 || dig(spec, "restartPolicy") != "Never" {
		t.Error("the clone would retry")
	}
	if dig(spec, "volumes", 0, "persistentVolumeClaim", "claimName") != "pr-acme-rocket-7" ||
		dig(spec, "containers", 0, "workingDir") != "/volume" ||
		dig(spec, "containers", 0, "command", 2) != "git clone x ." ||
		dig(spec, "containers", 0, "resources", "limits", "cpu") != "500m" {
		t.Errorf("job spec = %v", spec)
	}
}

func TestVolumeManifestIsUnowned(t *testing.T) {
	m := decode(t, volumeManifest("pr-acme-rocket-7", VolumeFor{Repo: "acme/rocket", Number: 7, Branch: "fix-auth"}))
	if _, owned := dig(m, "metadata").(map[string]any)["ownerReferences"]; owned {
		t.Error("the volume has an owner, so it would not outlive it")
	}
	if dig(m, "metadata", "annotations", repoAnnotation) != "acme/rocket" || dig(m, "metadata", "annotations", prAnnotation) != "7" {
		t.Errorf("annotations = %v", dig(m, "metadata", "annotations"))
	}
	im := decode(t, volumeManifest("issue-acme-rocket-42", VolumeFor{Repo: "acme/rocket", Number: 42, Branch: "fix-login", Kind: VolumeIssue}))
	if _, owned := dig(im, "metadata").(map[string]any)["ownerReferences"]; owned {
		t.Error("the issue volume has an owner, so it would not outlive it")
	}
	if dig(im, "metadata", "annotations", repoAnnotation) != "acme/rocket" ||
		dig(im, "metadata", "annotations", issueAnnotation) != "42" ||
		dig(im, "metadata", "annotations", branchAnnotation) != "fix-login" {
		t.Errorf("issue annotations = %v", dig(im, "metadata", "annotations"))
	}
	rm := decode(t, volumeManifest("repo-acme-rocket-develop", VolumeFor{Repo: "acme/rocket", Branch: "develop", Kind: VolumeRepo}))
	if _, owned := dig(rm, "metadata").(map[string]any)["ownerReferences"]; owned {
		t.Error("the repo volume has an owner, so it would not outlive it")
	}
	rAnn := dig(rm, "metadata", "annotations").(map[string]any)
	if rAnn[repoAnnotation] != "acme/rocket" || rAnn[branchAnnotation] != "develop" {
		t.Errorf("repo annotations = %v", rAnn)
	}
	if _, hasPR := rAnn[prAnnotation]; hasPR {
		t.Errorf("repo manifest unexpectedly has PR annotation: %v", rAnn)
	}
	if _, hasIssue := rAnn[issueAnnotation]; hasIssue {
		t.Errorf("repo manifest unexpectedly has issue annotation: %v", rAnn)
	}
}

func TestASandboxOnAVolumeCountsAsClaimed(t *testing.T) {
	raw := []byte(`{"metadata":{"name":"pi-pr-acme-rocket-7-x1","labels":{"nightshift.dev/volume":"pr-acme-rocket-7"},
		"annotations":{"agents.x-k8s.io/sandbox-template-ref":"pi-template"}}}`)
	warm := []byte(`{"metadata":{"name":"pi-pool-a","ownerReferences":[{"kind":"SandboxWarmPool","name":"pi-pool"}]}}`)
	boxes, err := buildSandboxes(sandboxList{Items: []json.RawMessage{raw, warm}}, podList{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b := boxes[1]
	if b.Name != "pi-pr-acme-rocket-7-x1" || !b.Claimed() || b.Volume != "pr-acme-rocket-7" || b.Owner != "pr-acme-rocket-7" || b.Template != "pi-template" {
		t.Errorf("box = %+v", b)
	}
	if b.PodRole() != RoleClaimed || b.Warm() {
		t.Error("a volume sandbox must be classified as claimed, not warm")
	}
	if boxes[0].Claimed() || OwnerRank(boxes[0]) != 0 || OwnerRank(b) != 1 {
		t.Error("ranks are off")
	}
}

func TestEgressWiringAddsProxyCAAndPlaceholdersWithoutRealSecrets(t *testing.T) {
	pod := map[string]any{"spec": map[string]any{
		"restartPolicy": "OnFailure",
		"containers":    []any{map[string]any{"name": "agent", "image": "ghcr.io/x/pi:latest"}},
	}}
	raw, err := volumeSandboxManifestWithEgress("pi-template", pod, "pr-acme-rocket-7", "/home/node/workspace/rocket", "http://10.96.0.42:8080")
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, raw)
	spec := dig(m, "spec", "podTemplate", "spec").(map[string]any)
	if spec["automountServiceAccountToken"] != false {
		t.Errorf("automountServiceAccountToken = %v, want false", spec["automountServiceAccountToken"])
	}
	vols := spec["volumes"].([]any)
	if len(vols) != 2 || dig(vols[1], "configMap", "name") != EgressCAConfigMap {
		t.Errorf("volumes = %v, want egress-ca ConfigMap", vols)
	}
	c := spec["containers"].([]any)[0].(map[string]any)
	mounts := c["volumeMounts"].([]any)
	if len(mounts) != 2 || dig(mounts[1], "mountPath") != egressCAMountDir || dig(mounts[1], "readOnly") != true {
		t.Errorf("mounts = %v", mounts)
	}
	envMap := map[string]string{}
	for _, rawEnv := range c["env"].([]any) {
		em := rawEnv.(map[string]any)
		envMap[em["name"].(string)] = em["value"].(string)
	}
	for k, want := range map[string]string{
		"HTTP_PROXY":              "http://10.96.0.42:8080",
		"HTTPS_PROXY":             "http://10.96.0.42:8080",
		"NODE_EXTRA_CA_CERTS":     egressCACertPath,
		"SSL_CERT_FILE":           egressCACertPath,
		"GIT_SSL_CAINFO":          egressCACertPath,
		"CLAUDE_CODE_OAUTH_TOKEN": "nightshift:secret:CLAUDE_ACCESS_TOKEN",
		"OPENAI_API_KEY":          "nightshift:secret:OPENAI_API_KEY",
		"GEMINI_API_KEY":          "nightshift:secret:GEMINI_API_KEY",
		"GITHUB_TOKEN":            "nightshift:secret:GITHUB_TOKEN",
	} {
		if envMap[k] != want {
			t.Errorf("env[%s] = %q, want %q", k, envMap[k], want)
		}
	}

	// Clone job gets proxy + CA + GITHUB_TOKEN placeholder, but not LLM API key placeholders.
	jobM := decode(t, cloneJobManifestWithEgress("pr-acme-rocket-7", "ghcr.io/x/pi:latest", "git clone x .", "http://10.96.0.42:8080"))
	jobSpec := dig(jobM, "spec", "template", "spec").(map[string]any)
	if jobSpec["automountServiceAccountToken"] != false {
		t.Errorf("clone job automountServiceAccountToken = %v, want false", jobSpec["automountServiceAccountToken"])
	}
	jobEnv := map[string]string{}
	for _, rawEnv := range dig(jobSpec, "containers", 0, "env").([]any) {
		em := rawEnv.(map[string]any)
		jobEnv[em["name"].(string)] = em["value"].(string)
	}
	if jobEnv["GITHUB_TOKEN"] != "nightshift:secret:GITHUB_TOKEN" || jobEnv["HTTPS_PROXY"] != "http://10.96.0.42:8080" {
		t.Errorf("clone job env missing proxy or GITHUB_TOKEN placeholder: %v", jobEnv)
	}
	if _, hasLLM := jobEnv["ANTHROPIC_API_KEY"]; hasLLM {
		t.Errorf("clone job should not receive LLM API key placeholder")
	}
	if _, hasOAuth := jobEnv["CLAUDE_CODE_OAUTH_TOKEN"]; hasOAuth {
		t.Errorf("clone job should not receive the oauth token placeholder")
	}
	if _, hasKey := envMap["ANTHROPIC_API_KEY"]; hasKey {
		t.Errorf("sandbox env must not set ANTHROPIC_API_KEY: it shadows oauth auth")
	}
}
