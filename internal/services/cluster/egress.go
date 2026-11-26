package cluster

import (
	"strings"
)

const (
	// EgressNamespace is the separate namespace hosting Nightshift's egress
	// proxy and real credential Secrets, isolated from nightshift.
	EgressNamespace = "nightshift-egress"

	// EgressServiceURL is the in-cluster HTTP/HTTPS proxy URL for sandboxes.
	EgressServiceURL = "http://egress-proxy.nightshift-egress.svc.cluster.local:8080"

	// EgressCAConfigMap is the public CA ConfigMap mirrored into nightshift
	// when the egress proxy is installed (contains only ca.crt, never ca.key).
	EgressCAConfigMap = "egress-ca"

	egressCAMountDir = "/etc/nightshift/ca"
	egressCACertPath = egressCAMountDir + "/ca.crt"
)

// EgressSandboxEnv returns the proxy, CA trust, and opaque secret placeholder
// environment variables injected into sandboxes when the egress proxy is
// enabled. Real credentials never enter the sandbox.
func EgressSandboxEnv(proxyURL string) []map[string]any {
	if proxyURL == "" {
		proxyURL = EgressServiceURL
	}
	pairs := [][2]string{
		{"HTTP_PROXY", proxyURL},
		{"HTTPS_PROXY", proxyURL},
		{"http_proxy", proxyURL},
		{"https_proxy", proxyURL},
		{"NO_PROXY", "localhost,127.0.0.1"},
		{"no_proxy", "localhost,127.0.0.1"},
		{"NODE_EXTRA_CA_CERTS", egressCACertPath},
		{"SSL_CERT_FILE", egressCACertPath},
		{"REQUESTS_CA_BUNDLE", egressCACertPath},
		{"CURL_CA_BUNDLE", egressCACertPath},
		{"GIT_SSL_CAINFO", egressCACertPath},
		// No ANTHROPIC_API_KEY: Claude Code gives it precedence over every
		// OAuth path, so setting it would disable credential-file and
		// oauth-token auth. The oauth placeholder carries Claude instead.
		{"CLAUDE_CODE_OAUTH_TOKEN", "nightshift:secret:CLAUDE_ACCESS_TOKEN"},
		{"OPENAI_API_KEY", "nightshift:secret:OPENAI_API_KEY"},
		{"GEMINI_API_KEY", "nightshift:secret:GEMINI_API_KEY"},
		{"GITHUB_TOKEN", "nightshift:secret:GITHUB_TOKEN"},
	}
	out := make([]map[string]any, len(pairs))
	for i, kv := range pairs {
		out[i] = map[string]any{"name": kv[0], "value": kv[1]}
	}
	return out
}

// EgressProxyURL returns the in-cluster proxy URL when the egress proxy's
// public CA ConfigMap is present in nightshift, or "" when egress control is
// not installed. Preferring the Service ClusterIP lets NetworkPolicies drop
// port 53 DNS completely from sandbox pods (blocking DNS exfiltration).
func EgressProxyURL() string {
	out, err := runKubectl(nil, "get", "configmap", EgressCAConfigMap, "--ignore-not-found", "-o", "name")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return ""
	}
	ipOut, err := runKubectl(nil, "-n", EgressNamespace, "get", "service", "egress-proxy",
		"--ignore-not-found", "-o", "jsonpath={.spec.clusterIP}")
	if err == nil {
		if ip := strings.TrimSpace(string(ipOut)); ip != "" && ip != "None" {
			return "http://" + ip + ":8080"
		}
	}
	return EgressServiceURL
}

// applyEgressToPodSpec wires the read-only CA mount, proxy/CA/placeholder env
// vars, and automountServiceAccountToken=false onto a pod spec map in place.
func applyEgressToPodSpec(spec map[string]any, proxyURL string, includeLLMPlaceholders bool) {
	if proxyURL == "" {
		return
	}
	spec["automountServiceAccountToken"] = false

	// A template that egress-setup.sh already wired carries the CA volume and
	// mount; adding them again makes Kubernetes refuse the pod as a duplicate.
	volumes, _ := spec["volumes"].([]any)
	if !hasNamed(volumes, "egress-ca") {
		volumes = append(volumes, map[string]any{
			"name": "egress-ca",
			"configMap": map[string]any{
				"name":     EgressCAConfigMap,
				"optional": true,
			},
		})
	}
	spec["volumes"] = volumes

	envEntries := EgressSandboxEnv(proxyURL)
	if !includeLLMPlaceholders {
		// Clone Jobs only need proxy + CA + GITHUB_TOKEN placeholder.
		filtered := make([]map[string]any, 0, len(envEntries))
		for _, e := range envEntries {
			name, _ := e["name"].(string)
			if strings.HasSuffix(name, "_API_KEY") || strings.HasSuffix(name, "_OAUTH_TOKEN") {
				continue
			}
			filtered = append(filtered, e)
		}
		envEntries = filtered
	}

	list, _ := spec["containers"].([]any)
	for _, item := range list {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mounts, _ := c["volumeMounts"].([]any)
		if !hasNamed(mounts, "egress-ca") {
			mounts = append(mounts, map[string]any{
				"name":       "egress-ca",
				keyMountPath: egressCAMountDir,
				"readOnly":   true,
			})
		}
		c["volumeMounts"] = mounts

		existing, _ := c["env"].([]any)
		seen := map[string]bool{}
		for _, raw := range existing {
			if em, ok := raw.(map[string]any); ok {
				if n, _ := em["name"].(string); n != "" {
					seen[n] = true
				}
			}
		}
		for _, e := range envEntries {
			if !seen[e["name"].(string)] {
				existing = append(existing, e)
			}
		}
		c["env"] = existing
	}
}

// keyMountPath is the field of a volume mount that says where it sits.
const keyMountPath = "mountPath"

// hasNamed is whether a list of volumes or mounts holds one called name.
func hasNamed(list []any, name string) bool {
	for _, item := range list {
		if m, ok := item.(map[string]any); ok && m["name"] == name {
			return true
		}
	}
	return false
}
