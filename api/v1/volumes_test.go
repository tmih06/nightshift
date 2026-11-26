package v1

import (
	"regexp"
	"strings"
	"testing"
)

var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestVolumeNamesAreValidAndStable(t *testing.T) {
	if got := PRVolumeName("acme/rocket", 7); got != "pr-acme-rocket-7" {
		t.Errorf("short = %s", got)
	}
	if got := PRVolumeName("Acme_Co/rocket.js", 12); got != "pr-acme-co-rocket-js-12" {
		t.Errorf("odd characters = %s", got)
	}
	long := PRVolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345)
	if len(long) > 63 || !strings.HasSuffix(long, "-12345") || long != PRVolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345) {
		t.Errorf("long = %s (%d)", long, len(long))
	}
	if PRVolumeName("a/b", 1) == PRVolumeName("a/b", 2) {
		t.Error("two pull requests share a volume")
	}
	if got := IssueVolumeName("acme/rocket", 42); got != "issue-acme-rocket-42" {
		t.Errorf("issue short = %s", got)
	}
	if IssueVolumeName("acme/rocket", 7) == PRVolumeName("acme/rocket", 7) {
		t.Error("issue and pull request share a volume")
	}
	longIssue := IssueVolumeName("some-very-long-organisation-name/an-even-longer-repository-name-here", 12345)
	if len(longIssue) > 63 || !strings.HasSuffix(longIssue, "-12345") || !strings.HasPrefix(longIssue, "issue-") {
		t.Errorf("long issue = %s (%d)", longIssue, len(longIssue))
	}

	if got := BranchVolumeName("acme/rocket", "main"); got != "repo-acme-rocket-main" {
		t.Errorf("repo short = %s", got)
	}
	if got := BranchVolumeName("acme/rocket", ""); got != "repo-acme-rocket-default" {
		t.Errorf("repo empty branch = %s", got)
	}
	if got := BranchVolumeName("Acme_Co/rocket.js", "feat/dark_mode.v2"); got != "repo-acme-co-rocket-js-feat-dark-mode-v2" {
		t.Errorf("repo odd chars = %s", got)
	}
	if BranchVolumeName("acme/rocket", "7") == PRVolumeName("acme/rocket", 7) ||
		BranchVolumeName("acme/rocket", "7") == IssueVolumeName("acme/rocket", 7) {
		t.Error("repo volume collides with PR or issue volume")
	}
	longRepo1 := BranchVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-alpha")
	longRepo2 := BranchVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-beta")
	for _, got := range []string{longRepo1, longRepo2} {
		if len(got) > 63 || !strings.HasPrefix(got, "repo-") || !dns1123Label.MatchString(got) {
			t.Errorf("invalid long repo volume = %q (%d)", got, len(got))
		}
	}
	if longRepo1 != BranchVolumeName("some-very-long-organisation-name/an-even-longer-repository-name", "feat/super-long-branch-name-alpha") {
		t.Error("long repo volume is not deterministic")
	}
	if longRepo1 == longRepo2 {
		t.Errorf("two distinct long repo branches share a volume: %s", longRepo1)
	}
}
