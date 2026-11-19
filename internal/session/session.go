// Package session manages one detached tmux session per sandbox pod
// running kubectl exec into bash, so sessions outlive dashboard exit.
package session

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/tmih06/nightshift/internal/cluster"
)

const prefix = "sbx-"

func Name(pod string) string {
	return prefix + pod
}

func paneCommand(pod string) string {
	return strings.Join([]string{
		"kubectl", "--context", cluster.KubeContext,
		"-n", cluster.Namespace, "exec", "-it", pod, "--", "bash",
	}, " ")
}

func Ensure(pod string) error {
	if Has(pod) {
		return nil
	}
	cmd := exec.Command("tmux", "new-session", "-d", "-s", Name(pod),
		"-x", "160", "-y", "40", paneCommand(pod))
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tmux new-session %s: %w: %s",
			Name(pod), err, strings.TrimSpace(errOut.String()))
	}
	return nil
}

func Has(pod string) bool {
	return exec.Command("tmux", "has-session", "-t", Name(pod)).Run() == nil
}

func Attach(pod string) *exec.Cmd {
	return exec.Command("tmux", "attach-session", "-t", Name(pod))
}

func Resize(pod string, w, h int) error {
	return exec.Command("tmux", "resize-window", "-t", Name(pod),
		"-x", strconv.Itoa(w), "-y", strconv.Itoa(h)).Run()
}

func Capture(pod string) ([]string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", Name(pod)).Output()
	if err != nil {
		return nil, fmt.Errorf("tmux capture-pane %s: %w", Name(pod), err)
	}
	lines := strings.Split(string(out), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

type Key struct {
	Text string // literal, sent with -l
	Name string // tmux key name
}

// SendBatch merges consecutive literal keys into one tmux send-keys call.
func SendBatch(pod string, keys []Key) error {
	for _, group := range sendGroups(keys) {
		args := append([]string{"send-keys", "-t", Name(pod)}, group...)
		if err := exec.Command("tmux", args...).Run(); err != nil {
			return fmt.Errorf("tmux send-keys: %w", err)
		}
	}
	return nil
}

func sendGroups(keys []Key) [][]string {
	var groups [][]string
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			groups = append(groups, []string{"-l", lit.String()})
			lit.Reset()
		}
	}
	for _, k := range keys {
		if k.Text != "" {
			lit.WriteString(k.Text)
		} else {
			flush()
			groups = append(groups, []string{k.Name})
		}
	}
	flush()
	return groups
}

// Live returns the set of pod names that currently have a live tmux session.
func Live() map[string]bool {
	live := map[string]bool{}
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return live
	}
	for _, line := range strings.Split(string(out), "\n") {
		if pod, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok && pod != "" {
			live[pod] = true
		}
	}
	return live
}
