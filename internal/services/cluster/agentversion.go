package cluster

import (
	"fmt"
	"strings"
)

// agentProbe prints the first of commands' --version answers, else the version
// file images/agent stamps as "<agent> <semver>" at build time. The CLI is
// asked first so a self-updated binary reports what it runs, not what the
// image was built with.
func agentProbe(commands []string) string {
	quoted := make([]string, len(commands))
	for i, c := range commands {
		quoted[i] = "'" + strings.ReplaceAll(c, "'", `'\''`) + "'"
	}
	return fmt.Sprintf(`for c in %s; do
  if command -v "$c" >/dev/null 2>&1; then
    out=$(timeout 10 "$c" --version 2>/dev/null | head -n1) || true
    if [ -n "$out" ]; then printf '%%s\n' "$out"; exit 0; fi
  fi
done
cat /etc/nightshift-agent 2>/dev/null || true`, strings.Join(quoted, " "))
}

// AgentVersions reads every pod's coding-agent version in parallel, asking
// each of commands in turn, with one kubectl exec per pod. A pod whose exec
// fails is left out so a caller can tell an unprobed pod from a silent one;
// one with no known CLI and no version file reads "".
func AgentVersions(pods, commands []string) map[string]string {
	probe := agentProbe(commands)
	return perPod(pods, func(pod string) (string, error) {
		out, err := runKubectl(nil, "exec", pod, "--", "sh", "-c", probe)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	})
}
