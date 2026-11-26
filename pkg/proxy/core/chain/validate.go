package chain

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode"
)

var ipv4ThisNetworkPrefix = netip.MustParsePrefix("0.0.0.0/8")

// AddressPolicy implements Confirmed Decision #1 (Plan A) for backend chain targets:
//   - Unconditionally rejects empty host, out-of-range ports, unspecified, multicast,
//     broadcast, link-local / cloud metadata (169.254.0.0/16, fe80::/10), and any
//     target matching loopback or LocalHosts whose port is reserved or allocated.
//   - Allows public IPs, DDNS domains (without resolving/pinning DNS), Tailscale CGNAT
//     (100.64.0.0/10), RFC1918 private ranges, and IPv6 ULA (fc00::/7).
//   - Rejects loopback (127.0.0.0/8, ::1, localhost) unless AllowLoopback is true.
type AddressPolicy struct {
	AllowLoopback   bool
	LocalHosts      []string
	ReservedPorts   map[uint32]struct{}
	IsPortAllocated func(uint32) bool
}

// ParseAndValidate parses rawURI and validates the resulting OutboundSpec
// against policy.
func ParseAndValidate(rawURI string, policy AddressPolicy) (*OutboundSpec, error) {
	spec, err := ParseURI(rawURI)
	if err != nil {
		return nil, err
	}
	if err := ValidateSpec(spec, policy); err != nil {
		return nil, err
	}
	return spec, nil
}

// ValidateSpec validates spec against policy without performing DNS resolution.
func ValidateSpec(spec *OutboundSpec, policy AddressPolicy) error {
	if spec == nil {
		return fmt.Errorf("chain: nil outbound spec")
	}

	host := cleanHostLiteral(spec.Host)
	if host == "" {
		return fmt.Errorf("chain: empty target host")
	}
	if spec.Port == 0 || spec.Port > 65535 {
		return fmt.Errorf("chain: target port %d out of range [1, 65535]", spec.Port)
	}

	// Check if host is an IP literal (handle IPv6 zones cleanly).
	if ip, err := netip.ParseAddr(host); err == nil {
		return validateIPTarget(ip, spec.Port, policy)
	}

	// Domain / hostname validation (never resolve DNS so DDNS stays fresh).
	if err := validateDomainSyntax(host); err != nil {
		return err
	}

	// Reject trailing-dot IP literals (e.g. "127.0.0.1.", "169.254.169.254.")
	// and non-standard inet_aton numeric IPv4 forms (e.g. "127.1", "2130706433",
	// "0177.0.0.1", "0x7f000001") that would otherwise bypass IP policy checks.
	trimmedHost := strings.TrimRight(host, ".")
	if trimmedHost == "" {
		return fmt.Errorf("chain: empty target host")
	}
	if trimmedHost != host {
		if _, err := netip.ParseAddr(trimmedHost); err == nil {
			return fmt.Errorf("chain: trailing-dot IP literal %q is not allowed", spec.Host)
		}
	}
	if isNumericIPv4Literal(trimmedHost) {
		return fmt.Errorf("chain: non-standard numeric IPv4 host %q is not allowed", spec.Host)
	}

	isLoopbackHost := isLocalhostDomain(host)
	if isLoopbackHost && !policy.AllowLoopback {
		return fmt.Errorf("chain: loopback target %q rejected (allow_loopback_chain_target is disabled)", spec.Host)
	}

	if (isLoopbackHost || policy.matchesLocalDomain(host)) && policy.isPortProtected(spec.Port) {
		return fmt.Errorf("chain: local target %s:%d hits reserved or allocated port", spec.Host, spec.Port)
	}

	return nil
}

func validateIPTarget(rawIP netip.Addr, port uint32, policy AddressPolicy) error {
	ip := rawIP.WithZone("").Unmap()
	if !ip.IsValid() {
		return fmt.Errorf("chain: invalid IP address %q", rawIP.String())
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("chain: unspecified address %s is not allowed", ip)
	}
	if ip.Is4() && ipv4ThisNetworkPrefix.Contains(ip) {
		return fmt.Errorf("chain: this-network address %s (0.0.0.0/8) is not allowed", ip)
	}
	if ip.IsMulticast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("chain: multicast address %s is not allowed", ip)
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("chain: link-local / cloud metadata address %s is not allowed", ip)
	}
	if ip == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return fmt.Errorf("chain: broadcast address %s is not allowed", ip)
	}
	if !ip.IsGlobalUnicast() && !ip.IsLoopback() {
		return fmt.Errorf("chain: non-unicast address %s is not allowed", ip)
	}

	isLoopback := ip.IsLoopback()
	if isLoopback && !policy.AllowLoopback {
		return fmt.Errorf("chain: loopback target %s rejected (allow_loopback_chain_target is disabled)", ip)
	}

	if (isLoopback || policy.matchesLocalIP(ip)) && policy.isPortProtected(port) {
		return fmt.Errorf("chain: local target %s:%d hits reserved or allocated port", ip, port)
	}

	return nil
}

func (p AddressPolicy) isPortProtected(port uint32) bool {
	if _, reserved := p.ReservedPorts[port]; reserved {
		return true
	}
	if p.IsPortAllocated != nil && p.IsPortAllocated(port) {
		return true
	}
	return false
}

func (p AddressPolicy) matchesLocalIP(target netip.Addr) bool {
	for _, entry := range p.LocalHosts {
		clean := cleanHostLiteral(entry)
		if clean == "" {
			continue
		}
		if addr, err := netip.ParseAddr(clean); err == nil {
			if addr.WithZone("").Unmap() == target {
				return true
			}
		}
	}
	return false
}

func (p AddressPolicy) matchesLocalDomain(target string) bool {
	normTarget := normalizeDomain(target)
	for _, entry := range p.LocalHosts {
		clean := cleanHostLiteral(entry)
		if clean == "" {
			continue
		}
		if _, err := netip.ParseAddr(clean); err == nil {
			continue
		}
		if normalizeDomain(clean) == normTarget {
			return true
		}
	}
	return false
}

func cleanHostLiteral(host string) string {
	h := strings.TrimSpace(host)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return strings.TrimSpace(h)
}

func normalizeDomain(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func isLocalhostDomain(host string) bool {
	d := normalizeDomain(host)
	return d == "localhost" || strings.HasSuffix(d, ".localhost")
}

func validateDomainSyntax(host string) error {
	for _, r := range host {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("chain: target host %q contains invalid whitespace or control character", host)
		}
		switch r {
		case '/', '\\', '?', '#', '@':
			return fmt.Errorf("chain: target host %q contains invalid delimiter %q", host, r)
		}
	}
	return nil
}

// isNumericIPv4Literal reports whether host looks like an inet_aton / WHATWG
// numeric IPv4 literal (1-4 dot-separated decimal, octal, or 0x-prefixed hex
// integers, e.g. "127.1", "2130706433", "0177.0.0.1", "0x7f000001").
func isNumericIPv4Literal(host string) bool {
	if host == "" {
		return false
	}
	parts := strings.Split(host, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if !isInetAtonPart(p) {
			return false
		}
	}
	return true
}

func isInetAtonPart(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > 2 && (strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X")) {
		for i := 2; i < len(s); i++ {
			c := s[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
