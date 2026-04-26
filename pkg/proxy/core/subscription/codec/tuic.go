package codec

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TuicNode is the decoded form of a tuic:// URI (v5 only).
//
// URI syntax follows the de-facto dae standard with mihomo's revisions:
//
//	tuic://<uuid>:<password>@host:port?congestion_control=...&alpn=...&sni=...&disable_sni=1&udp_relay_mode=...#name
//
// Notes worth flagging:
//
//   - Query keys use underscores (`congestion_control`, `udp_relay_mode`,
//     `disable_sni`), matching mihomo's URI parser at
//     `common/convert/converter.go:106-147`. Don't slip into kebab-case
//     here — that's the yaml shape, not the URI shape.
//   - dae's `allow_insecure=1` is emitted by Encode when SkipCertVerify
//     is true and honoured on Decode. Note that mihomo's built-in URI
//     parser strips this key — but v2raymg's own URI→clash pipeline
//     (DecodeTuic → tuicNodeToProxy) handles it correctly.
//   - TUIC v4 (token-based, userinfo without colon) is intentionally
//     unsupported; v2raymg targets v5 only for per-user attribution.
//   - HeartbeatInterval / ZeroRTTHandshake are mihomo client-only
//     outbound fields that have no representation in the URI spec; they
//     ride on SubscriptionSpec.Extensions for the clash path only and
//     are kept on the node struct so the in-memory exchange shape can
//     carry them.
type TuicNode struct {
	NodeName string
	Host     string
	Port     uint32
	UUID     string
	Password string

	SNI            string
	SkipCertVerify bool // Encode: emits allow_insecure=1; Decode: set from allow_insecure=1
	ALPN           []string

	CongestionControl string // mihomo URI key "congestion_control"; maps to listener/outbound `congestion-controller`
	UDPRelayMode      string // "" | "native" | "quic"
	DisableSNI        bool

	// Client-only knobs — not in URI; carried for Extensions pass-through.
	ZeroRTTHandshake  bool
	HeartbeatInterval string
}

func (n *TuicNode) Protocol() contracts.Protocol { return contracts.ProtocolTUIC }

func (n *TuicNode) Encode() string {
	q := url.Values{}
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if len(n.ALPN) > 0 {
		q.Set("alpn", strings.Join(n.ALPN, ","))
	}
	if n.CongestionControl != "" {
		q.Set("congestion_control", n.CongestionControl)
	}
	if n.UDPRelayMode != "" {
		q.Set("udp_relay_mode", n.UDPRelayMode)
	}
	if n.DisableSNI {
		q.Set("disable_sni", "1")
	}
	if n.SkipCertVerify {
		q.Set("allow_insecure", "1")
	}

	u := url.URL{
		Scheme:   "tuic",
		User:     url.UserPassword(n.UUID, n.Password),
		Host:     hostPort(n.Host, n.Port),
		RawQuery: q.Encode(),
		Fragment: n.NodeName,
	}
	return u.String()
}

func DecodeTuic(uri string) (*TuicNode, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("tuic: parse URI: %w", err)
	}
	if u.Scheme != "tuic" {
		return nil, fmt.Errorf("tuic: unexpected scheme %q", u.Scheme)
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	if port == 0 {
		return nil, fmt.Errorf("tuic: missing or invalid port")
	}
	if u.User == nil {
		return nil, fmt.Errorf("tuic: missing userinfo")
	}
	uuidStr := u.User.Username()
	password, hasPwd := u.User.Password()
	if !hasPwd {
		// userinfo without colon = v4 token form; v2raymg supports v5 only.
		return nil, fmt.Errorf("tuic: v4 token format not supported (use v5 uuid:password)")
	}
	if uuidStr == "" || password == "" {
		return nil, fmt.Errorf("tuic: empty uuid or password")
	}

	q := u.Query()
	// ALPN may arrive as repeated query keys (alpn=h3&alpn=h2) or as a single
	// comma-separated value (alpn=h3,h2); accept both.
	var alpn []string
	if vs := q["alpn"]; len(vs) > 1 {
		alpn = vs
	} else if raw := q.Get("alpn"); raw != "" {
		alpn = strings.Split(raw, ",")
	}
	n := &TuicNode{
		NodeName:          u.Fragment,
		Host:              u.Hostname(),
		Port:              uint32(port),
		UUID:              uuidStr,
		Password:          password,
		SNI:               q.Get("sni"),
		ALPN:              alpn,
		CongestionControl: q.Get("congestion_control"),
		UDPRelayMode:      q.Get("udp_relay_mode"),
	}
	if q.Get("disable_sni") == "1" {
		n.DisableSNI = true
	}
	if q.Get("allow_insecure") == "1" {
		// Honour dae-style insecure flag on Decode (NekoBox/Hiddify URIs may
		// emit it). Encode never emits this — see struct doc.
		n.SkipCertVerify = true
	}
	return n, nil
}
