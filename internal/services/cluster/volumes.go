package cluster

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PoolTemplate is the SandboxTemplate a warm pool's sandboxes are made from.
func PoolTemplate(pool string) (string, error) {
	out, err := runKubectl(nil, "get", "sandboxwarmpool", pool,
		"-o", "jsonpath={.spec.sandboxTemplateRef.name}")
	if err != nil {
		return "", err
	}
	tpl := strings.TrimSpace(string(out))
	if tpl == "" {
		return "", fmt.Errorf("warm pool %s names no template", pool)
	}
	return tpl, nil
}

// KeptVolume is a volume as it sits in the cluster: what it keeps, and whether
// its clone finished.
type KeptVolume struct {
	Name    string
	For     VolumeFor
	Cloned  bool
	Created time.Time
}

// ListVolumes reads every volume Nightshift keeps.
func ListVolumes() ([]KeptVolume, error) {
	out, err := runKubectl(nil, "get", "pvc", "-l", VolumeLabel, "-o", "json")
	if err != nil {
		return nil, err
	}
	return parseVolumes(out)
}

func parseVolumes(raw []byte) ([]KeptVolume, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name              string            `json:"name"`
				CreationTimestamp string            `json:"creationTimestamp"`
				Annotations       map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse volumes: %w", err)
	}
	vols := make([]KeptVolume, 0, len(list.Items))
	for _, it := range list.Items {
		a := it.Metadata.Annotations
		v := KeptVolume{
			Name:    it.Metadata.Name,
			Cloned:  a[clonedAnnotation] != "",
			Created: parseTime(it.Metadata.CreationTimestamp),
			For:     VolumeFor{Repo: a[repoAnnotation], Branch: a[branchAnnotation], Kind: VolumeRepo},
		}
		switch {
		case a[prAnnotation] != "":
			v.For.Kind = VolumePR
			v.For.Number, _ = strconv.Atoi(a[prAnnotation])
		case a[issueAnnotation] != "":
			v.For.Kind = VolumeIssue
			v.For.Number, _ = strconv.Atoi(a[issueAnnotation])
		}
		vols = append(vols, v)
	}
	return vols, nil
}
