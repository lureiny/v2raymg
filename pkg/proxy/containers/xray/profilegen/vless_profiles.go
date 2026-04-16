// Package profilegen provides standalone configuration generators for xray profiles.
// These functions are independent of container lifecycle and can be used to quickly
// generate VLESS configurations for various transport and security combinations.
package profilegen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"golang.org/x/crypto/curve25519"
)

// GenerateVLessParams defines parameters for generating a VLESS inbound spec.
type GenerateVLessParams struct {
	// Host is required - used for server_name in TLS/REALITY config and external access.
	Host string

	// Transport is the transport layer protocol.
	// Valid values: "tcp", "ws", "grpc", "httpupgrade", "xhttp".
	// Defaults to "tcp".
	// Note: "splithttp" is an alias for "xhttp" inside xray-core; use "xhttp" here.
	Transport string

	// Security is the security type.
	// Valid values: "tls", "reality".
	// Legacy "xtls" is accepted and auto-mapped to "tls" + flow="xtls-rprx-vision".
	// Defaults to "tls".
	Security string

	// Port is the listen port. If 0, a random valid port will be assigned.
	// Valid range: 100-65535.
	Port uint32

	// Tag is the inbound tag. If empty, auto-generated.
	Tag string

	// ListenAddr is the listen address. Defaults to "0.0.0.0".
	ListenAddr string

	// UUID is the VLESS user UUID. If empty, auto-generated.
	UUID string

	// Flow is the VLESS flow (e.g., "xtls-rprx-vision").
	// Automatically set for reality scenarios if not specified.
	Flow string

	// WSPath is the WebSocket path. Only used when Transport is "ws".
	// Defaults to "/ws".
	WSPath string

	// GRPCServiceName is the gRPC service name. Only used when Transport is "grpc".
	// Defaults to "grpc".
	GRPCServiceName string

	// HTTPUpgradePath is the HTTPUpgrade path. Only used when Transport is "httpupgrade".
	// Defaults to "/".
	HTTPUpgradePath string

	// HTTPUpgradeHost is the HTTPUpgrade host header. Only used when Transport is "httpupgrade".
	// Defaults to Host.
	HTTPUpgradeHost string

	// XHTTPMode is the XHTTP mode. Only used when Transport is "xhttp".
	// Valid values: "auto", "packet-up", "stream-one", "stream-up".
	XHTTPMode string

	// XHTTPPath is the XHTTP path. Only used when Transport is "xhttp".
	XHTTPPath string

	// XHTTPHost is the XHTTP host. Only used when Transport is "xhttp".
	XHTTPHost []string

	// Reality settings
	RealityServerNames []string
	RealityShortIDs    []string
	RealityTarget      string
	RealityPublicKey   string
	RealityPrivateKey  string
	RealityShortID     string

	// CertFile is the path to the TLS certificate file (.crt/.pem).
	// Required when security=tls. Ignored for security=reality.
	CertFile string

	// KeyFile is the path to the TLS private key file (.key/.pem).
	// Required when security=tls. Ignored for security=reality.
	KeyFile string

	// Sniffing settings
	SniffingEnabled      bool
	SniffingDestOverride []string
	SniffingRouteOnly    bool

	// TLS advanced settings
	TLSRejectUnknownSNI bool
	TLSMinVersion       string
	ALPN                []string
	OCSPStapling        int
}

// Validate checks required parameters.
func (p *GenerateVLessParams) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host is required")
	}

	// Normalize: splithttp is an internal xray alias for xhttp; always use xhttp.
	if p.Transport == "splithttp" {
		p.Transport = "xhttp"
	}

	// Validate transport
	validTransports := map[string]bool{
		"tcp":         true,
		"ws":          true,
		"grpc":        true,
		"httpupgrade": true,
		"xhttp":       true,
	}
	if p.Transport != "" && !validTransports[p.Transport] {
		return fmt.Errorf("invalid transport: %s", p.Transport)
	}

	// Handle legacy "xtls" → "tls" + flow mapping before normalization.
	// Xray-core has removed the xtls security type; the correct approach is
	// security=tls with flow=xtls-rprx-vision.
	if p.Security == "xtls" {
		if p.Flow == "" {
			p.Flow = "xtls-rprx-vision"
		}
		p.Security = "tls"
	}

	// Normalize security aliases
	p.Security = normalizeSecurity(p.Security)

	// Validate security
	validSecurities := map[string]bool{
		"tls":     true,
		"reality": true,
		"":        true,
	}
	if p.Security != "" && !validSecurities[p.Security] {
		return fmt.Errorf("invalid security: %s", p.Security)
	}

	// Validate port range if specified
	if p.Port != 0 && (p.Port < 100 || p.Port > 65535) {
		return fmt.Errorf("port %d out of range [100, 65535]", p.Port)
	}

	// Validate reality key pair: both or neither
	if p.Security == "reality" {
		hasPrivate := p.RealityPrivateKey != ""
		hasPublic := p.RealityPublicKey != ""
		if hasPrivate != hasPublic {
			return fmt.Errorf("reality_private_key and reality_public_key must be provided together")
		}
	}

	return nil
}

// normalizeSecurity normalizes security aliases.
func normalizeSecurity(s string) string {
	switch s {
	case "", "none":
		return ""
	default:
		return s
	}
}

// applyDefaults applies default values to unspecified parameters.
func (p *GenerateVLessParams) applyDefaults() {
	if p.Transport == "" {
		p.Transport = "tcp"
	}

	// Normalize security
	p.Security = normalizeSecurity(p.Security)
	if p.Security == "" {
		p.Security = "tls"
	}

	// Apply listen address default
	if p.ListenAddr == "" {
		p.ListenAddr = "0.0.0.0"
	}

	// Apply transport-specific defaults
	switch p.Transport {
	case "ws":
		if p.WSPath == "" {
			p.WSPath = "/ws"
		}
	case "grpc":
		if p.GRPCServiceName == "" {
			p.GRPCServiceName = "grpc"
		}
	case "httpupgrade":
		if p.HTTPUpgradePath == "" {
			p.HTTPUpgradePath = "/"
		}
		if p.HTTPUpgradeHost == "" {
			p.HTTPUpgradeHost = p.Host
		}
	case "xhttp":
		if p.XHTTPMode == "" {
			p.XHTTPMode = "auto"
		}
		if p.XHTTPPath == "" {
			p.XHTTPPath = "/"
		}
		if len(p.XHTTPHost) == 0 {
			p.XHTTPHost = []string{p.Host}
		}
	}

	// Auto-set flow for reality (only TCP/WS need flow; gRPC/xhttp must be empty)
	// Note: "xtls" security is already mapped to "tls" + flow in Validate(),
	// so we only handle reality here.
	if p.Flow == "" && p.Security == "reality" {
		needsEmptyFlow := p.Transport == "grpc" || p.Transport == "xhttp"
		if !needsEmptyFlow {
			p.Flow = "xtls-rprx-vision"
		}
	}

	// Apply reality defaults
	if p.Security == "reality" {
		if len(p.RealityServerNames) == 0 {
			p.RealityServerNames = []string{p.Host}
		}
		if p.RealityTarget == "" {
			p.RealityTarget = p.Host + ":443"
		}
		// Do NOT fill a default shortId here — leave RealityShortIDs empty so the
		// adapter layer (buildStreamSettings) generates a cryptographically random one.
		// Validate: both or neither - reality_private_key and reality_public_key must be provided together
		hasPrivate := p.RealityPrivateKey != ""
		hasPublic := p.RealityPublicKey != ""
		if hasPrivate != hasPublic {
			// Cannot provide only one - return empty to trigger validation error
			p.RealityPrivateKey = ""
		} else if !hasPrivate && !hasPublic {
			// Auto-generate key pair if not provided
			privateKey, publicKey, err := GenerateRealityKeyPairWithPublic()
			if err != nil {
				p.RealityPrivateKey = ""
				p.RealityPublicKey = ""
			} else {
				p.RealityPrivateKey = privateKey
				p.RealityPublicKey = publicKey
			}
		}
	}
}

// generateRandomBase64Key generates a random 32-byte key encoded in base64.
func generateRandomBase64Key() string {
	key := make([]byte, 32)
	rand.Read(key)
	return base64.RawURLEncoding.EncodeToString(key)
}

// GenerateRealityKeyPairWithPublic generates a random X25519 key pair for Reality.
// Returns (privateKey, publicKey) as base64url-encoded strings (no padding).
// This is the canonical implementation shared by both profilegen and the xray adapter.
func GenerateRealityKeyPairWithPublic() (string, string, error) {
	// Generate a random 32-byte scalar for X25519
	privateKey := [32]byte{}
	if _, err := rand.Read(privateKey[:]); err != nil {
		return "", "", fmt.Errorf("failed to generate random scalar: %w", err)
	}

	// Apply X25519 clamping
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	// Derive public key from private key using curve25519
	var publicKey [32]byte
	curve25519.ScalarBaseMult(&publicKey, &privateKey)

	privateKeyB64 := base64.RawURLEncoding.EncodeToString(privateKey[:])
	publicKeyB64 := base64.RawURLEncoding.EncodeToString(publicKey[:])

	return privateKeyB64, publicKeyB64, nil
}

// GenerateVLessInboundSpec generates a VLESS inbound spec based on params.
// This is a standalone function that supports multiple transport + security combinations.
func GenerateVLessInboundSpec(p GenerateVLessParams) (contracts.InboundSpec, error) {
	// Validate required parameters
	if err := p.Validate(); err != nil {
		return contracts.InboundSpec{}, err
	}

	// Apply defaults
	p.applyDefaults()

	// Generate random port if not specified
	port := p.Port
	if port == 0 {
		var err error
		port, err = generateRandomPort()
		if err != nil {
			return contracts.InboundSpec{}, fmt.Errorf("failed to generate random port: %w", err)
		}
	}

	// Generate UUID if not specified
	uuidStr := p.UUID
	if uuidStr == "" {
		uuidStr = uuid.New().String()
	}

	// Generate tag if not specified
	tag := p.Tag
	if tag == "" {
		security := p.Security
		if security == "" {
			security = "none"
		}
		tag = fmt.Sprintf("vless-%s-%s-%s-%d", p.Transport, security, p.Host, randInt64()%10000)
	}

	// Create default user config map for inbound_adapter
	defaultUser := map[string]any{
		"email": "auto@vless.local",
		"uuid":  uuidStr,
	}
	if p.Flow != "" {
		defaultUser["flow"] = p.Flow
	}

	// Build extensions for xray adapter
	extensions := map[string]any{
		"transport": p.Transport,
		"security":  p.Security,
	}

	// Add flow to extensions so subscription URI can include it.
	// (defaultUser also carries flow for the native xray config.)
	if p.Flow != "" {
		extensions["flow"] = p.Flow
	}

	// Add server_name for TLS/REALITY
	if p.Security == "tls" || p.Security == "reality" {
		extensions["server_name"] = p.Host
	}

	// Add transport-specific settings
	switch p.Transport {
	case "ws":
		if p.WSPath != "" {
			extensions["ws_path"] = p.WSPath
		}
	case "grpc":
		if p.GRPCServiceName != "" {
			extensions["grpc_service_name"] = p.GRPCServiceName
		}
	case "httpupgrade":
		if p.HTTPUpgradePath != "" {
			extensions["httpupgrade_path"] = p.HTTPUpgradePath
		}
		if p.HTTPUpgradeHost != "" {
			extensions["httpupgrade_host"] = p.HTTPUpgradeHost
		}
	case "xhttp":
		if p.XHTTPMode != "" {
			extensions["xhttp_mode"] = p.XHTTPMode
		}
		if p.XHTTPPath != "" {
			extensions["xhttp_path"] = p.XHTTPPath
		}
		if len(p.XHTTPHost) > 0 {
			extensions["xhttp_host"] = p.XHTTPHost
		}
	}

	// Add listen address if not default
	if p.ListenAddr != "0.0.0.0" {
		extensions["listen_addr"] = p.ListenAddr
	}

	// Add certificate file paths if provided (not used for reality)
	if p.Security != "reality" && p.CertFile != "" && p.KeyFile != "" {
		extensions["cert_file"] = p.CertFile
		extensions["key_file"] = p.KeyFile
	}

	// Add TLS advanced settings
	if p.Security == "tls" {
		if p.TLSRejectUnknownSNI {
			extensions["tls_reject_unknown_sni"] = true
		}
		if p.TLSMinVersion != "" {
			extensions["tls_min_version"] = p.TLSMinVersion
		}
		if len(p.ALPN) > 0 {
			extensions["alpn"] = p.ALPN
		}
		if p.OCSPStapling > 0 {
			extensions["ocsp_stapling"] = p.OCSPStapling
		}
	}

	// Add sniffing settings
	if p.SniffingEnabled {
		extensions["sniffing_enabled"] = true
		if len(p.SniffingDestOverride) > 0 {
			extensions["sniffing_dest_override"] = p.SniffingDestOverride
		}
		if p.SniffingRouteOnly {
			extensions["sniffing_route_only"] = true
		}
	}

	// Add Reality settings
	if p.Security == "reality" {
		if len(p.RealityServerNames) > 0 {
			extensions["reality_server_names"] = p.RealityServerNames
		}
		if p.RealityTarget != "" {
			extensions["reality_target"] = p.RealityTarget
		}
		if len(p.RealityShortIDs) > 0 {
			extensions["reality_short_ids"] = p.RealityShortIDs
		}
		if p.RealityShortID != "" {
			extensions["reality_short_id"] = p.RealityShortID
		}
		if p.RealityPublicKey != "" {
			extensions["reality_public_key"] = p.RealityPublicKey
		}
		if p.RealityPrivateKey != "" {
			extensions["reality_private_key"] = p.RealityPrivateKey
		}
	}

	spec := contracts.InboundSpec{
		Tag:        tag,
		Port:       port,
		Protocol:   contracts.ProtocolVLess,
		Extensions: extensions,
	}

	// Attach user to spec via special extension key
	spec.Extensions["users"] = []map[string]any{defaultUser}

	return spec, nil
}
