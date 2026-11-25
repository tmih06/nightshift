package cluster

import (
	"fmt"
	"strings"
)

// ClaimInfo reads a claim: the warm pool it draws from and the sandbox it has
// adopted, which is empty until it has one.
func ClaimInfo(name string) (pool, sandbox string, err error) {
	out, err := runKubectl(nil, "get", "sandboxclaim", name,
		"-o", "jsonpath={.spec.warmPoolRef.name}/{.status.sandbox.name}")
	if err != nil {
		return "", "", err
	}
	pool, sandbox, ok := strings.Cut(strings.TrimSpace(string(out)), "/")
	if !ok {
		return "", "", fmt.Errorf("claim %s: unexpected answer %q", name, out)
	}
	return pool, sandbox, nil
}
