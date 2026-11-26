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
	scheme := strings.ToLower(trimmed[:idx])

	if spec, err := ParseURI(trimmed); err == nil {
		target := formatHostPort(spec.Host, spec.Port)
		hasSecret := spec.Password != "" || spec.UUID != "" || spec.Username != ""
		u := url.URL{
			Scheme:   scheme,
			Host:     target,
			Fragment: spec.Remark,
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
	if fragIdx := strings.IndexByte(rest, '#'); fragIdx >= 0 {
		beforeFrag = rest[:fragIdx]
		fragSuffix = rest[fragIdx:]
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
	// port is a valid numeric port [1, 65535]. Credential-bearing schemes
	// (trojan/vless/vmess/ss/hy2/hysteria2/tuic/anytls/snell/socks) and
	// unknown schemes always redact fail-closed so a missing '@' never echoes
	// a password/UUID as the host.
	switch scheme {
	case "http", "https", "socks5", "socks5h":
		addr := strings.TrimSuffix(beforeQuery, "/")
		if host, port, err := net.SplitHostPort(addr); err == nil && host != "" && isValidPortStr(port) {
			return scheme + "://" + net.JoinHostPort(host, port) + fragSuffix
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
		return normalizeSchemeToProtocol(strings.ToLower(trimmed[:idx])), ""
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
