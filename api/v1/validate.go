package v1

import (
	"fmt"
	"regexp"
	"strings"
)

// BranchProblem checks git's check-ref-format rules for a branch name. It
// returns "" when the name is fine, and a short reason when it is not. The
// server applies it to every request, and a frontend can apply it as the person
// types, so both agree on what a branch can be called.
func BranchProblem(name string) string {
	if name == "" {
		return "enter a branch name"
	}
	if name == "@" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, "/") ||
		strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") ||
		strings.Contains(name, "..") || strings.Contains(name, "//") ||
		strings.Contains(name, "@{") || strings.Contains(name, "/.") ||
		strings.Contains(name, ".lock/") {
		return "invalid git branch name"
	}
	for _, r := range name {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("\x7e^:?*[\\", r) {
			return "invalid git branch name"
		}
	}
	return ""
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+$`)

// Problem is why the request cannot be acted on, or "" when it can.
func (r CheckoutRequest) Problem() string {
	t := r.Target
	switch {
	case !repoName.MatchString(t.Repo):
		return "target.repo must be owner/name"
	case r.Pool == "":
		return "pool is required"
	}
	switch t.Kind {
	case TargetPullRequest, TargetIssue:
		if t.Number < 1 {
			return fmt.Sprintf("target.number is required for a %s", t.Kind)
		}
	case TargetBranch:
	default:
		return "target.kind must be pull_request, issue, or branch"
	}
	if t.Branch != "" {
		if why := BranchProblem(t.Branch); why != "" {
			return "target.branch: " + why
		}
	}
	return ""
}
