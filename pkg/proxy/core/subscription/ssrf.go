package subscription

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// SSRF protection for outbound fetches driven by user-controlled URLs
// (ext_sub, proxy_groups_url, rule_providers_url, rules_url) and the Clash
// template sources. The panel is the only party making these requests; a
// user must not be able to point them at internal services (localhost, RFC1918,
// cloud metadata, etc.).
//
// The check runs at connection time via net.Dialer.Control, on the actual
// resolved ip:port about to be dialed. That defeats DNS rebinding (check and
// connect use the same IP) and covers every redirect hop (each hop re-dials
// and re-triggers Control), which a parse-time URL/host check cannot.

// ssrfGuardEnabled gates the connection guard. It is always true in production;
// tests that must dial loopback flip it via SetSSRFGuardEnabledForTest.
var ssrfGuardEnabled atomic.Bool

func init() { ssrfGuardEnabled.Store(true) }

// SetSSRFGuardEnabledForTest toggles the loopback-dial guard and returns a
// restore function. For tests only.
func SetSSRFGuardEnabledForTest(v bool) (restore func()) {
	prev := ssrfGuardEnabled.Load()
	ssrfGuardEnabled.Store(v)
	return func() { ssrfGuardEnabled.Store(prev) }
}

// blockedPrefixes are ranges that are "global unicast" by the stdlib's
// classification but must not be reachable from the panel — CGNAT, and IPv6
// transition ranges that can embed an internal IPv4 (NAT64, 6to4,
// IPv4-compatible), plus IPv4 special/reserved blocks.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT (RFC 6598)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved / future use
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 (RFC 6052) — can embed internal v4
	netip.MustParsePrefix("2002::/16"),       // 6to4 — can embed internal v4
	netip.MustParsePrefix("::/96"),           // IPv4-compatible IPv6 (deprecated)
}

// classifyBlockedAddr returns a non-nil error if the given "host:port" dial
// address resolves to a non-public IP. It is a pure function (not gated by the
// test flag) so it can be unit-tested directly. Only globally-routable unicast
// addresses that are not in any special range are allowed.
func classifyBlockedAddr(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ssrf guard: bad dial address %q: %w", address, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// Control receives an already-resolved IP; a non-IP here is unexpected.
		return fmt.Errorf("ssrf guard: unresolved dial host %q", host)
	}
	ip = ip.Unmap() // collapse ::ffff:a.b.c.d to the underlying IPv4
	if !ip.IsValid() ||
		!ip.IsGlobalUnicast() ||
		ip.IsLoopback() ||
		ip.IsUnspecified() ||
		ip.IsPrivate() || // RFC1918 + IPv6 ULA fc00::/7
		ip.IsLinkLocalUnicast() || // includes 169.254.0.0/16 + fe80::/10 (cloud metadata)
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsInterfaceLocalMulticast() {
		return fmt.Errorf("ssrf guard: blocked non-public address %s", ip)
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return fmt.Errorf("ssrf guard: blocked reserved address %s", ip)
		}
	}
	return nil
}

func guardConn(_, address string, _ syscall.RawConn) error {
	if !ssrfGuardEnabled.Load() {
		return nil
	}
	return classifyBlockedAddr(address)
}

var (
	guardedTransportOnce sync.Once
	guardedTransport     *http.Transport
)

func guardedHTTPTransport() *http.Transport {
	guardedTransportOnce.Do(func() {
		d := &net.Dialer{Timeout: 10 * time.Second, Control: guardConn}
		guardedTransport = &http.Transport{
			DialContext:         d.DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: 10 * time.Second,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConns:        100,
		}
	})
	return guardedTransport
}

// SafeHTTPClient returns an *http.Client whose dialer rejects connections to
// non-public IPs (see classifyBlockedAddr), sharing one connection pool.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: guardedHTTPTransport()}
}

// ValidateOutboundURL rejects URLs that are not http(s) or have no host,
// blocking file://, gopher://, and similar schemes before a request is built.
func ValidateOutboundURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("empty host")
	}
	return nil
}
