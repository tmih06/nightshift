package v1

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Volume names are part of the contract. A volume that keeps a pull request,
// an issue, or a branch has a name anyone can work out from the target, so a
// frontend can show it before the volume exists and recognise it afterwards.
// The names are valid Kubernetes names of at most 63 characters, cut and
// hashed when the target is too long.

// PRVolumeName is the volume that keeps repo's pull request number:
// pr-<owner>-<name>-<number>, cut to a valid Kubernetes name.
func PRVolumeName(repo string, number int) string {
	return volumeName("pr", repo, number)
}

// IssueVolumeName is the volume that keeps repo's issue number:
// issue-<owner>-<name>-<number>, cut to a valid Kubernetes name.
func IssueVolumeName(repo string, number int) string {
	return volumeName("issue", repo, number)
}

// BranchVolumeName is the volume that keeps repo's branch:
// repo-<owner>-<name>-<branch>, cut to a valid <=63-char DNS-1123 label.
// An empty branch is treated as "default".
func BranchVolumeName(repo, branch string) string {
	if branch == "" {
		branch = "default"
	}
	branchPart := DNSLabel(branch)
	if branchPart == "" {
		branchPart = "default"
	}
	repoPart := DNSLabel(strings.ReplaceAll(repo, "/", "-"))
	name := "repo-" + branchPart
	if repoPart != "" {
		name = "repo-" + repoPart + "-" + branchPart
	}
	if len(name) > 63 {
		sum := sha256.Sum256([]byte(repo + "@" + branch))
		hash := fmt.Sprintf("-%x", sum[:3])
		name = strings.TrimRight(name[:63-len(hash)], "-") + hash
	}
	return name
}

func volumeName(prefix, repo string, number int) string {
	suffix := fmt.Sprintf("-%d", number)
	name := prefix + "-" + DNSLabel(strings.ReplaceAll(repo, "/", "-"))
	if len(name)+len(suffix) > 63 {
		sum := sha256.Sum256([]byte(repo))
		hash := fmt.Sprintf("-%x", sum[:3])
		name = strings.TrimRight(name[:63-len(suffix)-len(hash)], "-") + hash
	}
	return name + suffix
}

// DNSLabel lowercases s and turns anything a name cannot hold into dashes.
func DNSLabel(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
