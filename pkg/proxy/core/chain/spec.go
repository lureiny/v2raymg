// Package chain provides backend proxy URI parsing, safety validation,
// credential redaction, and outbound config rendering for server-side proxy
// chaining across mihomo and xray containers.
package chain

import "github.com/lureiny/v2raymg/pkg/proxy/core/contracts"

// OutboundSpec is the protocol-agnostic parsed representation of a backend
// proxy URI for server-side outbound chaining.
type OutboundSpec struct {
	Scheme   string             `json:"scheme"`
	Protocol contracts.Protocol `json:"protocol"`
	Host     string             `json:"host"` // domain name preserved verbatim for DDNS, or unbracketed IP literal
	Port     uint32             `json:"port"`
	Remark   string             `json:"remark,omitempty"`

	// Authentication & cipher credentials.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	UUID     string `json:"uuid,omitempty"`
	Method   string `json:"method,omitempty"` // shadowsocks cipher or vmess "scy"
	AlterID  int    `json:"alter_id,omitempty"`

	// VLESS-specific fields.
	Flow       string `json:"flow,omitempty"`
	Encryption string `json:"encryption,omitempty"` // "" or "none" or mlkem768...

	// Transport & security layer.
	Transport      string   `json:"transport,omitempty"` // "", "tcp", "ws", "grpc", "h2", "httpupgrade", "xhttp"
	HeaderType     string   `json:"header_type,omitempty"`
	Security       string   `json:"security,omitempty"` // "", "none", "tls", "reality", "xtls"
	TLS            bool     `json:"tls,omitempty"`
	SNI            string   `json:"sni,omitempty"`
	ALPN           []string `json:"alpn,omitempty"`
	Fingerprint    string   `json:"fingerprint,omitempty"` // uTLS fp or cert-pin hpkp
	SkipCertVerify bool     `json:"skip_cert_verify,omitempty"`

	// Reality parameters.
	RealityPublicKey string `json:"reality_public_key,omitempty"`
	RealityShortID   string `json:"reality_short_id,omitempty"`
	RealitySpiderX   string `json:"reality_spider_x,omitempty"`

	// WebSocket transport.
	WSPath string `json:"ws_path,omitempty"`
	WSHost string `json:"ws_host,omitempty"`

	// gRPC transport.
	GRPCServiceName string `json:"grpc_service_name,omitempty"`

	// HTTP/2 transport.
	H2Path string `json:"h2_path,omitempty"`
	H2Host string `json:"h2_host,omitempty"`

	// HTTPUpgrade transport.
	HTTPUpgradePath string `json:"httpupgrade_path,omitempty"`
	HTTPUpgradeHost string `json:"httpupgrade_host,omitempty"`

	// XHTTP / SplitHTTP transport.
	XHTTPPath string `json:"xhttp_path,omitempty"`
	XHTTPHost string `json:"xhttp_host,omitempty"`
	XHTTPMode string `json:"xhttp_mode,omitempty"`

	// Shadowsocks SIP003 plugin.
	Plugin     string            `json:"plugin,omitempty"`
	PluginOpts map[string]string `json:"plugin_opts,omitempty"`

	// Hysteria2 options.
	Obfs         string `json:"obfs,omitempty"`
	ObfsPassword string `json:"obfs_password,omitempty"`
	Ports        string `json:"ports,omitempty"` // port hopping range from ?mport=
	PinSHA256    string `json:"pin_sha256,omitempty"`

	// TUIC v5 options.
	CongestionControl string `json:"congestion_control,omitempty"`
	UDPRelayMode      string `json:"udp_relay_mode,omitempty"`
	DisableSNI        bool   `json:"disable_sni,omitempty"`
}
