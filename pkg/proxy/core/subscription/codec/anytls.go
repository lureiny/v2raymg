package codec

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// AnyTLSNode is the decoded form of an anytls:// URI.
//
// URI syntax follows the upstream spec referenced in mihomo Alpha
// (`common/convert/converter.go:586-627`):
//
//	anytls://[username:]password@server:port?insecure=1&sni=xxx&hpkp=fp#name
//
// Notes:
//
//   - Userinfo: if the colon is present, password is used directly. If
//     missing (single-segment form `anytls://pwd@host:port`), the lone
//     value becomes the password — mirroring mihomo's parser
//     `password, exist := link.User.Password(); if !exist { password =
//     link.User.Username() }`. Encode always emits the single-segment
//     form (mihomo's outbound struct has no username field, so the
//     upstream `username` slot is decorative).
//   - Query keys: `insecure` (1 → SkipCertVerify), `sni`, `hpkp` (→
//     Fingerprint). Spec is plain (no underscores or kebab-case).
//   - PaddingScheme / Idle* / MinIdleSession / ALPN are NOT in the URI
//     spec. They live on the struct so SubscriptionSpec.Extensions can
//     carry them through the in-memory exchange, but Encode/Decode
//     deliberately skip them — adding non-standard keys would confuse
//     generic anytls clients without buying interop, and our clash
//     converter reads them from Extensions instead.
type AnyTLSNode struct {
	NodeName string
	Host     string
	Port     uint32
	Password string

	SNI            string
	SkipCertVerify bool
	Fingerprint    string // URI key `hpkp`; mihomo client `fingerprint`

	// In-memory carriers (not encoded into URI).
	ALPN                     []string
	PaddingScheme            string // server-only inline text; empty defers to mihomo DefaultPaddingScheme
	IdleSessionCheckInterval int    // client-only seconds; 0 defers to mihomo runtime fallback (30s)
	IdleSessionTimeout       int    // client-only seconds; 0 defers to mihomo runtime fallback (30s)
	MinIdleSession           int    // client-only
}

func (n *AnyTLSNode) Protocol() contracts.Protocol { return contracts.ProtocolAnyTLS }

func (n *AnyTLSNode) Encode() string {
	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if n.SkipCertVerify {
		q.Set("insecure", "1")
	}
	if n.Fingerprint != "" {
		q.Set("hpkp", n.Fingerprint)
	}

	u := url.URL{
		Scheme:   "anytls",
		User:     url.User(n.Password),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeAnyTLS(uri string) (*AnyTLSNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("anytls: parse URI: %w", err)
	}
	if u.Scheme != "anytls" {
		return nil, fmt.Errorf("anytls: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("anytls: missing or invalid port")
	}
	if u.User == nil {
		return nil, fmt.Errorf("anytls: missing userinfo")
	}
	password, hasPwd := u.User.Password()
	if !hasPwd {
		// Single-segment short form (no colon) — mihomo treats the lone
		// value as the password; do the same.
		password = u.User.Username()
	}
	if password == "" {
		return nil, fmt.Errorf("anytls: empty password")
	}

	q := u.Query()
	n := &AnyTLSNode{
		NodeName:    u.Fragment,
		Host:        u.Hostname(),
		Port:        uint32(port),
		Password:    password,
		SNI:         q.Get("sni"),
		Fingerprint: q.Get("hpkp"),
	}
	if q.Get("insecure") == "1" {
		n.SkipCertVerify = true
	}
	// Spec keys are sni/insecure/hpkp; unrelated query params from
	// third-party clients are silently ignored so we don't reject
	// otherwise-valid URIs over a stray key.
	return n, nil
}
