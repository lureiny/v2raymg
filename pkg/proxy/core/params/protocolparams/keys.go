package protocolparams

// Canonical string keys for the raw map[string]any that arrives from RPC
// FastAddInbound / HTTP fastAddInbound_handler. Every reader in the
// parser and every adapter that still peeks at the raw map MUST pull
// from this file rather than retyping a literal. Drift between this file
// and pkg/http/fastAddInbound_handler.go's convenience-field keys is a
// caller-to-parser contract bug.
//
// These keys are deliberately the lowercase/snake_case form used by the
// HTTP API. mihomo's own `inbound:"..."` tags use kebab-case (e.g.
// "ws-path") — adapters translate when emitting yaml. Keep the two
// naming systems separated so one API revision doesn't ripple through
// mihomo's yaml schema.
const (
	// Top-level (all protocols)
	KeyProtocol   = "protocol"
	KeyPort       = "port"
	KeyListenAddr = "listen_addr"
	KeyTag        = "tag"
	KeyTransport  = "transport"
	KeySecurity   = "security"

	// Credentials (credential presence is protocol-dependent)
	KeyUUID     = "uuid"
	KeyPassword = "password"
	KeyCipher   = "cipher"
	KeyAlterID  = "alter_id"

	// VLESS-specific
	KeyFlow       = "flow"
	KeyDecryption = "decryption"

	// Transport: ws
	KeyWSPath = "ws_path"
	KeyWSHost = "ws_host"

	// Transport: grpc
	KeyGRPCServiceName = "grpc_service_name"

	// Transport: httpupgrade
	KeyHTTPUpgradePath = "httpupgrade_path"
	KeyHTTPUpgradeHost = "httpupgrade_host"

	// Transport: xhttp / splithttp
	KeyXHTTPPath = "xhttp_path"
	KeyXHTTPHost = "xhttp_host"
	KeyXHTTPMode = "xhttp_mode"

	// Transport: h2
	KeyHTTPPath = "http_path"
	KeyHTTPHost = "http_host" // comma-separated list on the wire; array after Parse

	// TLS
	KeySNI                 = "sni"
	KeyALPN                = "alpn" // comma-separated list on the wire; array after Parse
	KeyCertFile            = "cert_file"
	KeyKeyFile             = "key_file"
	KeyCertSource          = "cert_source"
	KeyCertificatePEM      = "certificate"
	KeyKeyPEM              = "key"
	KeyDomain              = "domain"
	KeySelfSigned          = "self_signed"
	KeyServerName          = "server_name"
	KeyTLSMinVersion       = "tls_min_version"
	KeyTLSRejectUnknownSNI = "tls_reject_unknown_sni"
	KeyUTLSFingerprint     = "utls_fingerprint"
	// KeySkipCertVerify is a subscription-side hint: when true, the
	// generated client config sets skip-cert-verify regardless of
	// cert_source. Used by callers who want to ship a real-CA cert
	// whose chain the client can't (or shouldn't) validate.
	KeySkipCertVerify = "skip_cert_verify"

	// Reality
	KeyRealityTarget       = "reality_target"
	KeyRealityServerNames  = "reality_server_names"
	KeyRealityShortIDs     = "reality_short_ids"
	KeyRealityPrivateKey   = "reality_private_key"
	KeyRealityPublicKey    = "reality_public_key"
	KeyRealityMinClientVer = "reality_min_client_ver"
	KeyRealityMaxTimeDiff  = "reality_max_time_diff"

	// Shadowsocks
	KeyUDP        = "udp"
	KeyPlugin     = "plugin"
	KeyPluginOpts = "plugin_opts" // reserved for raw SIP003 string; individual opts use the keys below

	// Shadowsocks plugin opts (flat keys, no prefix)
	KeyPluginMode     = "plugin_mode"     // obfs-local: "http"|"tls"; v2ray-plugin: "websocket"|"quic"
	KeyPluginHost     = "plugin_host"     // obfs-local/shadow-tls target host
	KeyPluginPath     = "plugin_path"     // v2ray-plugin path
	KeyPluginTLS      = "plugin_tls"      // v2ray-plugin TLS bool
	KeyPluginPassword = "plugin_password" // shadow-tls password
	KeyPluginVersion  = "plugin_version"  // shadow-tls version ("2" or "3")

	// Hysteria2
	KeyObfs                  = "obfs"
	KeyObfsPassword          = "obfs_password"
	KeyUp                    = "up"
	KeyDown                  = "down"
	KeyMasquerade            = "masquerade"
	KeyIgnoreClientBandwidth = "ignore_client_bandwidth"

	// TUIC
	KeyCongestionController = "congestion_controller"
	KeyUDPRelayMode         = "udp_relay_mode"
	KeyZeroRTTHandshake     = "zero_rtt_handshake"
	KeyHeartbeatInterval    = "heartbeat_interval"
	KeyDisableSNI           = "disable_sni"

	// AnyTLS
	KeyPaddingScheme            = "padding_scheme"
	KeyIdleSessionCheckInterval = "idle_session_check_interval_seconds" // int seconds, mihomo outbound `idle-session-check-interval`
	KeyIdleSessionTimeout       = "idle_session_timeout_seconds"        // int seconds, mihomo outbound `idle-session-timeout`
	KeyMinIdleSession           = "min_idle_session"
)
