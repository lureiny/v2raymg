package codec

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TrojanNode is the decoded form of a trojan:// URI.
// TLS is always implied by the protocol; Security is only used for reality/xtls.
//
// Security values: "" (plain TLS, canonical) / "reality" / "xtls".
// Both "tls" and "none" normalize to "": "tls" is redundant (TLS is implied),
// "none" is a misconfiguration (trojan requires TLS at the wire level).
type TrojanNode struct {
	NodeName string
	Host     string
	Port     uint32
	Password string

	Security       string
	SNI            string
	Flow           string // e.g. xtls-rprx-vision
	Fingerprint    string // uTLS fingerprint (not TLS cert pinning)
	SkipCertVerify bool

	RealityPublicKey string
	RealityShortID   string

	Transport string

	WSPath string
	WSHost string

	GRPCServiceName string
}

func (n *TrojanNode) Protocol() contracts.Protocol { return contracts.ProtocolTrojan }

func (n *TrojanNode) Encode() string {
	q := url.Values{}

	// Only emit security for non-default variants
	if n.Security == "reality" || n.Security == "xtls" {
		q.Set("security", n.Security)
	}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if n.Flow != "" {
		q.Set("flow", n.Flow)
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
	}
	if n.SkipCertVerify {
		q.Set("insecure", "1")
	}

	t := n.Transport
	if t != "" && t != "tcp" {
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
	}

	u := url.URL{
		Scheme:   "trojan",
		User:     url.User(n.Password),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeTrojan(uri string) (*TrojanNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("trojan: parse URI: %w", err)
	}
	if u.Scheme != "trojan" {
		return nil, fmt.Errorf("trojan: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("trojan: missing or invalid port")
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return nil, fmt.Errorf("trojan: empty password")
	}

	q := u.Query()
	security := q.Get("security")
	// TLS is hardcoded at the trojan wire level. "tls" is redundant and "none"
	// is a misconfig — both collapse to the canonical empty value.
	if security == "tls" || security == "none" {
		security = ""
	}
	transport := q.Get("type")
	if transport == "tcp" {
		// tcp is the default; normalize to "" so Transport has a single canonical form.
		transport = ""
	}
	n := &TrojanNode{
		NodeName:    u.Fragment,
		Host:        u.Hostname(),
		Port:        uint32(port),
		Password:    password,
		Security:    security,
		SNI:         q.Get("sni"),
		Flow:        q.Get("flow"),
		Fingerprint: q.Get("fp"),
		Transport:   transport,
	}
	if n.Security == "reality" {
		n.RealityPublicKey = q.Get("pbk")
		n.RealityShortID = q.Get("sid")
	}
	if q.Get("insecure") == "1" {
		n.SkipCertVerify = true
	}

	switch n.Transport {
	case "ws":
		n.WSPath = q.Get("path")
		n.WSHost = q.Get("host")
	case "grpc":
		n.GRPCServiceName = q.Get("serviceName")
	}

	return n, nil
}
