package difflines

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	raw := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,0 +2,2 @@\n+x\n+y\n@@ -9 +11 @@\n-z\n+w\n" +
		"diff --git a/gone.go b/gone.go\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n"
	got := Parse(raw)
	for _, n := range []int{2, 3, 11} {
		if !got["a.go"][n] {
			t.Errorf("line %d not marked as added", n)
		}
	}
	if got["a.go"][4] {
		t.Error("line 4 marked as added")
	}
	if _, ok := got["gone.go"]; ok {
		t.Error("deleted file has added lines")
	}
}

func TestLoadCountsUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"diff": "", "untracked": "new.go\n", "new.go": "a\nb\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	got, err := Load(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	if !got["new.go"][1] || !got["new.go"][2] {
		t.Errorf("untracked lines = %v", got["new.go"])
	}
}

func TestLoadReportsMissingInput(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if _, err := Load(root, "."); err == nil {
		t.Error("Load succeeded without a saved diff")
	}
}
