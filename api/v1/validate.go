package v1

import (
	"regexp"
	"strings"
)

// BranchProblem checks git's check-ref-format rules for a branch name. It
// returns "" when the name is fine, and a short reason when it is not. The
// server applies it to every request, and a frontend can apply it as the person
// types, so both agree on what a branch can be called.
func BranchProblem(name string) string {
	switch {
	case name == "":
		return "enter a branch name"
	case name == "@", badEdge(name), badMiddle(name):
		return "invalid git branch name"
	}
	for _, r := range name {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("\x7e^:?*[\\", r) {
			return "invalid git branch name"
		}
	}
	return ""
}

// badEdge is whether name starts or ends the way a ref cannot.
func badEdge(name string) bool {
	return strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock")
}

// badMiddle is whether name holds a sequence a ref cannot.
func badMiddle(name string) bool {
	for _, bad := range []string{"..", "//", "@{", "/.", ".lock/"} {
		if strings.Contains(name, bad) {
			return true
		}
	}
	return false
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
			return "target.number is required for a " + t.Kind
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
