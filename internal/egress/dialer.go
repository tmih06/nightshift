package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"
)

var (
	cgnatPrefix     = netip.MustParsePrefix("100.64.0.0/10")
	benchmarkPrefix = netip.MustParsePrefix("198.18.0.0/15")
	metadataPrefix  = netip.MustParsePrefix("169.254.0.0/16")
)

// IsRestrictedIP reports whether ip belongs to a loopback, private, link-local,
// cloud metadata, CGNAT, multicast, or unspecified range that sandboxes must
// not reach via the egress proxy.
func IsRestrictedIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap()
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	if cgnatPrefix.Contains(ip) || benchmarkPrefix.Contains(ip) || metadataPrefix.Contains(ip) {
		return true
	}
	return false
}

// SafeDialer resolves DNS inside the proxy and rejects restricted IP addresses
// before connecting directly to the validated IP (preventing DNS rebinding and
// SSRF against cluster internals or cloud metadata endpoints).
type SafeDialer struct {
	Dialer          net.Dialer
	Resolver        *net.Resolver
	BlockPrivateIPs bool
}

// NewSafeDialer returns a SafeDialer with sensible proxy timeouts.
func NewSafeDialer(blockPrivate bool) *SafeDialer {
	return &SafeDialer{
		Dialer: net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		Resolver:        net.DefaultResolver,
		BlockPrivateIPs: blockPrivate,
	}
}

// DialContext validates the destination IP(s) and dials the first safe address.
func (d *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid target address %q: %w", addr, err)
	}
	if _, err := strconv.Atoi(portStr); err != nil {
		return nil, fmt.Errorf("invalid port %q: %w", portStr, err)
	}

	var ips []netip.Addr
	if parsed, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{parsed}
	} else {
		resolver := d.Resolver
		if resolver == nil {
			resolver = net.DefaultResolver
		}
		resolved, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", host, err)
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("resolve %s: no addresses", host)
		}
		ips = resolved
	}

	if d.BlockPrivateIPs {
		for _, ip := range ips {
			if IsRestrictedIP(ip) {
				return nil, fmt.Errorf("egress blocked: %s resolved to restricted IP %s", host, ip.Unmap())
			}
		}
	}

	var lastErr error
	for _, ip := range ips {
		target := net.JoinHostPort(ip.Unmap().String(), portStr)
		conn, err := d.Dialer.DialContext(ctx, network, target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
