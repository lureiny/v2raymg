package codec

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// VLessNode is the decoded form of a vless:// URI.
//
// Security values: "" (plain, no TLS, canonical) / "tls" / "reality" / "xtls".
// Decode normalizes "none" to "" (both mean plain TCP).
// Transport values: "" (canonical default, == tcp) / "ws" / "grpc" /
// "xhttp" / "splithttp". Decode normalizes an incoming type=tcp to "" so
// the field has a single canonical form.
type VLessNode struct {
	NodeName string
	Host     string
	Port     uint32
	UUID     string

	Security       string
	SNI            string
	Flow           string // e.g. xtls-rprx-vision
	Encryption     string // e.g. "none" or mlkem768...
	Fingerprint    string // uTLS fingerprint (not TLS cert pinning)
	SkipCertVerify bool

	RealityPublicKey string
	RealityShortID   string
	RealitySpiderX   string

	Transport string

	WSPath string
	WSHost string

	GRPCServiceName string

	H2Path string
	H2Host string

	HTTPUpgradePath string
	HTTPUpgradeHost string

	XHTTPPath string
	XHTTPHost string
	XHTTPMode string
}

func (n *VLessNode) Protocol() contracts.Protocol { return contracts.ProtocolVLess }

func (n *VLessNode) Encode() string {
	q := url.Values{}

	if n.Security != "" {
		q.Set("security", n.Security)
	}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if n.Flow != "" {
		q.Set("flow", n.Flow)
	}
	if n.Encryption != "" {
		q.Set("encryption", n.Encryption)
	}
	if n.Fingerprint != "" {
		q.Set("fp", n.Fingerprint)
	}
	if n.Security == "reality" {
		if n.RealityPublicKey != "" {
			q.Set("pbk", n.RealityPublicKey)
		}
		if n.RealityShortID != "" {
			q.Set("sid", n.RealityShortID)
		}
		if n.RealitySpiderX != "" {
			q.Set("spx", n.RealitySpiderX)
		}
	}
	if n.SkipCertVerify {
		q.Set("insecure", "1")
	}

	t := n.Transport
	if t != "" && t != "tcp" {
		if t == "http" {
			t = "h2"
		}
		q.Set("type", t)
	}
	switch t {
	case "ws":
		if n.WSPath != "" {
			q.Set("path", n.WSPath)
		}
		if n.WSHost != "" {
			q.Set("host", n.WSHost)
		}
	case "grpc":
		if n.GRPCServiceName != "" {
			q.Set("serviceName", n.GRPCServiceName)
		}
	case "h2", "http":
		if n.H2Path != "" {
			q.Set("path", n.H2Path)
		}
		if n.H2Host != "" {
			q.Set("host", n.H2Host)
		}
	case "httpupgrade":
		if n.HTTPUpgradePath != "" {
			q.Set("path", n.HTTPUpgradePath)
		}
		if n.HTTPUpgradeHost != "" {
			q.Set("host", n.HTTPUpgradeHost)
		}
	case "xhttp", "splithttp":
		if n.XHTTPPath != "" {
			q.Set("path", n.XHTTPPath)
		}
		if n.XHTTPHost != "" {
			q.Set("host", n.XHTTPHost)
		}
		if n.XHTTPMode != "" {
			q.Set("mode", n.XHTTPMode)
		}
	}

	u := url.URL{
		Scheme:   "vless",
		User:     url.User(n.UUID),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeVLess(uri string) (*VLessNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("vless: parse URI: %w", err)
	}
	if u.Scheme != "vless" {
		return nil, fmt.Errorf("vless: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("vless: missing or invalid port")
	}
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	if uuid == "" {
		return nil, fmt.Errorf("vless: empty UUID")
	}

	q := u.Query()
	transport := q.Get("type")
	switch transport {
	case "tcp":
		// tcp is the default; normalize to "" so Transport has a single canonical form.
		transport = ""
	case "http":
		transport = "h2"
	}
	security := q.Get("security")
	if security == "none" {
		// security=none means "no TLS" — same as canonical empty.
		security = ""
	}
	n := &VLessNode{
		NodeName:    u.Fragment,
		Host:        u.Hostname(),
		Port:        uint32(port),
		UUID:        uuid,
		Security:    security,
		SNI:         q.Get("sni"),
		Flow:        q.Get("flow"),
		Encryption:  q.Get("encryption"),
		Fingerprint: q.Get("fp"),
		Transport:   transport,
	}
	if n.Security == "reality" {
		n.RealityPublicKey = q.Get("pbk")
		n.RealityShortID = q.Get("sid")
		n.RealitySpiderX = q.Get("spx")
	}
	if isTruthyParam(q.Get("insecure")) || isTruthyParam(q.Get("allowInsecure")) {
		n.SkipCertVerify = true
	}

	switch n.Transport {
	case "ws":
		n.WSPath = q.Get("path")
		n.WSHost = q.Get("host")
	case "grpc":
		n.GRPCServiceName = q.Get("serviceName")
	case "h2":
		n.H2Path = q.Get("path")
		n.H2Host = q.Get("host")
	case "httpupgrade":
		n.HTTPUpgradePath = q.Get("path")
		n.HTTPUpgradeHost = q.Get("host")
	case "xhttp", "splithttp":
		n.XHTTPPath = q.Get("path")
		n.XHTTPHost = q.Get("host")
		n.XHTTPMode = q.Get("mode")
	}

	return n, nil
}
