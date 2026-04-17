package codec

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Hysteria2Node is the decoded form of a hysteria2:// URI.
// Accepts both "hysteria2://" and "hy2://" schemes on Decode; Encode always
// uses the canonical "hysteria2://" prefix.
type Hysteria2Node struct {
	NodeName string
	Host     string
	Port     uint32
	Password string

	SNI            string
	SkipCertVerify bool
	ALPN           []string // URI form is comma-separated, e.g. "h3,h2"

	Obfs         string // "salamander" or ""
	ObfsPassword string
}

func (n *Hysteria2Node) Protocol() contracts.Protocol { return contracts.ProtocolHysteria2 }

func (n *Hysteria2Node) Encode() string {
	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if n.SkipCertVerify {
		q.Set("insecure", "1")
	}
	if len(n.ALPN) > 0 {
		q.Set("alpn", strings.Join(n.ALPN, ","))
	}
	if n.Obfs != "" {
		q.Set("obfs", n.Obfs)
	}
	if n.ObfsPassword != "" {
		q.Set("obfs-password", n.ObfsPassword)
	}

	u := url.URL{
		Scheme:   "hysteria2",
		User:     url.User(n.Password),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeHysteria2(uri string) (*Hysteria2Node, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("hysteria2: parse URI: %w", err)
	}
	if u.Scheme != "hysteria2" && u.Scheme != "hy2" {
		return nil, fmt.Errorf("hysteria2: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("hysteria2: missing or invalid port")
	}
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	if password == "" {
		return nil, fmt.Errorf("hysteria2: empty password")
	}

	q := u.Query()
	// ALPN may arrive as repeated query keys (`alpn=h3&alpn=h2`, used by some
	// clients) or as a single comma-separated value (`alpn=h3,h2`, preferred
	// by others). Accept both; Encode emits the comma form.
	var alpn []string
	if vs := q["alpn"]; len(vs) > 1 {
		alpn = vs
	} else if raw := q.Get("alpn"); raw != "" {
		alpn = strings.Split(raw, ",")
	}
	n := &Hysteria2Node{
		NodeName:     u.Fragment,
		Host:         u.Hostname(),
		Port:         uint32(port),
		Password:     password,
		SNI:          q.Get("sni"),
		ALPN:         alpn,
		Obfs:         q.Get("obfs"),
		ObfsPassword: q.Get("obfs-password"),
	}
	if q.Get("insecure") == "1" {
		n.SkipCertVerify = true
	}
	return n, nil
}
