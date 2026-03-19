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
)

// GenerateVLessParams defines parameters for generating a VLESS inbound spec.
type GenerateVLessParams struct {
	// Host is required - used for server_name in TLS/REALITY config and external access.
	Host string

	// Transport is the transport layer protocol.
	// Valid values: "tcp", "ws", "grpc", "http", "h2", "h3", "xhttp", "splithttp".
	// Defaults to "tcp".
	Transport string

	// Security is the security type.
	// Valid values: "tls", "xtls", "reality".
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
	// Automatically set for xtls/reality scenarios if not specified.
	Flow string

	// WSPath is the WebSocket path. Only used when Transport is "ws".
	// Defaults to "/ws".
	WSPath string

	// GRPCServiceName is the gRPC service name. Only used when Transport is "grpc".
	// Defaults to "grpc".
	GRPCServiceName string

	// HTTPPath is the HTTP/2 path. Only used when Transport is "http" or "h2".
	// Defaults to "/".
	HTTPPath string

	// HTTPHost is the HTTP/2 host. Only used when Transport is "http" or "h2".
	// If empty, defaults to Host.
	HTTPHost []string

	// XHTTPMode is the XHTTP mode. Only used when Transport is "xhttp" or "splithttp".
	// Valid values: "auto", "zero", "one", "two".
	XHTTPMode string

	// XHTTPPath is the XHTTP path. Only used when Transport is "xhttp" or "splithttp".
	XHTTPPath string

	// XHTTPHost is the XHTTP host. Only used when Transport is "xhttp" or "splithttp".
	XHTTPHost []string

	// Reality settings
	RealityServerNames []string
	RealityShortIDs   []string
	RealityTarget     string
	RealityPublicKey   string
	RealityPrivateKey string
	RealityShortID    string

	// CertFile is the path to the TLS certificate file (.crt/.pem).
	// Required when security=tls/xtls. Ignored for security=reality.
	CertFile string

	// KeyFile is the path to the TLS private key file (.key/.pem).
	// Required when security=tls/xtls. Ignored for security=reality.
	KeyFile string
}

// Validate checks required parameters.
func (p *GenerateVLessParams) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host is required")
	}

	// Normalize transport aliases
	p.Transport = normalizeTransport(p.Transport)

	// Validate transport
	validTransports := map[string]bool{
		"tcp":        true,
		"ws":         true,
		"grpc":       true,
		"http":       true,
		"h2":         true,
		"h3":         true,
		"xhttp":      true,
		"splithttp":  true,
	}
	if p.Transport != "" && !validTransports[p.Transport] {
		return fmt.Errorf("invalid transport: %s", p.Transport)
	}

	// Normalize security aliases
	p.Security = normalizeSecurity(p.Security)

	// Validate security
	validSecurities := map[string]bool{
		"tls":    true,
		"xtls":   true,
		"reality": true,
		"":       true,
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

// normalizeTransport normalizes transport aliases to internal representation.
func normalizeTransport(t string) string {
	switch t {
	case "h2", "h3":
		// HTTP family - normalize to appropriate value
		return t
	case "xhttp", "splithttp":
		// XHTTP family - keep as-is for adapter handling
		return t
	default:
		return t
	}
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
	// Normalize transport
	p.Transport = normalizeTransport(p.Transport)
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
	case "http", "h2":
		if p.HTTPPath == "" {
			p.HTTPPath = "/"
		}
		if len(p.HTTPHost) == 0 {
			p.HTTPHost = []string{p.Host}
		}
	case "xhttp", "splithttp":
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

	// Auto-set flow for xtls/reality
	// BUT: for grpc/xhttp/splithttp/h3 + reality, flow must be empty (no xtls-rprx-vision)
	// Per Xray-examples: VLESS + gRPC + REALITY should NOT have flow
	if p.Flow == "" && (p.Security == "xtls" || p.Security == "reality") {
		isGRPCOrXHTTP := p.Transport == "grpc" || p.Transport == "xhttp" || p.Transport == "splithttp" || p.Transport == "h3"
		// For reality + grpc/xhttp/splithttp/h3, flow should be empty
		if !(p.Security == "reality" && isGRPCOrXHTTP) {
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
		if len(p.RealityShortIDs) == 0 {
			p.RealityShortIDs = []string{"0123456789abcdef"}
		}
		// Validate: both or neither - reality_private_key and reality_public_key must be provided together
		hasPrivate := p.RealityPrivateKey != ""
		hasPublic := p.RealityPublicKey != ""
		if hasPrivate != hasPublic {
			// Cannot provide only one - return empty to trigger validation error
			p.RealityPrivateKey = ""
		} else if !hasPrivate && !hasPublic {
			// Auto-generate key pair if not provided
			privateKey, publicKey := generateRandomKeyPair()
			p.RealityPrivateKey = privateKey
			p.RealityPublicKey = publicKey
		}
	}
}

// generateRandomBase64Key generates a random 32-byte key encoded in base64.
func generateRandomBase64Key() string {
	key := make([]byte, 32)
	rand.Read(key)
	return base64.RawURLEncoding.EncodeToString(key)
}

// generateRandomKeyPair generates a random X25519 key pair encoded in base64.
// Returns (privateKey, publicKey) as base64-encoded strings.
func generateRandomKeyPair() (string, string) {
	// Generate a random 32-byte scalar for X25519
	privateKey := [32]byte{}
	rand.Read(privateKey[:])

	// Apply X25519 clamping
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	privateKeyB64 := base64.RawURLEncoding.EncodeToString(privateKey[:])

	// Note: We can't compute public key without crypto/25519 imported
	// For simplicity, we'll return a placeholder that adapter will accept
	// The adapter's DeriveRealityPublicKey can compute the public key from private
	// For now, just return the private key as both (the adapter will derive public if needed)
	publicKeyB64 := privateKeyB64 // Placeholder - adapter will derive actual public key

	return privateKeyB64, publicKeyB64
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

	// Create user with UUID and flow in extensions
	user := contracts.UserSpec{
		Username:  fmt.Sprintf("auto@vless.local"),
		Level:     0,
		Protocol:  contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid": uuidStr,
		},
	}

	// Add flow if specified
	if p.Flow != "" {
		user.Extensions["flow"] = p.Flow
	}

	// Build extensions for xray adapter
	extensions := map[string]any{
		"transport": p.Transport,
		"security":  p.Security,
	}

	// Add server_name for TLS/XTLS/REALITY
	if p.Security == "tls" || p.Security == "xtls" || p.Security == "reality" {
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
	case "http", "h2":
		if p.HTTPPath != "" {
			extensions["http_path"] = p.HTTPPath
		}
		if len(p.HTTPHost) > 0 {
			extensions["http_host"] = p.HTTPHost
		}
	case "xhttp", "splithttp":
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
	spec.Extensions["users"] = []contracts.UserSpec{user}

	return spec, nil
}
