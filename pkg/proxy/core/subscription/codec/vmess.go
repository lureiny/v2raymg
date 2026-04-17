package codec

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// VMessNode is the decoded form of a vmess:// URI.
//
// Security values: "tls" / "" (no TLS).
// Transport values: "tcp" / "ws" / "h2" / "grpc" / "xhttp".
// HeaderType is the VMess "type" field (TCP header obfuscation), distinct from
// Transport; Decode normalizes the default "none" to "".
//
// Host handling: VMess carries server address in the JSON "add" field and the
// transport-level Host (ws/h2/xhttp) in the JSON "host" field, so neither
// passes through the URL authority. The package-level hostPort helper (which
// brackets IPv6 literals per RFC 3986) is therefore NOT used for VMess —
// IPv6 addresses go into JSON as-is, without brackets.
type VMessNode struct {
	NodeName string
	Host     string
	Port     uint32
	UUID     string
	AlterId  int // 0 for AEAD; non-zero for legacy mode

	Security    string
	SNI         string
	Fingerprint string // uTLS fingerprint (not TLS cert pinning)

	HeaderType string

	Transport string

	WSPath string
	WSHost string

	H2Path string
	H2Host string

	GRPCServiceName string

	XHTTPPath string
	XHTTPHost string
}

func (n *VMessNode) Protocol() contracts.Protocol { return contracts.ProtocolVMess }

type vmessEncodeBody struct {
	V    string `json:"v"`
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	Aid  string `json:"aid"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
	TLS  string `json:"tls,omitempty"`
	SNI  string `json:"sni,omitempty"`
	FP   string `json:"fp,omitempty"`
}

func (n *VMessNode) Encode() string {
	// VMess JSON requires a "type" field; most clients default it to "none".
	// The field is only meaningful with net=tcp; other transports ignore it.
	headerType := n.HeaderType
	if headerType == "" {
		headerType = "none"
	}
	b := vmessEncodeBody{
		V:    "2",
		PS:   n.NodeName,
		Add:  n.Host,
		Port: strconv.FormatUint(uint64(n.Port), 10),
		ID:   n.UUID,
		Aid:  strconv.Itoa(n.AlterId),
		Type: headerType,
		SNI:  n.SNI,
		FP:   n.Fingerprint,
	}
	if n.Security == "tls" {
		b.TLS = "tls"
	}

	// VMess wire format requires a non-empty `net`. We keep "" as the internal
	// canonical (so round-trip preserves it), but emit "tcp" on the wire so
	// clients like sing-box / shadowrocket that reject empty net still accept
	// the URI.
	if n.Transport == "" {
		b.Net = "tcp"
	} else {
		b.Net = n.Transport
	}

	switch n.Transport {
	case "ws":
		b.Path = n.WSPath
		b.Host = n.WSHost
	case "h2", "http":
		b.Net = "h2"
		b.Path = n.H2Path
		b.Host = n.H2Host
	case "grpc":
		b.Path = n.GRPCServiceName
	case "xhttp", "splithttp":
		b.Net = "xhttp"
		b.Path = n.XHTTPPath
		b.Host = n.XHTTPHost
	}

	data, _ := json.Marshal(b)
	return "vmess://" + base64.StdEncoding.EncodeToString(data)
}

// vmessDecodeBody tolerates port/aid being either a JSON number or string.
type vmessDecodeBody struct {
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
	FP   string `json:"fp"`
}

func DecodeVMess(uri string) (*VMessNode, error) {
	body := strings.TrimPrefix(uri, "vmess://")
	var raw []byte
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	} {
		if d, err := enc.DecodeString(body); err == nil {
			raw = d
			break
		}
	}
	if raw == nil {
		return nil, fmt.Errorf("vmess: base64 decode failed")
	}

	var b vmessDecodeBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("vmess: invalid JSON: %w", err)
	}
	if b.Add == "" {
		return nil, fmt.Errorf("vmess: missing server address (add)")
	}
	if b.ID == "" {
		return nil, fmt.Errorf("vmess: empty UUID (id)")
	}
	port := anyToUint32(b.Port)
	if port == 0 {
		return nil, fmt.Errorf("vmess: missing or invalid port")
	}

	headerType := b.Type
	if headerType == "none" {
		// "none" is the canonical default; normalize to "" for a single form.
		headerType = ""
	}
	// Collapse wire-level "tcp" (VMess default) back to canonical empty, and
	// canonicalize "http" → "h2" here so Encode/Decode are symmetric when a
	// caller constructs VMessNode{Transport: "http"} directly.
	transport := b.Net
	switch transport {
	case "tcp":
		transport = ""
	case "http":
		transport = "h2"
	case "splithttp":
		transport = "xhttp"
	}
	n := &VMessNode{
		NodeName:    b.PS,
		Host:        b.Add,
		Port:        port,
		UUID:        b.ID,
		AlterId:     anyToInt(b.Aid),
		SNI:         b.SNI,
		Fingerprint: b.FP,
		HeaderType:  headerType,
		Transport:   transport,
	}
	if b.TLS == "tls" {
		n.Security = "tls"
	}

	switch transport {
	case "ws":
		n.WSPath = b.Path
		n.WSHost = b.Host
	case "h2":
		n.H2Path = b.Path
		n.H2Host = b.Host
	case "grpc":
		n.GRPCServiceName = b.Path
	case "xhttp":
		n.XHTTPPath = b.Path
		n.XHTTPHost = b.Host
	}

	return n, nil
}

// anyToUint32 / anyToInt read a VMess JSON field that may be number or string
// (shadowrocket emits strings, v2rayN emits numbers).
func anyToUint32(v any) uint32 {
	switch val := v.(type) {
	case float64:
		return uint32(val)
	case string:
		n, _ := strconv.ParseUint(val, 10, 32)
		return uint32(n)
	}
	return 0
}

func anyToInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	}
	return 0
}
