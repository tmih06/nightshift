package cluster

import (
	"strings"
	"sync"
)

// agentProbe prints the first agent CLI's --version answer, else the version
// file images/agent stamps as "<agent> <semver>" at build time. The CLI is
// asked first so a self-updated binary reports what it runs, not what the
// image was built with.
const agentProbe = `for c in pi codex claude opencode agy ompig-slim; do
  if command -v "$c" >/dev/null 2>&1; then
    out=$(timeout 10 "$c" --version 2>/dev/null | head -n1) || true
    if [ -n "$out" ]; then printf '%s\n' "$out"; exit 0; fi
  fi
done
cat /etc/nightshift-agent 2>/dev/null || true`

// agentVersion reads pod's coding-agent version with one kubectl exec. It is
// empty when no known CLI answers and no version file exists.
func agentVersion(pod string) (string, error) {
	out, err := runKubectl(nil, "exec", pod, "--", "sh", "-c", agentProbe)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// AgentVersions reads every pod's version in parallel, leaving out the ones
// whose exec fails so a caller can tell an unprobed pod from a silent one.
func AgentVersions(pods []string) map[string]string {
	out := make(map[string]string, len(pods))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, pod := range pods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if v, err := agentVersion(pod); err == nil {
				mu.Lock()
				out[pod] = v
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}
