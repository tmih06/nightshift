package agent

import (
	"bufio"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

// repo is the repository's root.
func repo(t *testing.T) *os.Root {
	t.Helper()
	r, err := os.OpenRoot("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

// imageCommand is the COMMAND an agent's images/agent/agents/<name>/agent.env
// sets, or "" when there is no such file.
func imageCommand(r *os.Root, name string) string {
	raw, err := r.ReadFile("images/agent/agents/" + name + "/agent.env")
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "COMMAND="); ok {
			return v
		}
	}
	return ""
}

// An agent is three things that must agree: this catalogue's entry, its image
// directory, and the template and pool k8s/warmpool.sh makes from that
// directory. A new agent missing any of them would offer a pool nobody can
// launch, or launch a CLI the image does not have.
func TestEveryAgentHasAnImageAndAPool(t *testing.T) {
	r := repo(t)
	pools, err := r.ReadFile("k8s/warmpool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, a := range All() {
		names[a.Name] = true
		if got := imageCommand(r, a.Name); got != a.Command {
			t.Errorf("%s: images/agent/agents/%s/agent.env sets COMMAND=%q, want %q", a.Name, a.Name, got, a.Command)
		}
		for _, want := range []string{
			"name: " + a.Name + "-template\n",
			"name: " + a.Name + "-pool\n",
			"image: ghcr.io/tmih06/nightshift-" + a.Name + ":latest\n",
		} {
			if !strings.Contains(string(pools), want) {
				t.Errorf("%s: k8s/warmpool.yaml has no %q; run make warmpool", a.Name, strings.TrimSpace(want))
			}
		}
		for _, s := range []string{a.Name + "-pool", a.Name + "-template"} {
			if got, ok := Match(s); !ok || got.Name != a.Name {
				t.Errorf("%s names %q, want %s", s, got.Name, a.Name)
			}
		}
		if a.Label == "" || len(a.Models) == 0 || a.Task == "" || strings.Count(a.Task, "%s") != 1 {
			t.Errorf("%s: incomplete entry %+v", a.Name, a)
		}
	}
	dirs, err := fs.ReadDir(r.FS(), "images/agent/agents")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if d.IsDir() && !names[d.Name()] {
			t.Errorf("images/agent/agents/%s has no catalogue entry", d.Name())
		}
	}
}

func TestMatchKeepsShortNamesToWholeWords(t *testing.T) {
	for s, want := range map[string]string{
		"pi-pool":                   "pi",
		"api-pool":                  "",
		"ompig-slim-template":       "ompig-slim",
		"my-claude-box":             "claude-code",
		"nightshift-devin:latest":   "devin",
		"legacy-agy-sandbox":        "agy",
		"agyle-pool":                "",
		"image: nightshift-codex:1": "codex",
	} {
		got, _ := Match(s)
		if got.Name != want {
			t.Errorf("Match(%q) = %q, want %q", s, got.Name, want)
		}
	}
}

func TestHarnessEndsWithTheCLIDefault(t *testing.T) {
	for _, a := range All() {
		h := a.Harness()
		last := h.Models[len(h.Models)-1]
		if h.Name != a.Name || h.Command != a.Command || last.ID != "" || last.Label != "default" {
			t.Errorf("%s: harness = %+v", a.Name, h)
		}
		if len(h.Models) != len(a.Models)+1 {
			t.Errorf("%s: harness models = %d, want %d", a.Name, len(h.Models), len(a.Models)+1)
		}
	}
}

func TestCommandsAreEveryCLIOnce(t *testing.T) {
	cmds := Commands()
	if len(cmds) != len(All()) {
		t.Fatalf("commands = %v", cmds)
	}
	for _, c := range cmds {
		if a, ok := ByCommand(c); !ok || a.Command != c {
			t.Errorf("ByCommand(%q) = %+v", c, a)
		}
	}
	sorted := slices.Clone(cmds)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(cmds) {
		t.Errorf("a CLI is listed twice: %v", cmds)
	}
}
