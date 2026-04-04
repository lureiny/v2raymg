// Package profilegen provides standalone configuration generators for xray profiles.
// These functions are independent of container lifecycle and can be used to quickly
// generate Shadowsocks configurations.
package profilegen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Supported Shadowsocks methods
const (
	// 2022 methods (recommended)
	Method2022Blake3Aes256Gcm     = "2022-blake3-aes-256-gcm"
	Method2022Blake3Aes128Gcm     = "2022-blake3-aes-128-gcm"
	Method2022Blake3ChaCha20Poly1305 = "2022-blake3-chacha20-poly1305"

	// Legacy methods (for compatibility)
	MethodAes256Gcm        = "aes-256-gcm"
	MethodAes128Gcm        = "aes-128-gcm"
	MethodChaCha20Poly1305 = "chacha20-ietf-poly1305"
	MethodAes256Cfb        = "aes-256-cfb"
	MethodAes128Cfb        = "aes-128-cfb"
	MethodChaCha20Ietf     = "chacha20-ietf"
	MethodPlain            = "plain"
)

// DefaultShadowsocksMethod is the default encryption method (2022 series)
const DefaultShadowsocksMethod = Method2022Blake3Aes256Gcm

// GenerateShadowsocksParams defines parameters for generating a Shadowsocks inbound spec.
// Only Host is required; other fields will be auto-generated if not specified.
type GenerateShadowsocksParams struct {
	// Host is required - used for server_name in TLS config (if enabled) and external access.
	Host string

	// Transport is the transport layer protocol. Valid values: "tcp", "grpc", "ws".
	// Defaults to "tcp".
	Transport string

	// Port is the listen port. If 0, a random valid port will be assigned.
	// Valid range: 100-65535.
	Port uint32

	// Tag is the inbound tag. If empty, auto-generated like "ss-tcp-<random>".
	Tag string

	// ListenAddr is the listen address. Defaults to "0.0.0.0".
	ListenAddr string

	// Method is the encryption method.
	// If empty, defaults to "2022-blake3-aes-256-gcm" (recommended).
	// Supported 2022 methods: "2022-blake3-aes-256-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-chacha20-poly1305"
	// Supported legacy methods: "aes-256-gcm", "aes-128-gcm", "chacha20-ietf-poly1305", etc.
	Method string

	// Password is the Shadowsocks password. If empty, auto-generated.
	// For 2022 methods: 16 or 32 bytes recommended.
	Password string

	// Security is the TLS/XTLS/Reality security mode. Valid values: "none", "tls", "reality".
	// Defaults to "none".
	Security string

	// GRPCServiceName is the gRPC service name. Only used when Transport is "grpc".
	// Defaults to "grpc".
	GRPCServiceName string

	// WSPath is the WebSocket path. Only used when Transport is "ws".
	WSPath string
}

// Validate checks required parameters.
func (p *GenerateShadowsocksParams) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host is required")
	}

	// Validate transport
	if p.Transport != "" && p.Transport != "tcp" && p.Transport != "grpc" && p.Transport != "ws" {
		return fmt.Errorf("invalid transport: %s (must be 'tcp', 'grpc' or 'ws')", p.Transport)
	}

	// Validate security
	if p.Security != "" && p.Security != "none" && p.Security != "tls" && p.Security != "reality" {
		return fmt.Errorf("invalid security: %s (must be 'none', 'tls' or 'reality')", p.Security)
	}

	// Validate port range if specified
	if p.Port != 0 && (p.Port < 100 || p.Port > 65535) {
		return fmt.Errorf("port %d out of range [100, 65535]", p.Port)
	}

	// Validate method if specified
	if p.Method != "" && !isValidShadowsocksMethod(p.Method) {
		return fmt.Errorf("invalid encryption method: %s", p.Method)
	}

	// Validate 2022 password if provided
	if p.Password != "" && is2022Method(p.Method) {
		if err := validate2022Password(p.Method, p.Password); err != nil {
			return err
		}
	}

	return nil
}

// isValidShadowsocksMethod checks if the given method is a valid Shadowsocks encryption method.
func isValidShadowsocksMethod(method string) bool {
	switch method {
	case
		Method2022Blake3Aes256Gcm,
		Method2022Blake3Aes128Gcm,
		Method2022Blake3ChaCha20Poly1305,
		MethodAes256Gcm,
		MethodAes128Gcm,
		MethodChaCha20Poly1305,
		MethodAes256Cfb,
		MethodAes128Cfb,
		MethodChaCha20Ietf,
		MethodPlain:
		return true
	}
	return false
}

// applyDefaults applies default values to unspecified parameters.
func (p *GenerateShadowsocksParams) applyDefaults() {
	if p.Transport == "" {
		p.Transport = "tcp"
	}
	if p.Security == "" {
		p.Security = "none"
	}
	if p.ListenAddr == "" {
		p.ListenAddr = "0.0.0.0"
	}
	if p.Method == "" {
		p.Method = DefaultShadowsocksMethod
	}
	if p.GRPCServiceName == "" && p.Transport == "grpc" {
		p.GRPCServiceName = "grpc"
	}
	if p.WSPath == "" && p.Transport == "ws" {
		p.WSPath = "/"
	}
}

// get2022KeyLength returns the required key length for 2022 methods.
func get2022KeyLength(method string) int {
	switch method {
	case Method2022Blake3Aes128Gcm:
		return 16
	case Method2022Blake3Aes256Gcm, Method2022Blake3ChaCha20Poly1305:
		return 32
	default:
		return 0
	}
}

// is2022Method checks if the given method is a 2022 method.
func is2022Method(method string) bool {
	return len(method) >= 4 && method[:4] == "2022"
}

// generateRandomSSPassword generates a random Shadowsocks password.
// For 2022 methods, we generate raw bytes of appropriate length and encode to base64 (no truncation).
// For legacy methods, we generate a base64 string.
func generateRandomSSPassword(method string) string {
	// For 2022 methods, use raw bytes of correct length
	if is2022Method(method) {
		length := get2022KeyLength(method)
		if length == 0 {
			length = 32 // fallback
		}
		b := make([]byte, length)
		rand.Read(b)
		return base64.StdEncoding.EncodeToString(b) // full base64, no truncation
	}
	// For legacy methods, generate a simpler password
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// validate2022Password validates that the given password is valid for the 2022 method.
// It tries to base64 decode the password and checks if the decoded length matches the method requirement.
func validate2022Password(method, password string) error {
	keyLen := get2022KeyLength(method)
	if keyLen == 0 {
		return nil // not a 2022 method, skip validation
	}

	// Try to decode as base64
	decoded, err := base64.StdEncoding.DecodeString(password)
	if err != nil {
		return fmt.Errorf("invalid 2022 password: not valid base64 (method %s requires %d bytes key)", method, keyLen)
	}

	if len(decoded) != keyLen {
		return fmt.Errorf("invalid 2022 password: key length %d bytes (method %s requires %d bytes)", len(decoded), method, keyLen)
	}

	return nil
}

// GenerateShadowsocksInboundSpec generates a Shadowsocks inbound spec.
// This is a standalone function that does not depend on container lifecycle.
//
// The returned InboundSpec can be passed directly to xray.Adapter.ToProvider()
// to generate the native xray JSON config, then used with container's
// AddInboundConfig or FastAddInbound flow.
func GenerateShadowsocksInboundSpec(p GenerateShadowsocksParams) (contracts.InboundSpec, error) {
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

	// Generate password if not specified
	password := p.Password
	if password == "" {
		password = generateRandomSSPassword(p.Method)
	}

	// Generate tag if not specified
	tag := p.Tag
	if tag == "" {
		tag = fmt.Sprintf("ss-%s-%d", p.Transport, randInt64()%10000)
	}

	// Create default user config map for inbound_adapter
	defaultUser := map[string]any{
		"email":    "auto@ss.local",
		"password": password,
		"method":   p.Method,
	}

	// Build extensions for xray adapter
	extensions := map[string]any{
		"transport": p.Transport,
		"security":  p.Security,
	}

	// Add method to extensions (required for SS adapter)
	extensions["method"] = p.Method

	// Add gRPC service name if transport is grpc
	if p.Transport == "grpc" && p.GRPCServiceName != "" {
		extensions["grpc_service_name"] = p.GRPCServiceName
	}

	// Add WebSocket path if transport is ws
	if p.Transport == "ws" && p.WSPath != "" {
		extensions["ws_path"] = p.WSPath
	}

	// Add listen address if not default
	if p.ListenAddr != "0.0.0.0" {
		extensions["listen_addr"] = p.ListenAddr
	}

	spec := contracts.InboundSpec{
		Tag:        tag,
		Port:       port,
		Protocol:   contracts.ProtocolShadowsocks,
		Extensions: extensions,
	}

	// Attach user to spec via special extension key
	// The adapter will read this to build the client list
	spec.Extensions["users"] = []map[string]any{defaultUser}

	return spec, nil
}
