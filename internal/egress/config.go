// Package egress implements a lightweight HTTP/HTTPS egress proxy with
// default-deny host/path/method allowlists, SSRF-safe dialing, selective TLS
// interception, host-scoped secret placeholder injection, and inbound response
// scrubbing.
package egress

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Mode controls how the proxy handles HTTPS CONNECT tunnels for a rule.
type Mode string

const (
	// ModeTunnel forwards HTTPS via raw TCP CONNECT without decrypting TLS.
	ModeTunnel Mode = "tunnel"
	// ModeMITM terminates TLS with the proxy CA to enforce L7 method/path
	// rules, inject authorized secrets, and scrub reflected secrets.
	ModeMITM Mode = "mitm"
)

// Config is the declarative egress policy loaded by the proxy.
type Config struct {
	CA              CAConfig                 `yaml:"ca"`
	BlockPrivateIPs *bool                    `yaml:"block_private_ips"`
	ScrubResponses  *bool                    `yaml:"scrub_responses"`
	Rules           []Rule                   `yaml:"rules"`
	Secrets         map[string]SecretBinding `yaml:"secrets"`
}

// CAConfig locates the proxy's MITM CA certificate and private key on disk.
// When omitted, the proxy generates an ephemeral in-memory CA at startup.
type CAConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Rule allows outbound requests matching Hosts and Ports (and, in MITM or
// plain HTTP mode, optional Methods and PathPrefixes) and binds the named
// Secrets for placeholder substitution on matching requests only.
type Rule struct {
	Name         string   `yaml:"name"`
	Hosts        []string `yaml:"hosts"`
	Ports        []int    `yaml:"ports"`
	Mode         Mode     `yaml:"mode"`
	Methods      []string `yaml:"methods"`
	PathPrefixes []string `yaml:"path_prefixes"`
	Secrets      []string `yaml:"secrets"`
}

// SecretBinding maps an opaque placeholder string (exposed inside sandboxes)
// to a real credential sourced outside the sandbox from Env, File, or Value.
type SecretBinding struct {
	Placeholder string `yaml:"placeholder"`
	Env         string `yaml:"env"`
	File        string `yaml:"file"`
	Value       string `yaml:"value"` // primarily for unit tests
}

// DefaultPlaceholderPrefix is the standard prefix for sandbox secret sentinels.
const DefaultPlaceholderPrefix = "nightshift:secret:"

// ShouldBlockPrivateIPs returns true unless explicitly disabled in Config.
func (c Config) ShouldBlockPrivateIPs() bool {
	if c.BlockPrivateIPs == nil {
		return true
	}
	return *c.BlockPrivateIPs
}

// ShouldScrubResponses returns true unless explicitly disabled in Config.
func (c Config) ShouldScrubResponses() bool {
	if c.ScrubResponses == nil {
		return true
	}
	return *c.ScrubResponses
}

// LoadConfig reads, parses, and validates a YAML egress policy file.
func LoadConfig(filePath string) (Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, fmt.Errorf("read egress config %s: %w", filePath, err)
	}
	return ParseConfig(data)
}

// ParseConfig unmarshals and validates YAML egress policy bytes.
func ParseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse egress config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate normalizes defaults and rejects invalid rules or secret bindings.
func (c *Config) Validate() error {
	for name, b := range c.Secrets {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("secret binding has empty name")
		}
		if b.Placeholder == "" {
			b.Placeholder = DefaultPlaceholderPrefix + name
			c.Secrets[name] = b
		}
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Name == "" {
			r.Name = fmt.Sprintf("rule-%d", i+1)
		}
		if len(r.Hosts) == 0 {
			return fmt.Errorf("rule %q has no hosts", r.Name)
		}
		for j, h := range r.Hosts {
			h = strings.ToLower(strings.TrimSpace(h))
			if h == "" {
				return fmt.Errorf("rule %q has empty host", r.Name)
			}
			r.Hosts[j] = h
		}
		if len(r.Ports) == 0 {
			r.Ports = []int{443}
		}
		for _, p := range r.Ports {
			if p <= 0 || p > 65535 {
				return fmt.Errorf("rule %q has invalid port %d", r.Name, p)
			}
		}
		if r.Mode == "" {
			if len(r.Secrets) > 0 || len(r.Methods) > 0 || len(r.PathPrefixes) > 0 {
				r.Mode = ModeMITM
			} else {
				r.Mode = ModeTunnel
			}
		}
		if r.Mode != ModeTunnel && r.Mode != ModeMITM {
			return fmt.Errorf("rule %q has unknown mode %q", r.Name, r.Mode)
		}
		if r.Mode == ModeTunnel && (len(r.Secrets) > 0 || len(r.Methods) > 0 || len(r.PathPrefixes) > 0) {
			return fmt.Errorf("rule %q uses mode tunnel with L7 secrets/methods/path_prefixes (use mode: mitm)", r.Name)
		}
		for j, m := range r.Methods {
			r.Methods[j] = strings.ToUpper(strings.TrimSpace(m))
		}
		for _, s := range r.Secrets {
			if _, ok := c.Secrets[s]; !ok {
				return fmt.Errorf("rule %q references unknown secret %q", r.Name, s)
			}
		}
	}
	return nil
}

// MatchHostPort returns the first rule permitting host and port.
func (c *Config) MatchHostPort(host string, port int) (*Rule, bool) {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.matchesHost(host) && r.matchesPort(port) {
			return r, true
		}
	}
	return nil, false
}

func (r *Rule) matchesHost(host string) bool {
	for _, pat := range r.Hosts {
		if pat == host {
			return true
		}
		if strings.HasPrefix(pat, "*.") {
			suffix := pat[1:] // ".example.com"
			if len(host) > len(suffix) && strings.HasSuffix(host, suffix) {
				return true
			}
		}
	}
	return false
}

func (r *Rule) matchesPort(port int) bool {
	for _, p := range r.Ports {
		if p == port {
			return true
		}
	}
	return false
}

// AllowsRequest checks L7 HTTP method and path prefix rules (after canonical
// path cleaning so /../ traversal cannot bypass path_prefixes).
func (r *Rule) AllowsRequest(method, reqPath string) bool {
	if len(r.Methods) > 0 {
		m := strings.ToUpper(method)
		ok := false
		for _, allowed := range r.Methods {
			if allowed == m {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.PathPrefixes) > 0 {
		clean := cleanHTTPPath(reqPath)
		ok := false
		for _, prefix := range r.PathPrefixes {
			p := cleanHTTPPath(prefix)
			if clean == p || strings.HasPrefix(clean, p+"/") || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(clean, prefix)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func cleanHTTPPath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}
