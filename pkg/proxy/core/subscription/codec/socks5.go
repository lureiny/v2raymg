package codec

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// SOCKS5Node is the decoded form of a socks5://, socks://, or socks5h:// URI.
//
// Supported formats:
//   - Unauthenticated: socks5://host:port#remark, socks5h://[ipv6]:port
//   - RFC plaintext auth: socks5://user:pass@host:port#remark
//   - v2rayN base64 auth: socks://BASE64(user:pass)@[ipv6]:port#remark
//
// Encode always uses the canonical "socks5://" scheme.
type SOCKS5Node struct {
	NodeName string
	Host     string
	Port     uint32
	Username string
	Password string
}

func (n *SOCKS5Node) Protocol() contracts.Protocol { return contracts.ProtocolSOCKS5 }

func (n *SOCKS5Node) Encode() string {
	u := url.URL{
		Scheme:   "socks5",
		Host:     hostPort(n.Host, n.Port),
		Fragment: n.NodeName,
	}
	if n.Username != "" || n.Password != "" {
		if n.Password != "" {
			u.User = url.UserPassword(n.Username, n.Password)
		} else {
			u.User = url.User(n.Username)
		}
	}
	return u.String()
}

func DecodeSOCKS5(uri string) (*SOCKS5Node, error) {
	idx := strings.Index(uri, "://")
	if idx < 0 {
		return nil, fmt.Errorf("socks5: missing scheme")
	}
	scheme := strings.ToLower(uri[:idx])
	switch scheme {
	case "socks5", "socks", "socks5h":
	default:
		return nil, fmt.Errorf("socks5: unexpected scheme %q", scheme)
	}

	rest := uri[idx+3:]
	var fragment string
	beforeFrag := rest
	if fragIdx := strings.IndexByte(rest, '#'); fragIdx >= 0 {
		beforeFrag = rest[:fragIdx]
		rawFrag := rest[fragIdx+1:]
		if p, err := url.Parse("socks5://x#" + rawFrag); err == nil {
			fragment = p.Fragment
		} else {
			fragment = rawFrag
		}
	}

	beforeQuery := beforeFrag
	if qIdx := strings.IndexByte(beforeFrag, '?'); qIdx >= 0 {
		beforeQuery = beforeFrag[:qIdx]
	}

	var host string
	var port uint32
	var username, password string

	if atIdx := strings.LastIndexByte(beforeQuery, '@'); atIdx >= 0 {
		rawUserInfo := beforeQuery[:atIdx]
		rawAddr := strings.TrimSuffix(beforeQuery[atIdx+1:], "/")

		var err error
		host, port, err = splitValidHostPort(rawAddr)
		if err != nil {
			return nil, fmt.Errorf("socks5: %w", err)
		}

		username, password = parseSOCKS5UserInfo(scheme, rawUserInfo)
	} else {
		rawAddr := strings.TrimSuffix(beforeQuery, "/")
		// Full-string base64 form: socks://BASE64(user:pass@host:port)
		if !strings.Contains(rawAddr, ":") && rawAddr != "" {
			if decoded, ok := decodeBase64Any(rawAddr); ok {
				if decAt := strings.LastIndexByte(decoded, '@'); decAt >= 0 {
					var err error
					host, port, err = splitValidHostPort(strings.TrimSuffix(decoded[decAt+1:], "/"))
					if err == nil {
						u, p := splitUserPass(decoded[:decAt])
						return &SOCKS5Node{
							NodeName: fragment,
							Host:     host,
							Port:     port,
							Username: u,
							Password: p,
						}, nil
					}
				}
			}
		}

		var err error
		host, port, err = splitValidHostPort(rawAddr)
		if err != nil {
			return nil, fmt.Errorf("socks5: %w", err)
		}
	}

	return &SOCKS5Node{
		NodeName: fragment,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	}, nil
}

func splitValidHostPort(addr string) (string, uint32, error) {
	if addr == "" {
		return "", 0, fmt.Errorf("empty host")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		var addrErr *net.AddrError
		if errors.As(err, &addrErr) && addrErr.Err != "" {
			return "", 0, fmt.Errorf("invalid host:port: %s", addrErr.Err)
		}
		return "", 0, fmt.Errorf("invalid host:port")
	}
	if host == "" {
		return "", 0, fmt.Errorf("empty host")
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil || port == 0 || port > 65535 {
		return "", 0, fmt.Errorf("missing or invalid port")
	}
	return host, uint32(port), nil
}

func parseSOCKS5UserInfo(scheme, rawUserInfo string) (string, string) {
	if rawUserInfo == "" {
		return "", ""
	}
	// If ':' is present in raw userinfo (or URL-encoded %3A? Check unescaped after split),
	// treat as plaintext user:pass per RFC 3986.
	if colonIdx := strings.IndexByte(rawUserInfo, ':'); colonIdx >= 0 {
		u := unescapeUserComponent(rawUserInfo[:colonIdx])
		p := unescapeUserComponent(rawUserInfo[colonIdx+1:])
		return u, p
	}

	unescaped := unescapeUserComponent(rawUserInfo)
	// If %3A was decoded into ':', handle it as plaintext user:pass.
	if colonIdx := strings.IndexByte(unescaped, ':'); colonIdx >= 0 {
		return unescaped[:colonIdx], unescaped[colonIdx+1:]
	}

	// v2rayN format: socks://BASE64(user:pass)@host:port (also tolerate on socks5/socks5h
	// when the decoded payload is clearly printable UTF-8 user:pass).
	for _, candidate := range []string{unescaped, rawUserInfo} {
		if decoded, ok := decodeBase64Any(candidate); ok && isPrintableUserPass(decoded) {
			return splitUserPass(decoded)
		}
	}

	_ = scheme
	return unescaped, ""
}

func unescapeUserComponent(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func splitUserPass(s string) (string, string) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s, ""
}

func decodeBase64Any(s string) (string, bool) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if d, err := enc.DecodeString(s); err == nil {
			return string(d), true
		}
	}
	return "", false
}

func isPrintableUserPass(s string) bool {
	if !strings.Contains(s, ":") || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
