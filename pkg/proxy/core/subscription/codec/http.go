package codec

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// HTTPNode is the decoded form of an http:// or https:// proxy URI.
// https:// sets TLS=true and supports ?sni= and ?insecure=1.
type HTTPNode struct {
	NodeName       string
	Host           string
	Port           uint32
	Username       string
	Password       string
	TLS            bool
	SNI            string
	SkipCertVerify bool
}

func (n *HTTPNode) Protocol() contracts.Protocol { return contracts.ProtocolHTTP }

func (n *HTTPNode) Encode() string {
	scheme := "http"
	if n.TLS {
		scheme = "https"
	}

	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if n.SkipCertVerify {
		q.Set("insecure", "1")
	}

	u := url.URL{
		Scheme:   scheme,
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
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

func DecodeHTTP(uri string) (*HTTPNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("http: parse URI: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("http: unexpected scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("http: empty host")
	}
	port, err := strconv.ParseUint(u.Port(), 10, 32)
	if err != nil || port == 0 || port > 65535 {
		return nil, fmt.Errorf("http: missing or invalid port")
	}

	var username, password string
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}

	q := u.Query()
	n := &HTTPNode{
		NodeName: u.Fragment,
		Host:     host,
		Port:     uint32(port),
		Username: username,
		Password: password,
		TLS:      u.Scheme == "https",
		SNI:      q.Get("sni"),
	}
	if isTruthyParam(q.Get("insecure")) || isTruthyParam(q.Get("allowInsecure")) {
		n.SkipCertVerify = true
	}
	return n, nil
}

func isTruthyParam(v string) bool {
	s := strings.TrimSpace(strings.ToLower(v))
	return s == "1" || s == "true"
}
