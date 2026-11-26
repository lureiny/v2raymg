package codec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ShadowsocksNode is the decoded form of an ss:// URI.
//
// Two SIP variants are supported:
//   - SIP002 (stream/AEAD ciphers): userinfo is base64url(method:password) — RFC 4648 §5, no padding.
//   - SIP022 / AEAD-2022 (methods prefixed with "2022-"): userinfo is method:password (percent-encoded).
//
// PluginOpts follows SIP003 "plugin=name;k1=v1;k2" in the query; flag-style
// tokens (no "=") are stored with an empty value.
type ShadowsocksNode struct {
	NodeName string
	Host     string
	Port     uint32
	Password string
	Method   string

	Plugin     string
	PluginOpts map[string]string
}

func (n *ShadowsocksNode) Protocol() contracts.Protocol { return contracts.ProtocolShadowsocks }

// Encode returns a SIP002/SIP022-compliant ss:// URI. Plugin opts are emitted
// in lexicographic key order so round-trip produces a stable form; plugins
// whose wire protocol is sensitive to opt order (e.g. old simple-obfs variants
// that expect "obfs" before "obfs-host") are not supported by this encoder.
func (n *ShadowsocksNode) Encode() string {
	u := url.URL{
		Scheme:   "ss",
		Host:     hostPort(n.Host, n.Port),
		Fragment: n.NodeName,
	}
	if strings.HasPrefix(n.Method, "2022-") {
		u.User = url.UserPassword(n.Method, n.Password)
	} else {
		creds := n.Method + ":" + n.Password
		u.User = url.User(base64.RawURLEncoding.EncodeToString([]byte(creds)))
	}

	if n.Plugin != "" {
		parts := []string{n.Plugin}
		keys := make([]string, 0, len(n.PluginOpts))
		for k := range n.PluginOpts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v := n.PluginOpts[k]; v == "" {
				parts = append(parts, k)
			} else {
				parts = append(parts, k+"="+v)
			}
		}
		q := url.Values{}
		q.Set("plugin", strings.Join(parts, ";"))
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func DecodeShadowsocks(uri string) (*ShadowsocksNode, error) {
	idx := strings.Index(uri, "://")
	if idx < 0 {
		return nil, fmt.Errorf("ss: parse URI: missing scheme")
	}
	scheme := strings.ToLower(uri[:idx])
	if scheme != "ss" {
		return nil, fmt.Errorf("ss: unexpected scheme %q", scheme)
	}

	// Check whether authority before '?'/'#' lacks '@' — if so, handle OpenWrt
	// PassWall/SSRPlus full-string base64 format: ss://BASE64(method:pass@host:port)#remark
	rest := uri[idx+3:]
	beforeFrag := rest
	if fragIdx := strings.IndexByte(rest, '#'); fragIdx >= 0 {
		beforeFrag = rest[:fragIdx]
	}
	beforeQuery := beforeFrag
	if qIdx := strings.IndexByte(beforeFrag, '?'); qIdx >= 0 {
		beforeQuery = beforeFrag[:qIdx]
	}
	if !strings.Contains(beforeQuery, "@") {
		if n, err := decodeShadowsocksFullBase64(uri); err == nil {
			return n, nil
		}
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("ss: parse URI: %w", sanitizeURLError(err))
	}
	if u.Scheme != "ss" {
		return nil, fmt.Errorf("ss: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("ss: missing or invalid port")
	}

	method, password := decodeSSUserInfo(u)
	if method == "" {
		return nil, fmt.Errorf("ss: cannot extract cipher method from userinfo")
	}
	if password == "" {
		return nil, fmt.Errorf("ss: empty password")
	}

	n := &ShadowsocksNode{
		NodeName: u.Fragment,
		Host:     u.Hostname(),
		Port:     uint32(port),
		Password: password,
		Method:   method,
	}

	applyShadowsocksPluginQuery(n, u.Query())
	return n, nil
}

func decodeShadowsocksFullBase64(uri string) (*ShadowsocksNode, error) {
	idx := strings.Index(uri, "://")
	rest := uri[idx+3:]

	var fragment string
	beforeFrag := rest
	if fragIdx := strings.IndexByte(rest, '#'); fragIdx >= 0 {
		beforeFrag = rest[:fragIdx]
		rawFrag := rest[fragIdx+1:]
		if p, err := url.Parse("ss://x#" + rawFrag); err == nil {
			fragment = p.Fragment
		} else {
			fragment = rawFrag
		}
	}

	beforeQuery := beforeFrag
	rawQuery := ""
	if qIdx := strings.IndexByte(beforeFrag, '?'); qIdx >= 0 {
		beforeQuery = beforeFrag[:qIdx]
		rawQuery = beforeFrag[qIdx+1:]
	}

	decodedStr, ok := decodeBase64Any(beforeQuery)
	if !ok && strings.HasSuffix(beforeQuery, "/") {
		decodedStr, ok = decodeBase64Any(strings.TrimSuffix(beforeQuery, "/"))
	}
	if !ok {
		return nil, fmt.Errorf("ss: invalid full-string base64")
	}

	decBeforeQuery := decodedStr
	decRawQuery := ""
	if qIdx := strings.IndexByte(decodedStr, '?'); qIdx >= 0 {
		decBeforeQuery = decodedStr[:qIdx]
		decRawQuery = decodedStr[qIdx+1:]
	}

	atIdx := strings.LastIndexByte(decBeforeQuery, '@')
	if atIdx <= 0 {
		return nil, fmt.Errorf("ss: decoded payload missing @")
	}
	credPart := decBeforeQuery[:atIdx]
	addrPart := strings.TrimSuffix(decBeforeQuery[atIdx+1:], "/")

	credParts := strings.SplitN(credPart, ":", 2)
	if len(credParts) != 2 || credParts[0] == "" {
		return nil, fmt.Errorf("ss: cannot extract cipher method")
	}
	if credParts[1] == "" {
		return nil, fmt.Errorf("ss: empty password")
	}

	host, portStr, err := net.SplitHostPort(addrPart)
	if err != nil || host == "" {
		return nil, fmt.Errorf("ss: missing or invalid host:port")
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil || port == 0 || port > 65535 {
		return nil, fmt.Errorf("ss: missing or invalid port")
	}

	n := &ShadowsocksNode{
		NodeName: fragment,
		Host:     host,
		Port:     uint32(port),
		Method:   credParts[0],
		Password: credParts[1],
	}

	if rawQuery != "" {
		if q, err := url.ParseQuery(rawQuery); err == nil {
			applyShadowsocksPluginQuery(n, q)
		}
	} else if decRawQuery != "" {
		if q, err := url.ParseQuery(decRawQuery); err == nil {
			applyShadowsocksPluginQuery(n, q)
		}
	}
	return n, nil
}

func applyShadowsocksPluginQuery(n *ShadowsocksNode, q url.Values) {
	if raw := q.Get("plugin"); raw != "" {
		plugin, opts := parsePluginSpec(raw)
		n.Plugin = plugin
		n.PluginOpts = opts
		return
	}
	if raw := q.Get("shadow-tls"); raw != "" {
		jsonStr := raw
		if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
			if decoded, ok := decodeBase64Any(raw); ok {
				jsonStr = decoded
			}
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &m); err == nil && len(m) > 0 {
			n.Plugin = "shadow-tls"
			n.PluginOpts = make(map[string]string, len(m))
			for k, v := range m {
				switch val := v.(type) {
				case string:
					n.PluginOpts[k] = val
				case float64:
					n.PluginOpts[k] = strconv.Itoa(int(val))
				case bool:
					if val {
						n.PluginOpts[k] = "true"
					}
				}
			}
		}
	}
}

func decodeSSUserInfo(u *url.URL) (method, password string) {
	if u.User == nil {
		return "", ""
	}
	username := u.User.Username()

	if strings.HasPrefix(username, "2022-") {
		pwd, _ := u.User.Password()
		return username, pwd
	}

	raw := u.User.String()
	var decoded []byte
	// RawURLEncoding is the SIP002 canonical; others are tolerated for shadowrocket/outline quirks.
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.StdEncoding,
		base64.RawStdEncoding,
	} {
		if d, err := enc.DecodeString(raw); err == nil {
			decoded = d
			break
		}
	}
	if decoded == nil {
		return "", ""
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// SIP003 itself only defines `k=v` plugin opts. v2ray-plugin et al. extend
// the syntax with flag-style bare keys (e.g. `;tls;`); we tolerate those by
// storing them with an empty value.
func parsePluginSpec(raw string) (name string, opts map[string]string) {
	parts := strings.Split(raw, ";")
	name = parts[0]
	opts = make(map[string]string)
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			opts[kv[0]] = kv[1]
		} else {
			opts[kv[0]] = ""
		}
	}
	return name, opts
}
