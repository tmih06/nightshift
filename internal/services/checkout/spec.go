// Package checkout puts a pull request, an issue, or a branch on a volume of
// its own and starts a sandbox on it. It is the pipeline that used to run
// inside the dashboard: it now runs in the services, so it carries on when a
// frontend goes away and any frontend can start one.
package checkout

import (
	"fmt"
	"strings"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/cluster"
)

// workspaceDir is the sandbox images' working directory (images/base), where a
// volume is mounted under the repository's name.
const workspaceDir = "/home/node/workspace"

// Spec is a target to keep on its own volume, and the agent to start on it.
type Spec struct {
	Target v1.Target
	// Harness is the command to start in the clone; empty leaves bash open.
	Harness string
	// Model is passed to the harness as --model; empty uses its default.
	Model string
}

// SpecOf is the spec a request asks for.
func SpecOf(r v1.CheckoutRequest) Spec {
	return Spec{Target: r.Target, Harness: r.Harness, Model: r.Model}
}

// Volume is the volume that keeps the target.
func (s Spec) Volume() string {
	switch s.Target.Kind {
	case v1.TargetIssue:
		return cluster.IssueVolumeName(s.Target.Repo, s.Target.Number)
	case v1.TargetBranch:
		return cluster.RepoVolumeName(s.Target.Repo, s.branch())
	default:
		return cluster.VolumeName(s.Target.Repo, s.Target.Number)
	}
}

func (s Spec) volumeKind() cluster.VolumeKind {
	switch s.Target.Kind {
	case v1.TargetIssue:
		return cluster.VolumeIssue
	case v1.TargetBranch:
		return cluster.VolumeRepo
	default:
		return cluster.VolumePR
	}
}

// MountPath is where the volume sits in the sandbox.
func (s Spec) MountPath() string {
	return workspaceDir + "/" + s.Target.Repo[strings.LastIndex(s.Target.Repo, "/")+1:]
}

func (s Spec) branch() string {
	if s.Target.Branch != "" {
		return s.Target.Branch
	}
	switch s.Target.Kind {
	case v1.TargetIssue:
		return fmt.Sprintf("issue-%d", s.Target.Number)
	case v1.TargetBranch:
		return "main"
	default:
		return fmt.Sprintf("pr-%d", s.Target.Number)
	}
}

// Branch is the branch the clone ends up on.
func (s Spec) Branch() string { return s.branch() }

// String names the target the way people write it: owner/name#7, or
// owner/name@branch.
func (s Spec) String() string {
	if s.Target.Kind == v1.TargetBranch {
		return fmt.Sprintf("%s@%s", s.Target.Repo, s.branch())
	}
	return fmt.Sprintf("%s#%d", s.Target.Repo, s.Target.Number)
}

// CloneScript runs at the volume's root in the clone Job. It drops what an
// unfinished clone left, clones over https into cluster.VolumeRepoDir, and
// checks out either the pull request's head ref (which works for forks too and
// needs no gh in the image) or the chosen branch for an issue or repository
// (tracking origin/<branch> if it already exists, or creating it from HEAD).
// It pins HTTP/1.1 with a low-speed timeout and retries transient stalls so a
// hung GitHub stream does not sit decaying for minutes. git's progress is
// carriage returns on one line, which the container log would hold back until
// the line ends, so it is turned into lines, unbuffered, to follow.
func (s Spec) CloneScript() string {
	dir := cluster.VolumeRepoDir
	lines := []string{
		"set -euo pipefail",
		`lines() { if command -v stdbuf >/dev/null; then stdbuf -o0 tr '\r' '\n'; else tr '\r' '\n'; fi; }`,
		"export GIT_CONFIG_COUNT=3",
		"export GIT_CONFIG_KEY_0=http.version GIT_CONFIG_VALUE_0=HTTP/1.1",
		"export GIT_CONFIG_KEY_1=http.lowSpeedLimit GIT_CONFIG_VALUE_1=1024",
		"export GIT_CONFIG_KEY_2=http.lowSpeedTime GIT_CONFIG_VALUE_2=20",
		`if [ -n "${GITHUB_TOKEN:-}" ]; then export GIT_ASKPASS=/tmp/git-askpass.sh GIT_TERMINAL_PROMPT=0; printf '#!/bin/sh\ncase "$1" in *Username*) echo x-access-token;; *) echo "$GITHUB_TOKEN";; esac\n' > "$GIT_ASKPASS"; chmod 700 "$GIT_ASKPASS"; fi`,
	}
	lines = append(lines, retryScriptLines(
		"clone",
		"rm -rf "+dir,
		"git clone --progress "+shellQuote("https://github.com/"+s.Target.Repo+".git")+" "+dir+" 2>&1 | lines",
	)...)
	lines = append(lines, "cd "+dir)
	if s.Target.Kind == v1.TargetIssue || s.Target.Kind == v1.TargetBranch {
		b := shellQuote(s.branch())
		ref := shellQuote("refs/remotes/origin/" + s.branch())
		lines = append(lines,
			"if git show-ref --verify --quiet "+ref+"; then git checkout -B "+b+" --track "+ref+"; else git checkout -B "+b+"; fi",
		)
	} else {
		lines = append(lines, retryScriptLines(
			"fetch",
			"",
			fmt.Sprintf("git fetch --progress origin pull/%d/head 2>&1 | lines", s.Target.Number),
		)...)
		lines = append(lines, "git checkout -B "+shellQuote(s.branch())+" FETCH_HEAD")
	}
	return strings.Join(lines, "\n")
}

func retryScriptLines(label, setup, cmd string) []string {
	out := []string{"for i in 1 2 3; do"}
	if setup != "" {
		out = append(out, "  "+setup)
	}
	return append(out,
		"  if "+cmd+"; then break; fi",
		`  [ "$i" -lt 3 ] || exit 1`,
		fmt.Sprintf(`  echo "%s stalled, retrying ($((i+1))/3)..."`, label),
		"  sleep 2",
		"done",
	)
}

// ShellLine is what the sandbox's bash is given after a fresh clone: into the
// clone, and where it stands.
func (s Spec) ShellLine() string {
	return s.shellLine(false)
}

// shellLine is what the sandbox's bash is given. When an issue or repository
// checkout reuses a kept volume, it also checks out (or creates) the requested
// branch without resetting any existing local branch. When a coding agent
// harness was selected, it launches the harness (with the chosen model, if
// any) directly inside the repository directory.
func (s Spec) shellLine(cloned bool) string {
	var line string
	if (s.Target.Kind == v1.TargetIssue || s.Target.Kind == v1.TargetBranch) && cloned {
		b := shellQuote(s.branch())
		line = "cd " + shellQuote(s.MountPath()) + " && (git checkout " + b + " 2>/dev/null || git checkout -b " + b + ") && git status -sb"
	} else {
		line = "cd " + shellQuote(s.MountPath()) + " && git status -sb"
	}
	if cmd := LaunchCommand(s.Harness, s.Model); cmd != "" {
		line += " && " + cmd
	}
	return line
}

func (s Spec) harnessSummary() string {
	h := strings.TrimSpace(s.Harness)
	if h == "" {
		return ""
	}
	if m := strings.TrimSpace(s.Model); m != "" {
		return h + " (" + m + ")"
	}
	return h
}
