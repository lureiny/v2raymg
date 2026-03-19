// Package profilegen provides standalone configuration generators for xray profiles.
// These functions are independent of container lifecycle and can be used to quickly
// generate VMess/TLS configurations for trusted channel scenarios.
package profilegen

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// GenerateVMessTLSParams defines parameters for generating a VMess + TLS inbound spec.
// Only Host is required; other fields will be auto-generated if not specified.
type GenerateVMessTLSParams struct {
	// Host is required - used for server_name in TLS config and external access.
	Host string

	// Transport is the transport layer protocol. Valid values: "tcp", "ws", "h2".
	// Defaults to "tcp". "http" is accepted as alias and normalized to "h2".
	Transport string

	// Port is the listen port. If 0, a random valid port will be assigned.
	// Valid range: 100-65535.
	Port uint32

	// Tag is the inbound tag. If empty, auto-generated like "vmess-tcp-tls-<random>".
	Tag string

	// WSPath is the WebSocket path. Only used when Transport is "ws".
	// Defaults to "/ws".
	WSPath string

	// HTTPPath is the HTTP/2 path. Only used when Transport is "h2" (or alias "http").
	// Defaults to "/".
	HTTPPath string

	// HTTPHost is the HTTP/2 host. Only used when Transport is "h2" (or alias "http").
	// If empty, defaults to Host.
	HTTPHost []string

	// ListenAddr is the listen address. Defaults to "0.0.0.0".
	ListenAddr string

	// CertFile is the path to the TLS certificate file (.crt/.pem).
	// Required when security=tls/xtls. If empty, caller must handle cert separately.
	CertFile string

	// KeyFile is the path to the TLS private key file (.key/.pem).
	// Required when security=tls/xtls. If empty, caller must handle cert separately.
	KeyFile string
}

// Validate checks required parameters.
func (p *GenerateVMessTLSParams) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host is required")
	}
	// Normalize transport alias: http -> h2
	if p.Transport == "http" {
		p.Transport = "h2"
	}
	// Validate transport
	if p.Transport != "" && p.Transport != "tcp" && p.Transport != "ws" && p.Transport != "h2" {
		return fmt.Errorf("invalid transport: %s (must be 'tcp', 'ws', 'h2', or alias 'http')", p.Transport)
	}
	// Validate port range if specified
	if p.Port != 0 && (p.Port < 100 || p.Port > 65535) {
		return fmt.Errorf("port %d out of range [100, 65535]", p.Port)
	}
	return nil
}

// applyDefaults applies default values to unspecified parameters.
func (p *GenerateVMessTLSParams) applyDefaults() {
	// Normalize transport alias: http -> h2
	if p.Transport == "http" {
		p.Transport = "h2"
	}
	if p.Transport == "" {
		p.Transport = "tcp"
	}
	if p.ListenAddr == "" {
		p.ListenAddr = "0.0.0.0"
	}
	if p.WSPath == "" && p.Transport == "ws" {
		p.WSPath = "/ws"
	}
	if p.HTTPPath == "" && p.Transport == "h2" {
		p.HTTPPath = "/"
	}
	if len(p.HTTPHost) == 0 && p.Transport == "h2" {
		p.HTTPHost = []string{p.Host}
	}
}

// GenerateVMessTLSInboundSpec generates a VMess + TLS inbound spec.
// This is a standalone function that does not depend on container lifecycle.
//
// Security constraint: This function only generates TLS security mode.
// It will never generate security=none configurations.
//
// The returned InboundSpec can be passed directly to xray.Adapter.ToProvider()
// to generate the native xray JSON config, then used with container's
// AddInboundConfig or FastAddInbound flow.
func GenerateVMessTLSInboundSpec(p GenerateVMessTLSParams) (contracts.InboundSpec, error) {
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

	// Generate random UUID for VMess
	uuidStr := uuid.New().String()

	// Generate tag if not specified
	tag := p.Tag
	if tag == "" {
		tag = fmt.Sprintf("vmess-%s-tls-%d", p.Transport, randInt64()%10000)
	}

	// Create user with UUID in extensions
	user := contracts.UserSpec{
		Username:  fmt.Sprintf("auto@vmess.local"),
		Level:     0,
		Protocol:  contracts.ProtocolVMess,
		Extensions: map[string]any{
			"uuid":      uuidStr,
			"alter_id":  uint32(0), // Recommended for TLS
		},
	}

	// Build extensions for xray adapter
	extensions := map[string]any{
		"transport":           p.Transport,
		"security":            "tls", // Always TLS - hard constraint
		"server_name":         p.Host,
		"tls_allow_insecure":  false, // Trusted channel
	}

	// Add WS path if transport is ws
	if p.Transport == "ws" && p.WSPath != "" {
		extensions["ws_path"] = p.WSPath
	}

	// Add HTTP/2 settings if transport is h2
	if p.Transport == "h2" {
		if p.HTTPPath != "" {
			extensions["http_path"] = p.HTTPPath
		}
		if len(p.HTTPHost) > 0 {
			extensions["http_host"] = p.HTTPHost
		}
	}

	// Add listen address if not default
	if p.ListenAddr != "0.0.0.0" {
		extensions["listen_addr"] = p.ListenAddr
	}

	// Add certificate file paths if provided
	if p.CertFile != "" && p.KeyFile != "" {
		extensions["cert_file"] = p.CertFile
		extensions["key_file"] = p.KeyFile
	}

	spec := contracts.InboundSpec{
		Tag:        tag,
		Port:       port,
		Protocol:   contracts.ProtocolVMess,
		Extensions: extensions,
	}

	// Attach user to spec via special extension key
	// The adapter will read this to build the client list
	spec.Extensions["users"] = []contracts.UserSpec{user}

	return spec, nil
}

// generateRandomPort generates a random valid port in range [100, 65535].
func generateRandomPort() (uint32, error) {
	// Range: 100-65535 = 65436 possible values
	n, err := rand.Int(rand.Reader, big.NewInt(65436))
	if err != nil {
		return 0, err
	}
	return uint32(n.Int64()) + 100, nil
}

// randInt64 generates a random int64.
func randInt64() int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	return n.Int64()
}

// IsPortAvailable checks if a port is available for binding.
// Returns true if the port is available, false if it's already in use.
func IsPortAvailable(port uint32) bool {
	if port < 100 || port > 65535 {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// AllocateAvailablePort finds an available port starting from hint.
// If hint is 0, starts from a random port.
// Returns 0 if no port is available.
func AllocateAvailablePort(hint uint32) uint32 {
	if hint == 0 {
		hint = 10000 + uint32(randInt64()%50000) // Random start in 10000-60000 range
	}

	for i := uint32(0); i < 1000; i++ {
		port := hint + i
		if port > 65535 {
			port = 100 + (port - 65535)
		}
		if IsPortAvailable(port) {
			return port
		}
	}
	return 0
}

// GetOutboundIP tries to get the outbound IP address.
// Returns empty string if it cannot be determined.
func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// GetPreferredHost returns the preferred host for the generated config.
// Priority: env V2RAYMG_PREFERRED_HOST > system outbound IP > empty (use input host).
func GetPreferredHost() string {
	if host := os.Getenv("V2RAYMG_PREFERRED_HOST"); host != "" {
		return host
	}
	return GetOutboundIP()
}
