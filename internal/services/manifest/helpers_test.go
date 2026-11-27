package manifest

import (
	"encoding/json"
	"os"
	"testing"
)

// openRoot opens dir for reading and writing without following paths out of
// it, closing it when the test ends.
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

// readJSON reads rel under root as a JSON object.
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
