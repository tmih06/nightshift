package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A run's record is kept on its volume, as an annotation, so it lasts exactly
// as long as the work it describes and survives a restart of the services.
const (
	// RunLabel marks the volume of a run.
	RunLabel      = "nightshift.dev/run"
	runAnnotation = "nightshift.dev/run-record"

	// runCallTimeout bounds what a run asks of kubectl.
	runCallTimeout = 30 * time.Second
)

// bounded runs kubectl on Nightshift's namespace, giving up after
// runCallTimeout.
func bounded(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runCallTimeout)
	defer cancel()
	return kubectlIn(ctx, Namespace, stdin, args)
}

// CreateRunVolume creates the volume of a run with its record on it. created is
// false when a volume of that name exists already.
func CreateRunVolume(name, repo, branch string, record []byte) (created bool, err error) {
	manifest := pvcManifest(name,
		map[string]string{VolumeLabel: name, RunLabel: name},
		map[string]string{repoAnnotation: repo, branchAnnotation: branch, runAnnotation: string(record)})
	if _, err := bounded(manifest, "create", "-f", "-"); err != nil {
		if strings.Contains(err.Error(), "(AlreadyExists)") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RunRecords is the record on each run's volume, by volume name. Volumes being
// deleted are left out.
func RunRecords() (map[string][]byte, error) {
	out, err := bounded(nil, "get", "pvc", "-l", RunLabel, "-o", "json")
	if err != nil {
		return nil, err
	}
	return parseRunRecords(out)
}

func parseRunRecords(raw []byte) (map[string][]byte, error) {
	var list pvcList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse run volumes: %w", err)
	}
	records := make(map[string][]byte, len(list.Items))
	for _, it := range list.Items {
		if rec := it.Metadata.Annotations[runAnnotation]; rec != "" && it.Metadata.DeletionTimestamp == "" {
			records[it.Metadata.Name] = []byte(rec)
		}
	}
	return records, nil
}

// annotationPatch merges annotations into an object.
type annotationPatch struct {
	Metadata struct {
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
}

// SaveRunRecord replaces the record on a run's volume.
func SaveRunRecord(name string, record []byte) error {
	var patch annotationPatch
	patch.Metadata.Annotations = map[string]string{runAnnotation: string(record)}
	_, err := bounded(mustJSON(patch), "patch", "pvc", name, "--type=merge", "--patch-file=/dev/stdin")
	return err
}

// ExecInPod runs a command in a pod's first container, feeding it stdin, and
// returns what it printed. It gives up after runCallTimeout.
func ExecInPod(pod string, stdin []byte, args ...string) ([]byte, error) {
	return bounded(stdin, append([]string{"exec", "-i", pod, "--"}, args...)...)
}
