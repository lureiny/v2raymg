package codec

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// SnellNode is the decoded form of a snell:// URI.
type SnellNode struct {
	NodeName string
	Host     string
	Port     uint32
	PSK      string

	Version  int    // protocol version; 1 / 3 / 4 / 5
	Obfs     string // "tls" / "http" / ""
	ObfsHost string
}

func (n *SnellNode) Protocol() contracts.Protocol { return contracts.ProtocolSnell }

func (n *SnellNode) Encode() string {
	q := url.Values{}
	if n.Version > 0 {
		q.Set("version", strconv.Itoa(n.Version))
	}
	if n.Obfs != "" {
		q.Set("obfs", n.Obfs)
	}
	if n.ObfsHost != "" {
		q.Set("obfs-host", n.ObfsHost)
	}

	u := url.URL{
		Scheme:   "snell",
		User:     url.User(n.PSK),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeSnell(uri string) (*SnellNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("snell: parse URI: %w", err)
	}
	if u.Scheme != "snell" {
		return nil, fmt.Errorf("snell: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("snell: missing or invalid port")
	}
	psk := ""
	if u.User != nil {
		psk = u.User.Username()
	}
	if psk == "" {
		return nil, fmt.Errorf("snell: empty PSK")
	}

	q := u.Query()
	version, _ := strconv.Atoi(q.Get("version"))
	return &SnellNode{
		NodeName: u.Fragment,
		Host:     u.Hostname(),
		Port:     uint32(port),
		PSK:      psk,
		Version:  version,
		Obfs:     q.Get("obfs"),
		ObfsHost: q.Get("obfs-host"),
	}, nil
}
