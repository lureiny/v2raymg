// Package protocolparams is the typed, container-agnostic view of a
// FastAddInbound request. It sits one layer above pkg/proxy/core/params's
// defaults.go FillDefaults: FillDefaults operates on the raw map[string]any
// (filling credentials, materialising certs to disk), then Parse promotes
// the filled map into a strongly-typed *ProtocolParams.
//
// Design (docs/mihomo-protocol-expansion-plan.md D1):
//
//   - Exactly one of the protocol-specific *Params pointers is non-nil,
//     selected by the Protocol field. The rest stay nil and are ignored by
//     downstream adapters.
//   - TransportSpec is nil for protocols whose transport is fixed (hy2, tuic
//     and anytls are QUIC/TCP-native, with no pluggable transport).
//   - SecuritySpec is nil for protocols that ship without a TLS layer
//     (Shadowsocks plain mode is the only current case).
//   - All field names mirror the keys in keys.go verbatim — the parser reads
//     one key into one field, no renaming. That makes it possible to
//     round-trip a request through a debugger or log snapshot without
//     decoding a translation table.
//
// Container-agnostic means: mihomo is the first full consumer, but the
// package takes no mihomo-specific dependencies. xray/hysteria/snell can
// opt in later by having their adapters consume *ProtocolParams instead of
// raw map[string]any; their existing map-based path continues to work
// unchanged.
package protocolparams

import (
	"errors"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ProtocolParams is the structured form of the FastAddInbound request
// payload, with credentials and certificate paths already resolved by
// pkg/proxy/core/params.FillDefaults.
//
// JSON tags are snake_case — this struct is persisted to InboundStore via
// MihomoInbound's native JSON, and a future rename of any field without
// updating the tag would silently break deserialisation of existing
// records. TestProtocolParamsJSONTagSchema locks the on-disk shape.
type ProtocolParams struct {
	// Protocol is the authoritative dispatch key. Exactly the matching
	// *Params pointer below is non-nil.
	Protocol contracts.Protocol `json:"protocol"`

	// Tag is the inbound identifier chosen by the caller (stable across
	// config reloads). Empty-string is invalid.
	Tag string `json:"tag"`

	// Port is the listener's server-side port. The forward layer maps
	// per-user public ports onto this. 0 means "let the container choose",
	// which it does by drawing from the shared port authority
	// (container.ClaimInboundPort) — the same allocation table the forward
	// layer draws from, so the two can never be handed the same port.
	Port uint32 `json:"port"`

	// ListenAddr defaults to "127.0.0.1" when unset — external ingress is
	// always the forward layer's responsibility (see
	// docs/container-design-principles.md principle 3). Non-loopback
	// addresses are accepted but warn-logged by each adapter.
	ListenAddr string `json:"listen_addr,omitempty"`

	// Transport carries the pluggable transport layer (WS/gRPC/xHTTP/...)
	// for protocols that support one. nil for Hysteria2/TUIC/AnyTLS which
	// are transport-fixed at the protocol level.
	Transport *TransportSpec `json:"transport,omitempty"`

	// Security carries the TLS/Reality layer. nil means plain. Protocols
	// that mandate TLS (Trojan/Hysteria2/TUIC/AnyTLS) reject nil here.
	Security *SecuritySpec `json:"security,omitempty"`

	// Exactly one of the below is non-nil, determined by Protocol.
	VLESS     *VLESSParams     `json:"vless,omitempty"`
	VMess     *VMessParams     `json:"vmess,omitempty"`
	Trojan    *TrojanParams    `json:"trojan,omitempty"`
	SS        *SSParams        `json:"ss,omitempty"`
	Hysteria2 *Hysteria2Params `json:"hysteria2,omitempty"`
	TUIC      *TUICParams      `json:"tuic,omitempty"`
	AnyTLS    *AnyTLSParams    `json:"anytls,omitempty"`
}

// TransportSpec describes a pluggable transport layer for vless/vmess/trojan.
//
// Only the fields relevant to Kind are populated; others stay zero. Adapters
// should read fields based on Kind rather than guessing from non-zero checks
// — a caller-supplied "ws_path" on Kind=tcp is a caller error, not a
// silent promotion to ws.
type TransportSpec struct {
	Kind contracts.Transport `json:"kind"` // tcp | ws | grpc | httpupgrade | xhttp | splithttp | h2

	// ws
	WSPath string `json:"ws_path,omitempty"`
	WSHost string `json:"ws_host,omitempty"`

	// grpc
	GRPCServiceName string `json:"grpc_service_name,omitempty"`

	// httpupgrade
	HTTPUpgradePath string `json:"httpupgrade_path,omitempty"`
	HTTPUpgradeHost string `json:"httpupgrade_host,omitempty"`

	// xhttp / splithttp (share the same field set; Kind discriminates)
	XHTTPPath string `json:"xhttp_path,omitempty"`
	XHTTPHost string `json:"xhttp_host,omitempty"`
	XHTTPMode string `json:"xhttp_mode,omitempty"`

	// h2 (HTTP/2 inbound, used by vmess)
	HTTPPath string   `json:"http_path,omitempty"`
	HTTPHost []string `json:"http_host,omitempty"`
}

// SecuritySpec describes the TLS / Reality layer on top of the transport.
// Kind discriminates; TLS is non-nil iff Kind=tls, Reality iff Kind=reality.
type SecuritySpec struct {
	Kind    contracts.Security `json:"kind"` // none | tls | reality
	TLS     *TLSSpec           `json:"tls,omitempty"`
	Reality *RealitySpec       `json:"reality,omitempty"`
}

// TLSSpec carries ordinary-TLS server parameters. CertFile/KeyFile are
// always absolute paths — FillDefaults materialises PEM content and
// resolves domain/self_signed sources into files before Parse runs.
//
// CertSource records where those files came from so the inbound-removal
// path knows whether it may delete them ("pem" / "self_signed" — yes;
// "file" / "domain" — no).
type TLSSpec struct {
	SNI  string   `json:"sni,omitempty"`
	ALPN []string `json:"alpn,omitempty"`

	CertFile   string `json:"cert_file,omitempty"`
	KeyFile    string `json:"key_file,omitempty"`
	CertSource string `json:"cert_source,omitempty"` // file | pem | domain | self_signed

	MinVersion       string `json:"min_version,omitempty"`
	RejectUnknownSNI bool   `json:"reject_unknown_sni,omitempty"`
	UTLSFingerprint  string `json:"utls_fingerprint,omitempty"`

	// SkipCertVerify is a subscription-side override. When true, the
	// client config emits skip-cert-verify even if CertSource is not
	// self_signed. Useful for non-standard CAs where the operator
	// accepts the reduced security. Default false — real-CA certs go
	// through normal validation.
	SkipCertVerify bool `json:"skip_cert_verify,omitempty"`
}

// RealitySpec carries the Reality handshake parameters. PrivateKey and
// PublicKey are the x25519 server keypair; ServerNames is the list of SNIs
// the listener will accept (echoed to matching clients).
type RealitySpec struct {
	Target       string   `json:"target"` // e.g. "www.microsoft.com:443"
	ServerNames  []string `json:"server_names"`
	ShortIDs     []string `json:"short_ids,omitempty"`
	PrivateKey   string   `json:"private_key,omitempty"`
	PublicKey    string   `json:"public_key,omitempty"`
	MinClientVer string   `json:"min_client_ver,omitempty"`
	MaxTimeDiff  string   `json:"max_time_diff,omitempty"`
}

// ---- Per-protocol Params ----

type VLESSParams struct {
	UUID       string `json:"uuid"`
	Flow       string `json:"flow,omitempty"`       // xtls-rprx-vision for Reality, or empty
	Decryption string `json:"decryption,omitempty"` // canonical "none"; kept for future ML-KEM decryption values
}

type VMessParams struct {
	UUID    string `json:"uuid"`
	AlterID int    `json:"alter_id,omitempty"` // legacy; default 0 for AEAD-only
	// Cipher is caller-supplied only; xray and mihomo VMess listeners
	// have no cipher field, the URI carries no cipher, and the clash
	// converter hardcodes "auto" client-side. parseVMess does NOT fill
	// a default — keep this field empty unless the caller explicitly
	// supplied it (xray-parity).
	Cipher string `json:"cipher,omitempty"`
}

type TrojanParams struct {
	Password string `json:"password"`
	// TLS/Reality material lives on the parent SecuritySpec; Trojan
	// always requires one or the other at the mihomo listener level.
}

type SSParams struct {
	Password   string            `json:"password"`
	Cipher     string            `json:"cipher"`
	UDP        bool              `json:"udp,omitempty"`
	Plugin     string            `json:"plugin,omitempty"`      // shadow-tls | obfs-local | v2ray-plugin (kcp-tun out of scope)
	PluginOpts map[string]string `json:"plugin_opts,omitempty"` // plugin-specific key/value pairs
}

type Hysteria2Params struct {
	Password              string `json:"password"`
	Obfs                  string `json:"obfs,omitempty"` // "salamander" currently
	ObfsPassword          string `json:"obfs_password,omitempty"`
	Up                    string `json:"up,omitempty"`         // e.g. "50 Mbps"
	Down                  string `json:"down,omitempty"`       // e.g. "100 Mbps"
	Masquerade            string `json:"masquerade,omitempty"` // URL / "file:/path" / "proxy:..." (server-side masquerade)
	IgnoreClientBandwidth bool   `json:"ignore_client_bandwidth,omitempty"`
}

type TUICParams struct {
	UUID                 string `json:"uuid"`
	Password             string `json:"password"`
	CongestionController string `json:"congestion_controller,omitempty"` // "bbr" | "cubic" | "new_reno"
	UDPRelayMode         string `json:"udp_relay_mode,omitempty"`        // "native" | "quic"
	ZeroRTTHandshake     bool   `json:"zero_rtt_handshake,omitempty"`    // client-only → reduce-rtt
	HeartbeatInterval    string `json:"heartbeat_interval,omitempty"`    // duration string, e.g. "10s"
	DisableSNI           bool   `json:"disable_sni,omitempty"`           // client-only; mihomo outbound disable-sni
}

// AnyTLSParams carries the AnyTLS-specific knobs.
//
// Schema fact-check (mihomo Alpha):
//   - Listener (`listener/inbound/anytls.go:12-21`) consumes only
//     password (via users map) + padding-scheme; the idle-session and
//     min-idle-session fields exist solely on the outbound side
//     (`adapter/outbound/anytls.go:25-43`).
//   - Outbound idle-session-check-interval / idle-session-timeout are
//     `int` seconds (NOT a duration string), so the JSON tag carries the
//     `_seconds` suffix to make the unit explicit on the wire.
//   - PaddingScheme is server-only inline text; empty defers to mihomo's
//     `transport/anytls/padding.DefaultPaddingScheme`.
type AnyTLSParams struct {
	Password                 string `json:"password"`
	PaddingScheme            string `json:"padding_scheme,omitempty"`
	IdleSessionCheckInterval int    `json:"idle_session_check_interval_seconds,omitempty"` // client-only; ≤5 → mihomo fallback to 30s
	IdleSessionTimeout       int    `json:"idle_session_timeout_seconds,omitempty"`        // client-only; ≤5 → mihomo fallback to 30s
	MinIdleSession           int    `json:"min_idle_session,omitempty"`                    // client-only
}

// ---- Errors ----

// ErrProtocolNotSupported signals an unknown or not-yet-implemented
// protocol in Parse. Wrap with fmt.Errorf so callers (HTTP/RPC handlers)
// can map to a 400 status via errors.Is.
var ErrProtocolNotSupported = errors.New("protocolparams: protocol not supported")

// ErrMissingRequired signals a required field was empty or missing.
var ErrMissingRequired = errors.New("protocolparams: required field missing")

// ErrInvalidCombination signals a (Protocol, Transport, Security) triple
// that is individually valid but not allowed together (e.g. hy2 + ws).
var ErrInvalidCombination = errors.New("protocolparams: invalid combination")
