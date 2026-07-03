package forward

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// listenEndpoint describes a single socket to bind for a forward rule.
//
// A rule resolves to one endpoint (a specific IP, or a single-stack wildcard)
// or two endpoints (dual-stack: the IPv4 wildcard plus the IPv6 wildcard).
type listenEndpoint struct {
	// address is a "host:port" string, already bracketed for IPv6 literals
	// (produced by net.JoinHostPort), suitable for net.Listen / net.ListenPacket.
	address string
	// v6only requests IPV6_V6ONLY on the socket. Set for the "[::]" wildcard so
	// it can coexist with a sibling "0.0.0.0" socket on the same port without an
	// address-in-use conflict, and so IPv4 clients keep their real (non
	// v4-mapped) source addresses.
	v6only bool
	// optional marks a best-effort endpoint: if binding fails (e.g. the host
	// lacks one IP family), the failure is logged and the endpoint skipped
	// instead of failing the whole rule. BOTH halves of a dual-stack listener
	// are optional (so an IPv4-only or IPv6-only host still comes up on the
	// family it has); explicit single-stack / specific-IP listeners are always
	// required. multiRelay.Start fails only if every endpoint is optional AND
	// none of them bind.
	optional bool
}

// normalizeListenStack canonicalizes a stack selector, returning "" for empty
// or unrecognized input so callers can fall through to the next default.
func normalizeListenStack(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ListenStackIPv4, "v4":
		return ListenStackIPv4
	case ListenStackIPv6, "v6":
		return ListenStackIPv6
	case ListenStackDual, "both":
		return ListenStackDual
	default:
		return ""
	}
}

// firstListenStack returns the first recognized stack among the candidates,
// defaulting to dual-stack when none is set.
func firstListenStack(candidates ...string) string {
	for _, c := range candidates {
		if s := normalizeListenStack(c); s != "" {
			return s
		}
	}
	return ListenStackDual
}

// resolveListenEndpoints computes the set of sockets to bind for a rule.
//
//   - A non-empty listenAddr (a specific IP literal) yields exactly one socket
//     bound to that address; ruleStack/defaultStack are ignored.
//   - An empty listenAddr yields a wildcard listener governed by the effective
//     stack (rule override, else manager default, else dual):
//     "ipv4" -> [0.0.0.0], "ipv6" -> [[::] v6only],
//     "dual" -> [0.0.0.0, [::] v6only] with BOTH best-effort (a host missing
//     either family skips that half; the rule fails only if neither binds).
func resolveListenEndpoints(listenAddr, ruleStack, defaultStack string, port uint32) ([]listenEndpoint, error) {
	p := strconv.Itoa(int(port))

	if addr := strings.TrimSpace(listenAddr); addr != "" {
		if net.ParseIP(addr) == nil {
			return nil, fmt.Errorf("listen_addr %q is not a valid IP literal", addr)
		}
		return []listenEndpoint{{address: net.JoinHostPort(addr, p)}}, nil
	}

	switch firstListenStack(ruleStack, defaultStack) {
	case ListenStackIPv4:
		return []listenEndpoint{{address: net.JoinHostPort("0.0.0.0", p)}}, nil
	case ListenStackIPv6:
		return []listenEndpoint{{address: net.JoinHostPort("::", p), v6only: true}}, nil
	default: // dual
		// Both halves are best-effort: a host with only IPv4 skips the [::]
		// bind, a host with only IPv6 skips the 0.0.0.0 bind. multiRelay.Start
		// still fails the rule if NEITHER family binds.
		return []listenEndpoint{
			{address: net.JoinHostPort("0.0.0.0", p), optional: true},
			{address: net.JoinHostPort("::", p), v6only: true, optional: true},
		}, nil
	}
}

// describeEndpoints renders the endpoint addresses as a comma-joined string for
// log messages (e.g. "0.0.0.0:12345,[::]:12345").
func describeEndpoints(eps []listenEndpoint) string {
	parts := make([]string, 0, len(eps))
	for _, e := range eps {
		parts = append(parts, e.address)
	}
	return strings.Join(parts, ",")
}

// listenTCP binds a TCP listener on address. When v6only is true it uses the
// "tcp6" network so Go sets IPV6_V6ONLY on the socket; this lets an IPv6
// wildcard ("[::]") coexist with a sibling IPv4 wildcard ("0.0.0.0") on the
// same port and keeps IPv4 clients' real (non v4-mapped) source addresses.
func listenTCP(address string, v6only bool) (net.Listener, error) {
	network := "tcp"
	if v6only {
		network = "tcp6"
	}
	return net.Listen(network, address)
}

// listenUDP binds a UDP packet connection on address, using "udp6" (IPV6_V6ONLY)
// when v6only is requested. See listenTCP for the rationale.
func listenUDP(address string, v6only bool) (net.PacketConn, error) {
	network := "udp"
	if v6only {
		network = "udp6"
	}
	return net.ListenPacket(network, address)
}
