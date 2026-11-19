// Package session owns the terminal sessions that outlive navigation.
//
// One tmux session per sandbox pod, named after the pod. The session's
// single pane runs the kubectl exec tunnel into the sandbox, so detaching
// the dashboard leaves bash running: the tmux server holds the kubectl
// process, and the kubectl process holds the pod's exec channel open.
// When the pod dies the pane command exits and tmux reaps the session,
// so sessions never outlive their sandbox.
package session

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tmih06/nightshift/internal/cluster"
)

// Name returns the tmux session name for a sandbox pod.
func Name(pod string) string {
	return "sbx-" + pod
}

// paneCommand is the shell command a session's pane runs. It is a single
// argument to tmux, and pod names cannot contain spaces, so no quoting
// is needed.
func paneCommand(pod string) string {
	return strings.Join([]string{
		"kubectl", "--context", cluster.KubeContext,
		"-n", cluster.Namespace, "exec", "-it", pod, "--", "bash",
	}, " ")
}

// Ensure creates the session for a pod if it does not exist. Attaching
// afterwards is guaranteed to find a session.
func Ensure(pod string) error {
	if Has(pod) {
		return nil
	}
	cmd := exec.Command("tmux", "new-session", "-d", "-s", Name(pod), paneCommand(pod))
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux new-session %s: %w: %s",
			Name(pod), err, strings.TrimSpace(errOut.String()))
	}
	return nil
}

// Has reports whether the pod has a live session.
func Has(pod string) bool {
	return exec.Command("tmux", "has-session", "-t", Name(pod)).Run() == nil
}

// Attach returns the command that attaches to the pod's session. Detach
// with the tmux prefix, C-b d.
func Attach(pod string) *exec.Cmd {
	return exec.Command("tmux", "attach-session", "-t", Name(pod))
}

// Existing returns the set of the given pods that have a live session,
// using one tmux call.
func Existing(pods []string) map[string]bool {
	live := map[string]bool{}
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return live
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names[line] = true
		}
	}
	for _, pod := range pods {
		if names[Name(pod)] {
			live[pod] = true
		}
	}
	return live
}
