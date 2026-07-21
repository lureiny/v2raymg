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
	// family pins the socket's address family via the network suffix:
	// "" -> "tcp"/"udp" (family inferred from a specific IP literal),
	// "4" -> "tcp4"/"udp4" (real AF_INET), "6" -> "tcp6"/"udp6" (AF_INET6, which
	// Go binds with IPV6_V6ONLY=1). Pinning matters for wildcards: a bare
	// net.Listen("tcp","0.0.0.0:p") is promoted by Go to a dual-stack AF_INET6
	// socket, so "ipv4" would wrongly also accept IPv6 and would collide with a
	// sibling "[::]" bind. "4"+"6" give two clean, non-overlapping sockets with
	// real (non v4-mapped) client addresses on each.
	family string
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
//     bound to that address; ruleStack/defaultStack are ignored. A wildcard
//     literal ("0.0.0.0" / "::") is family-pinned so it stays single-stack.
//   - An empty listenAddr yields a wildcard listener governed by the effective
//     stack (rule override, else manager default, else dual):
//     "ipv4" -> [0.0.0.0 tcp4], "ipv6" -> [[::] tcp6],
//     "dual" -> [0.0.0.0 tcp4, [::] tcp6] with BOTH best-effort (a host missing
//     either family skips that half; the rule fails only if neither binds).
func resolveListenEndpoints(listenAddr, ruleStack, defaultStack string, port uint32) ([]listenEndpoint, error) {
	p := strconv.Itoa(int(port))

	if addr := strings.TrimSpace(listenAddr); addr != "" {
		ip := net.ParseIP(addr)
		if ip == nil {
			return nil, fmt.Errorf("listen_addr %q is not a valid IP literal", addr)
		}
		// A specific (non-wildcard) address is bound as-is with an inferred
		// family. An explicit wildcard literal is pinned so it does not get
		// promoted to a dual-stack socket by net.Listen.
		family := ""
		if ip.IsUnspecified() {
			if ip.To4() != nil {
				family = "4"
			} else {
				family = "6"
			}
		}
		return []listenEndpoint{{address: net.JoinHostPort(addr, p), family: family}}, nil
	}

	switch firstListenStack(ruleStack, defaultStack) {
	case ListenStackIPv4:
		return []listenEndpoint{{address: net.JoinHostPort("0.0.0.0", p), family: "4"}}, nil
	case ListenStackIPv6:
		return []listenEndpoint{{address: net.JoinHostPort("::", p), family: "6"}}, nil
	default: // dual
		// Two clean sockets (real AF_INET + AF_INET6/V6ONLY). Both are
		// best-effort: a host with only IPv4 skips the [::] bind, a host with
		// only IPv6 skips the 0.0.0.0 bind. multiRelay.Start still fails the
		// rule if NEITHER family binds.
		return []listenEndpoint{
			{address: net.JoinHostPort("0.0.0.0", p), family: "4", optional: true},
			{address: net.JoinHostPort("::", p), family: "6", optional: true},
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

// listenTCP binds a TCP listener on address using the "tcp"+family network
// ("tcp", "tcp4", or "tcp6"). See listenEndpoint.family for why the family is
// pinned for wildcard binds.
//
// A pinned family ("4"/"6") binds exactly that family with no fallback: dual
// stack wildcards rely on this so a failed best-effort half is skipped by
// multiRelay instead of silently double-binding the sibling family, and an
// explicit single-stack choice must fail rather than switch stacks. An
// unpinned bind delegates to listenDualStack, which keeps the kernel
// dual-stack + IPv4-fallback semantics for wildcard hosts (and is a plain
// bind for specific IPs).
func listenTCP(address, family string) (net.Listener, error) {
	if family == "" {
		return listenDualStack(address)
	}
	return net.Listen("tcp"+family, address)
}

// listenUDP binds a UDP packet connection on address using the "udp"+family
// network ("udp", "udp4", or "udp6"). Family semantics match listenTCP:
// pinned = exact, unpinned = best-effort dual-stack via listenPacketDualStack.
func listenUDP(address, family string) (net.PacketConn, error) {
	if family == "" {
		return listenPacketDualStack(address)
	}
	return net.ListenPacket("udp"+family, address)
}
