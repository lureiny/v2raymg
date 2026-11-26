package chain

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// RedactURI returns a log- and API-safe representation of rawURI with all
// credentials (passwords, UUIDs, base64 userinfo blobs, and sensitive query
// parameters) stripped.
func RedactURI(rawURI string) string {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return ""
	}

	idx := strings.Index(trimmed, "://")
	if idx <= 0 {
		return "[redacted]"
	}
	rawScheme := trimmed[:idx]
	if !isValidURIScheme(rawScheme) {
		return "[redacted]"
	}
	scheme := strings.ToLower(rawScheme)

	if spec, err := ParseURI(trimmed); err == nil {
		target := formatHostPort(spec.Host, spec.Port)
		hasSecret := spec.Password != "" || spec.UUID != "" || spec.Username != ""
		// If an unescaped '#' inside a numeric password caused the URI to parse
		// without userinfo (e.g. `socks5://user:1234#SECRET@host:1080`), redact
		// fail-closed so neither the prefix nor the tail leaks.
		if !hasSecret && strings.ContainsRune(spec.Remark, '@') {
			return scheme + "://[redacted]"
		}
		frag := spec.Remark
		if strings.ContainsRune(frag, '@') {
			frag = ""
		}
		u := url.URL{
			Scheme:   scheme,
			Host:     target,
			Fragment: frag,
		}
		if hasSecret || scheme == "vmess" || scheme == "ss" {
			// Insert literal [redacted] userinfo without percent-encoding brackets.
			if u.Fragment != "" {
				return scheme + "://[redacted]@" + target + "#" + (&url.URL{Fragment: u.Fragment}).EscapedFragment()
			}
			return scheme + "://[redacted]@" + target
		}
		return u.String()
	}

	// Fallback for malformed or unsupported URIs: never echo raw base64 blobs
	// or userinfo/query values.
	if scheme == "vmess" {
		return "vmess://[redacted]"
	}

	rest := trimmed[idx+3:]
	beforeFrag := rest
	fragSuffix := ""
	fragHasAt := false
	if fragIdx := strings.IndexByte(rest, '#'); fragIdx >= 0 {
		beforeFrag = rest[:fragIdx]
		rawFrag := rest[fragIdx:]
		// If the fallback fragment contains '@', '#' may have been an unescaped
		// character inside credentials (e.g. `socks5://user:1234#SECRET@host:1080`).
		// Never echo such fragments in fallback mode.
		if strings.ContainsRune(rawFrag, '@') {
			fragHasAt = true
		} else {
			fragSuffix = rawFrag
		}
	}
	beforeQuery := beforeFrag
	if qIdx := strings.IndexByte(beforeFrag, '?'); qIdx >= 0 {
		beforeQuery = beforeFrag[:qIdx]
	}

	if atIdx := strings.LastIndexByte(beforeQuery, '@'); atIdx >= 0 {
		addr := strings.TrimSuffix(beforeQuery[atIdx+1:], "/")
		if host, port, err := net.SplitHostPort(addr); err == nil && host != "" && isValidPortStr(port) {
			return scheme + "://[redacted]@" + net.JoinHostPort(host, port) + fragSuffix
		}
		return scheme + "://[redacted]"
	}

	// Without '@', only schemes that legitimately allow unauthenticated
	// host:port targets may expose host:port in fallback mode, and only when
	// port is a valid numeric port [1, 65535] and no '@' appeared in the
	// fragment (which would indicate a broken user:pass#...@host credential).
	if !fragHasAt {
		switch scheme {
		case "http", "https", "socks5", "socks5h":
			addr := strings.TrimSuffix(beforeQuery, "/")
			if host, port, err := net.SplitHostPort(addr); err == nil && host != "" && isValidPortStr(port) {
				return scheme + "://" + net.JoinHostPort(host, port) + fragSuffix
			}
		}
	}
	return scheme + "://[redacted]"
}

func isValidPortStr(port string) bool {
	if port == "" {
		return false
	}
	p, err := strconv.ParseUint(port, 10, 32)
	return err == nil && p >= 1 && p <= 65535
}

// SummarizeURI extracts only the canonical protocol name and host:port target
// from rawURI for admin inspection endpoints/MCP responses. Never leaks any
// credentials or base64 blobs.
func SummarizeURI(rawURI string) (protocol, target string) {
	trimmed := strings.TrimSpace(rawURI)
	if trimmed == "" {
		return "", ""
	}
	if spec, err := ParseURI(trimmed); err == nil {
		return string(spec.Protocol), formatHostPort(spec.Host, spec.Port)
	}
	if idx := strings.Index(trimmed, "://"); idx > 0 {
		rawScheme := trimmed[:idx]
		if !isValidURIScheme(rawScheme) {
			return "", ""
		}
		return normalizeSchemeToProtocol(strings.ToLower(rawScheme)), ""
	}
	return "", ""
}

func formatHostPort(host string, port uint32) string {
	return net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10))
}

func normalizeSchemeToProtocol(scheme string) string {
	switch scheme {
	case "hy2", "hysteria2":
		return string(contracts.ProtocolHysteria2)
	case "ss":
		return string(contracts.ProtocolShadowsocks)
	case "socks", "socks5", "socks5h":
		return string(contracts.ProtocolSOCKS5)
	case "http", "https":
		return string(contracts.ProtocolHTTP)
	case "vless":
		return string(contracts.ProtocolVLess)
	case "vmess":
		return string(contracts.ProtocolVMess)
	case "trojan":
		return string(contracts.ProtocolTrojan)
	case "tuic":
		return string(contracts.ProtocolTUIC)
	case "anytls":
		return string(contracts.ProtocolAnyTLS)
	case "snell":
		return string(contracts.ProtocolSnell)
	default:
		return scheme
	}
}
