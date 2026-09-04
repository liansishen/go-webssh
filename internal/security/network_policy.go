package security

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type NetworkPolicy struct {
	AllowPrivateRanges bool
	DenyPrefixes       []netip.Prefix
	AllowPrefixes      []netip.Prefix
	Resolver           *net.Resolver
	LookupTimeout      time.Duration
}

func NewNetworkPolicy(allowPrivate bool, deny, allow []string) (*NetworkPolicy, error) {
	p := &NetworkPolicy{
		AllowPrivateRanges: allowPrivate,
		Resolver:           net.DefaultResolver,
		LookupTimeout:      5 * time.Second,
	}

	// Always include hard defaults if not present.
	defaults := []string{
		"127.0.0.0/8",
		"::1/128",
		"0.0.0.0/8",
		"169.254.0.0/16",
	}
	seen := map[string]bool{}
	for _, d := range defaults {
		seen[d] = true
		pref, err := netip.ParsePrefix(d)
		if err != nil {
			return nil, err
		}
		p.DenyPrefixes = append(p.DenyPrefixes, pref)
	}
	for _, d := range deny {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		pref, err := parsePrefixOrIP(d)
		if err != nil {
			return nil, fmt.Errorf("invalid deny entry %q: %w", d, err)
		}
		p.DenyPrefixes = append(p.DenyPrefixes, pref)
		seen[d] = true
	}
	for _, a := range allow {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		pref, err := parsePrefixOrIP(a)
		if err != nil {
			return nil, fmt.Errorf("invalid allow entry %q: %w", a, err)
		}
		p.AllowPrefixes = append(p.AllowPrefixes, pref)
	}
	if !allowPrivate {
		for _, d := range []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"fc00::/7",
			"fe80::/10",
		} {
			pref, _ := netip.ParsePrefix(d)
			p.DenyPrefixes = append(p.DenyPrefixes, pref)
		}
	}
	return p, nil
}

func parsePrefixOrIP(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits), nil
}

func (p *NetworkPolicy) ValidateHostPort(host string, port int) error {
	_, err := p.ResolveAllowedHostPort(host, port)
	return err
}

// ResolveAllowedHostPort resolves and validates a destination once so callers
// can dial the exact address that passed the network policy.
func (p *NetworkPolicy) ResolveAllowedHostPort(host string, port int) (string, error) {
	if port < 1 || port > 65535 {
		return "", &PolicyError{Code: "INVALID_PORT", Message: "port must be between 1 and 65535"}
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", &PolicyError{Code: "INVALID_HOST", Message: "host is required"}
	}
	if strings.Contains(host, "://") {
		return "", &PolicyError{Code: "INVALID_HOST", Message: "host must not include URL scheme"}
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return "", &PolicyError{Code: "NETWORK_DENIED", Message: "target host is blocked by network policy"}
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		ip = ip.Unmap()
		if err := p.checkAddr(ip); err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.LookupTimeout)
	defer cancel()
	ips, err := p.Resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", &PolicyError{Code: "INVALID_HOST", Message: "failed to resolve host"}
	}
	if len(ips) == 0 {
		return "", &PolicyError{Code: "INVALID_HOST", Message: "host resolved to no addresses"}
	}
	var resolved netip.Addr
	for _, ipa := range ips {
		addr, ok := netip.AddrFromSlice(ipa.IP)
		if !ok {
			return "", &PolicyError{Code: "NETWORK_DENIED", Message: "target host is blocked by network policy"}
		}
		addr = addr.Unmap()
		if err := p.checkAddr(addr); err != nil {
			return "", err
		}
		if !resolved.IsValid() {
			resolved = addr
		}
	}
	return net.JoinHostPort(resolved.String(), strconv.Itoa(port)), nil
}

func (p *NetworkPolicy) checkAddr(addr netip.Addr) error {
	// Explicit allow list override (for exceptions).
	for _, pref := range p.AllowPrefixes {
		if pref.Contains(addr) {
			return nil
		}
	}
	for _, pref := range p.DenyPrefixes {
		if pref.Contains(addr) {
			return &PolicyError{Code: "NETWORK_DENIED", Message: "target host is blocked by network policy"}
		}
	}
	// Block unspecified / invalid
	if !addr.IsValid() || addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return &PolicyError{Code: "NETWORK_DENIED", Message: "target host is blocked by network policy"}
	}
	if !p.AllowPrivateRanges && addr.IsPrivate() {
		return &PolicyError{Code: "NETWORK_DENIED", Message: "target host is blocked by network policy"}
	}
	return nil
}

type PolicyError struct {
	Code    string
	Message string
}

func (e *PolicyError) Error() string {
	return e.Message
}
