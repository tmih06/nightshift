package cluster

import (
	"encoding/json"
	"fmt"
	v1 "github.com/tmih06/nightshift/api/v1"
	"strconv"
	"strings"
	"time"
)

// A pull request's volume is a PersistentVolumeClaim Nightshift creates and
// owns, not one from a claim's volumeClaimTemplates: the controller makes
// those owned by the sandbox, so they would go with it. Nothing owns this
// one, so destroying a sandbox on it leaves the clone and its work in place.
const (
	// VolumeLabel names the volume on the volume itself, its clone Job, and
	// the sandbox that mounts it.
	VolumeLabel = "nightshift.dev/volume"

	clonedAnnotation = "nightshift.dev/cloned"
	repoAnnotation   = "nightshift.dev/repo"
	prAnnotation     = "nightshift.dev/pull-request"
	issueAnnotation  = "nightshift.dev/issue"
	branchAnnotation = "nightshift.dev/branch"
	templateRefKey   = "agents.x-k8s.io/sandbox-template-ref"

	volumeSize = "2Gi"

	// VolumeRepoDir is the clone's directory on the volume, and what the
	// sandbox mounts. The volume's root belongs to root, where git, run as
	// the sandbox's user, refuses to work; a directory that user made is its
	// own.
	VolumeRepoDir = "repo"
)

// VolumeState is whether the volume exists and whether a clone into it
// finished. A volume without a finished clone is cloned again from scratch.
func VolumeState(name string) (exists, cloned bool, err error) {
	out, err := runKubectl(nil, "get", "pvc", name, "--ignore-not-found", "-o", "json")
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return false, false, err
	}
	var pvc struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &pvc); err != nil {
		return true, false, fmt.Errorf("parse pvc %s: %w", name, err)
	}
	return true, pvc.Metadata.Annotations[clonedAnnotation] != "", nil
}

// VolumeKind distinguishes what kind of GitHub target a volume keeps.
type VolumeKind int

const (
	VolumePR VolumeKind = iota
	VolumeIssue
	VolumeRepo
)

// VolumeFor records which pull request, issue, or repository branch a volume keeps.
type VolumeFor struct {
	Repo   string
	Number int
	Branch string
	Kind   VolumeKind
}

// CreateVolume creates the volume, or leaves it be if it exists.
func CreateVolume(name string, target VolumeFor) error {
	_, err := runKubectl(volumeManifest(name, target), "apply", "-f", "-")
	return err
}

func volumeManifest(name string, target VolumeFor) []byte {
	annotations := map[string]string{
		repoAnnotation:   target.Repo,
		branchAnnotation: target.Branch,
	}
	switch target.Kind {
	case VolumePR:
		annotations[prAnnotation] = strconv.Itoa(target.Number)
	case VolumeIssue:
		annotations[issueAnnotation] = strconv.Itoa(target.Number)
	case VolumeRepo:
		// Repo volumes only carry repo and branch annotations.
	}
	return mustJSON(map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata": map[string]any{
			"name":        name,
			"namespace":   Namespace,
			"labels":      map[string]string{VolumeLabel: name},
			"annotations": annotations,
		},
		"spec": map[string]any{
			"accessModes": []string{"ReadWriteOnce"},
			"resources":   map[string]any{"requests": map[string]string{"storage": volumeSize}},
		},
	})
}

// TemplatePod is a SandboxTemplate's pod template and its first image.
func TemplatePod(template string) (map[string]any, string, error) {
	out, err := runKubectl(nil, "get", "sandboxtemplate", template, "-o", "json")
	if err != nil {
		return nil, "", err
	}
	var tpl struct {
		Spec struct {
			PodTemplate map[string]any `json:"podTemplate"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &tpl); err != nil {
		return nil, "", fmt.Errorf("parse sandboxtemplate %s: %w", template, err)
	}
	if tpl.Spec.PodTemplate == nil {
		return nil, "", fmt.Errorf("sandboxtemplate %s has no pod template", template)
	}
	image := ""
	for _, c := range containersOf(tpl.Spec.PodTemplate) {
		if s, ok := c["image"].(string); ok {
			image = s
			break
		}
	}
	if image == "" {
		return nil, "", fmt.Errorf("sandboxtemplate %s names no image", template)
	}
	return tpl.Spec.PodTemplate, image, nil
}

func containersOf(podTemplate map[string]any) []map[string]any {
	spec, _ := podTemplate["spec"].(map[string]any)
	list, _ := spec["containers"].([]any)
	var out []map[string]any
	for _, c := range list {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// CloneIntoVolume runs script at the volume's root in a Job on image (the
// script clones into VolumeRepoDir), the agent's own so the files belong to
// the sandbox's user, and marks the volume cloned once it succeeds. The Job
// is deleted either way. progress gets the Job's latest log line about once
// a second, or "" while its pod is not running yet.
func CloneIntoVolume(volume, image, script string, timeout time.Duration, progress func(string)) error {
	out, err := runKubectl(cloneJobManifestWithEgress(volume, image, script, EgressProxyURL()), "create", "-f", "-", "-o", "jsonpath={.metadata.name}")
	if err != nil {
		return err
	}
	job := strings.TrimSpace(string(out))
	defer func() { _, _ = runKubectl(nil, "delete", "job", job, "--wait=false") }()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := runKubectl(nil, "get", "job", job, "-o", "jsonpath={.status.succeeded}/{.status.failed}")
		if err == nil {
			succeeded, failed, _ := strings.Cut(strings.TrimSpace(string(out)), "/")
			switch {
			case succeeded != "" && succeeded != "0":
				_, err := runKubectl(nil, "annotate", "--overwrite", "pvc", volume,
					clonedAnnotation+"="+time.Now().UTC().Format(time.RFC3339))
				return err
			case failed != "" && failed != "0":
				logs, _ := runKubectl(nil, "logs", "job/"+job, "--tail=5")
				return fmt.Errorf("clone job %s failed: %s", job, strings.TrimSpace(string(logs)))
			}
		}
		if progress != nil {
			out, err := runKubectl(nil, "logs", "job/"+job, "--tail=1")
			line := ""
			if err == nil {
				line = strings.TrimSpace(string(out))
			}
			progress(line)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("clone job %s did not finish within %s", job, timeout)
}

func cloneJobManifest(volume, image, script string) []byte {
	return cloneJobManifestWithEgress(volume, image, script, "")
}

func cloneJobManifestWithEgress(volume, image, script, proxyURL string) []byte {
	prefix := volume
	if len(prefix) > 50 {
		prefix = strings.TrimRight(prefix[:50], "-")
	}
	podSpec := map[string]any{
		"restartPolicy": "Never",
		"containers": []any{map[string]any{
			"name":            "clone",
			"image":           image,
			"imagePullPolicy": "IfNotPresent",
			"command":         []string{"bash", "-c", script},
			"workingDir":      "/volume",
			"volumeMounts":    []any{map[string]any{"name": "repo", "mountPath": "/volume"}},
			"resources": map[string]any{
				"requests": map[string]string{"cpu": "100m", "memory": "128Mi"},
				"limits":   map[string]string{"cpu": "500m", "memory": "512Mi"},
			},
		}},
		"volumes": []any{map[string]any{
			"name":                  "repo",
			"persistentVolumeClaim": map[string]any{"claimName": volume},
		}},
	}
	applyEgressToPodSpec(podSpec, proxyURL, false)
	return mustJSON(map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"generateName": "clone-" + prefix + "-",
			"namespace":    Namespace,
			"labels":       map[string]string{VolumeLabel: volume},
		},
		"spec": map[string]any{
			"backoffLimit":            0,
			"ttlSecondsAfterFinished": 600,
			"template": map[string]any{
				"metadata": map[string]any{"labels": map[string]string{VolumeLabel: volume}},
				"spec":     podSpec,
			},
		},
	})
}

// CreateVolumeSandbox starts a Sandbox from template's pod with the volume's
// VolumeRepoDir mounted at mountPath in every container, and returns its
// name, which is
// also its pod's. Nothing owns it: DeleteSandbox ends it, and the volume
// stays.
func CreateVolumeSandbox(template string, podTemplate map[string]any, volume, mountPath string) (string, error) {
	manifest, err := volumeSandboxManifestWithEgress(template, podTemplate, volume, mountPath, EgressProxyURL())
	if err != nil {
		return "", err
	}
	out, err := runKubectl(manifest, "create", "-f", "-", "-o", "jsonpath={.metadata.name}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func volumeSandboxManifest(template string, podTemplate map[string]any, volume, mountPath string) ([]byte, error) {
	return volumeSandboxManifestWithEgress(template, podTemplate, volume, mountPath, "")
}

func volumeSandboxManifestWithEgress(template string, podTemplate map[string]any, volume, mountPath, proxyURL string) ([]byte, error) {
	var pod map[string]any
	if err := json.Unmarshal(mustJSON(podTemplate), &pod); err != nil { // a deep copy
		return nil, err
	}
	spec, ok := pod["spec"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sandboxtemplate %s has no pod spec", template)
	}
	volumes, _ := spec["volumes"].([]any)
	spec["volumes"] = append(volumes, map[string]any{
		"name":                  "repo",
		"persistentVolumeClaim": map[string]any{"claimName": volume},
	})
	containers := containersOf(pod)
	if len(containers) == 0 {
		return nil, fmt.Errorf("sandboxtemplate %s has no containers", template)
	}
	for _, c := range containers {
		mounts, _ := c["volumeMounts"].([]any)
		c["volumeMounts"] = append(mounts, map[string]any{"name": "repo", "mountPath": mountPath, "subPath": VolumeRepoDir})
	}
	applyEgressToPodSpec(spec, proxyURL, true)
	agent := v1.DNSLabel(strings.TrimSuffix(template, "-template"))
	if len(agent) > 30 {
		agent = strings.TrimRight(agent[:30], "-")
	}
	return mustJSON(map[string]any{
		"apiVersion": "agents.x-k8s.io/v1beta1",
		"kind":       "Sandbox",
		"metadata": map[string]any{
			"generateName": agent + "-" + volume[:min(len(volume), 26)] + "-",
			"namespace":    Namespace,
			"labels":       map[string]string{VolumeLabel: volume},
			"annotations":  map[string]string{templateRefKey: template},
		},
		"spec": map[string]any{"podTemplate": pod},
	}), nil
}

// WaitForPod waits until pod runs. progress gets its phase and, while its
// container waits, why: "Pending ContainerCreating" while the image pulls.
func WaitForPod(pod string, timeout time.Duration, progress func(string)) error {
	deadline := time.Now().Add(timeout)
	phase := ""
	for time.Now().Before(deadline) {
		out, err := runKubectl(nil, "get", "pod", pod, "--ignore-not-found", "-o",
			"jsonpath={.status.phase} {.status.containerStatuses[0].state.waiting.reason}")
		if err == nil {
			state := strings.TrimSpace(string(out))
			phase, _, _ = strings.Cut(state, " ")
			if phase == "Running" {
				return nil
			}
			if progress != nil {
				progress(state)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if phase == "" {
		phase = "not created"
	}
	return fmt.Errorf("pod %s is %s after %s", pod, phase, timeout)
}

// DeleteSandbox ends a sandbox Nightshift started on a volume; its pod goes
// with it, and the volume stays.
func DeleteSandbox(name string) error {
	_, err := runKubectl(nil, "delete", "sandbox", name)
	return err
}

// DeleteVolume deletes a pull request's volume and the clone on it. Kubernetes
// holds the deletion until no pod mounts it, so this does not wait.
func DeleteVolume(name string) error {
	_, err := runKubectl(nil, "delete", "pvc", name, "--wait=false")
	return err
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
