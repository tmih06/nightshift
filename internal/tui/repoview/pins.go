package repoview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type pinsFile struct {
	Pinned []string `json:"pinned"`
}

func defaultPinsPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "nightshift", "repos.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".config", "nightshift", "repos.json")
	}
	return filepath.Join(home, ".config", "nightshift", "repos.json")
}

func loadPins(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var pf pinsFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return normalizePins(pf.Pinned), nil
}

func savePins(path string, pins []string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(pinsFile{Pinned: normalizePins(pins)}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(dir, ".repos-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func normalizePins(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" || !strings.Contains(s, "/") {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}
