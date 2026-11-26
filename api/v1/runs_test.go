package v1

import (
	"strings"
	"testing"
)

func validRun() RunRequest {
	return RunRequest{
		Source: RunSource{CloneURL: "https://gitlab.example.com/group/sub/rocket.git"},
		Prompt: "fix the bug",
		Pool:   "claude-code-pool",
	}
}

func TestRunRequestAcceptsAnyHTTPSHost(t *testing.T) {
	r := validRun()
	r.Source.Ref, r.Source.Branch = "refs/pull/7/head", "fix/eng-42"
	if why := r.Problem(); why != "" {
		t.Errorf("Problem() = %q", why)
	}
	if got := r.Source.RepoName(); got != "rocket" {
		t.Errorf("RepoName() = %q", got)
	}
}

func TestRunRequestRefusesWhatCannotBeRunSafely(t *testing.T) {
	cases := map[string]func(*RunRequest){
		"ssh":              func(r *RunRequest) { r.Source.CloneURL = "git@github.com:a/b.git" },
		"file":             func(r *RunRequest) { r.Source.CloneURL = "file:///etc" },
		"helper":           func(r *RunRequest) { r.Source.CloneURL = "ext::sh -c id" },
		"credentials":      func(r *RunRequest) { r.Source.CloneURL = "https://u:p@example.com/a/b" },
		"query":            func(r *RunRequest) { r.Source.CloneURL = "https://example.com/a/b?x=1" },
		"no repository":    func(r *RunRequest) { r.Source.CloneURL = "https://example.com/" },
		"dot directory":    func(r *RunRequest) { r.Source.CloneURL = "https://example.com/a/.." },
		"flag as ref":      func(r *RunRequest) { r.Source.Ref = "--upload-pack=id" },
		"flag as branch":   func(r *RunRequest) { r.Source.Branch = "-f" },
		"flag as model":    func(r *RunRequest) { r.Model = "--help" },
		"no prompt":        func(r *RunRequest) { r.Prompt = " " },
		"huge prompt":      func(r *RunRequest) { r.Prompt = strings.Repeat("a", MaxPrompt+1) },
		"no pool":          func(r *RunRequest) { r.Pool = "" },
		"short timeout":    func(r *RunRequest) { r.TimeoutSeconds = 1 },
		"long timeout":     func(r *RunRequest) { r.TimeoutSeconds = MaxRunSeconds + 1 },
		"long externalRef": func(r *RunRequest) { r.ExternalRef = strings.Repeat("a", MaxExternalRef+1) },
	}
	for name, spoil := range cases {
		r := validRun()
		spoil(&r)
		if r.Problem() == "" {
			t.Errorf("%s: accepted %+v", name, r.Source)
		}
	}
}
