package cluster

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	if b.ManifestYAML != "" {
		t.Error("a claimed sandbox got a warm sandbox's configs")
	}
	if boxes[0].Claimed() || OwnerRank(boxes[0]) != 0 || OwnerRank(b) != 1 {
		t.Error("ranks are off")
	}
}
