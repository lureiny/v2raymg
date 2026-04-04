// Package profilegen provides standalone configuration generators for xray profiles.
// These functions are independent of container lifecycle and can be used to quickly
// generate Trojan configurations for trusted channel scenarios.
package profilegen

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// GenerateTrojanTLSParams defines parameters for generating a Trojan + TLS inbound spec.
// Only Host is required; other fields will be auto-generated if not specified.
type GenerateTrojanTLSParams struct {
	// Host is required - used for server_name in TLS config and external access.
	Host string

	// Transport is the transport layer protocol. Valid values: "tcp", "grpc".
	// Defaults to "tcp".
	Transport string

	// Port is the listen port. If 0, a random valid port will be assigned.
	// Valid range: 100-65535.
	Port uint32

	// Tag is the inbound tag. If empty, auto-generated like "trojan-tcp-tls-<random>".
	Tag string

	// ListenAddr is the listen address. Defaults to "0.0.0.0".
	ListenAddr string

	// Password is the Trojan password. If empty, auto-generated as random string.
	Password string

	// GRPCServiceName is the gRPC service name. Only used when Transport is "grpc".
	// Defaults to "grpc".
	GRPCServiceName string

	// CertFile is the path to the TLS certificate file (.crt/.pem).
	// Required when security=tls. If empty, caller must handle cert separately.
	CertFile string

	// KeyFile is the path to the TLS private key file (.key/.pem).
	// Required when security=tls. If empty, caller must handle cert separately.
	KeyFile string
}

// Validate checks required parameters.
func (p *GenerateTrojanTLSParams) Validate() error {
	if p.Host == "" {
		return fmt.Errorf("host is required")
	}

	// Normalize transport alias: http -> h2 (not applicable for Trojan, but keep for consistency)
	// Trojan only supports tcp and grpc
	if p.Transport != "" && p.Transport != "tcp" && p.Transport != "grpc" {
		return fmt.Errorf("invalid transport: %s (must be 'tcp' or 'grpc')", p.Transport)
	}

	// Validate port range if specified
	if p.Port != 0 && (p.Port < 100 || p.Port > 65535) {
		return fmt.Errorf("port %d out of range [100, 65535]", p.Port)
	}

	return nil
}

// applyDefaults applies default values to unspecified parameters.
func (p *GenerateTrojanTLSParams) applyDefaults() {
	if p.Transport == "" {
		p.Transport = "tcp"
	}
	if p.ListenAddr == "" {
		p.ListenAddr = "0.0.0.0"
	}
	if p.GRPCServiceName == "" && p.Transport == "grpc" {
		p.GRPCServiceName = "grpc"
	}
}

// generateRandomPassword generates a random Trojan password (random 32-byte hex string).
func generateRandomPassword() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// GenerateTrojanTLSInboundSpec generates a Trojan + TLS inbound spec.
// This is a standalone function that does not depend on container lifecycle.
//
// Security constraint: This function only generates TLS security mode.
// It will never generate security=none configurations.
//
// The returned InboundSpec can be passed directly to xray.Adapter.ToProvider()
// to generate the native xray JSON config, then used with container's
// AddInboundConfig or FastAddInbound flow.
func GenerateTrojanTLSInboundSpec(p GenerateTrojanTLSParams) (contracts.InboundSpec, error) {
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
		password = generateRandomPassword()
	}

	// Generate tag if not specified
	tag := p.Tag
	if tag == "" {
		tag = fmt.Sprintf("trojan-%s-tls-%d", p.Transport, randInt64()%10000)
	}

	// Create user with password as first-class field
	user := contracts.UserSpec{
		Username: fmt.Sprintf("auto@trojan.local"),
		Level:    0,
		Protocol: contracts.ProtocolTrojan,
		AuthToken: password,
	}

	// Build extensions for xray adapter
	extensions := map[string]any{
		"transport":   p.Transport,
		"security":    "tls", // Always TLS - hard constraint
		"server_name": p.Host,
		"tls_allow_insecure": false, // Trusted channel
	}

	// Add gRPC service name if transport is grpc
	if p.Transport == "grpc" && p.GRPCServiceName != "" {
		extensions["grpc_service_name"] = p.GRPCServiceName
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
		Protocol:   contracts.ProtocolTrojan,
		Extensions: extensions,
	}

	// Attach user to spec via special extension key
	// The adapter will read this to build the client list
	spec.Extensions["users"] = []contracts.UserSpec{user}

	return spec, nil
}
