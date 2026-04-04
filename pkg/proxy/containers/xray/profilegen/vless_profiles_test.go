package profilegen

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateVLessInboundSpec_HostRequired(t *testing.T) {
	_, err := GenerateVLessInboundSpec(GenerateVLessParams{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host is required")
}

func TestGenerateVLessInboundSpec_DefaultTCP(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, spec.Tag)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
	assert.Equal(t, contracts.ProtocolVLess, spec.Protocol)

	// Validate transport defaults to tcp
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "tcp", transport)

	// Validate security defaults to tls
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "tls", security)

	// Validate user exists
	users, ok := spec.Extensions["users"].([]map[string]any)
	require.True(t, ok, "users should be in extensions")
	require.Len(t, users, 1)
	assert.NotEmpty(t, users[0]["uuid"])
}

func TestGenerateVLessInboundSpec_WithWS(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "ws",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "ws", transport)

	// Validate WS path default
	wsPath, ok := spec.Extensions["ws_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/ws", wsPath)
}

func TestGenerateVLessInboundSpec_WithGRPC(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "grpc",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "grpc", transport)

	grpcService, ok := spec.Extensions["grpc_service_name"].(string)
	require.True(t, ok)
	assert.Equal(t, "grpc", grpcService)
}

func TestGenerateVLessInboundSpec_WithHTTP(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "http",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "http", transport)

	httpPath, ok := spec.Extensions["http_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/", httpPath)
}

func TestGenerateVLessInboundSpec_WithReality(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:     "example.com",
		Security: "reality",
	})
	require.NoError(t, err)

	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "reality", security)

	// Check flow is auto-set
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"].(string)
	require.True(t, ok)
	assert.Equal(t, "xtls-rprx-vision", flow)

	// Check reality settings
	serverNames, ok := spec.Extensions["reality_server_names"].([]string)
	require.True(t, ok)
	assert.Contains(t, serverNames, "example.com")

	// Check reality key pair is auto-generated
	privateKey, ok := spec.Extensions["reality_private_key"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, privateKey)

	publicKey, ok := spec.Extensions["reality_public_key"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, publicKey)
}

func TestGenerateVLessInboundSpec_WithRealityKeyPair(t *testing.T) {
	// Test providing both keys explicitly
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:             "example.com",
		Security:         "reality",
		RealityPrivateKey: "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE",
		RealityPublicKey:  "YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
	})
	require.NoError(t, err)

	privateKey, ok := spec.Extensions["reality_private_key"].(string)
	require.True(t, ok)
	assert.Equal(t, "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE", privateKey)

	publicKey, ok := spec.Extensions["reality_public_key"].(string)
	require.True(t, ok)
	assert.Equal(t, "YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI", publicKey)
}

func TestGenerateVLessInboundSpec_WithRealityOnlyPrivateKey_ShouldFail(t *testing.T) {
	// Test providing only private key should fail
	_, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:             "example.com",
		Security:         "reality",
		RealityPrivateKey: "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reality_private_key and reality_public_key must be provided together")
}

func TestGenerateVLessInboundSpec_WithRealityOnlyPublicKey_ShouldFail(t *testing.T) {
	// Test providing only public key should fail
	_, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:            "example.com",
		Security:        "reality",
		RealityPublicKey: "YmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmJiYmI",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reality_private_key and reality_public_key must be provided together")
}

func TestGenerateVLessInboundSpec_WithXHTTP(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:       "example.com",
		Transport:  "xhttp",
		XHTTPMode:  "auto",
		XHTTPPath:  "/xhttp",
		XHTTPHost:  []string{"xhttp.example.com"},
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "xhttp", transport)

	mode, ok := spec.Extensions["xhttp_mode"].(string)
	require.True(t, ok)
	assert.Equal(t, "auto", mode)

	path, ok := spec.Extensions["xhttp_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/xhttp", path)
}

func TestGenerateVLessInboundSpec_WithSplitHTTP(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:       "example.com",
		Transport:  "splithttp",
		XHTTPMode:  "auto",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "splithttp", transport)
}

func TestGenerateVLessInboundSpec_WithCustomUUID(t *testing.T) {
	customUUID := "a348c600-1234-5678-9abc-def012345678"
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:  "example.com",
		UUID:  customUUID,
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]map[string]any)
	uuid, ok := users[0]["uuid"].(string)
	require.True(t, ok)
	assert.Equal(t, customUUID, uuid)
}

func TestGenerateVLessInboundSpec_WithCustomFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:    "example.com",
		Security: "xtls",
		Flow:    "xtls-rprx-vision",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"].(string)
	require.True(t, ok)
	assert.Equal(t, "xtls-rprx-vision", flow)
}

func TestGenerateVLessInboundSpec_WithPort(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host: "example.com",
		Port: 12345,
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(12345), spec.Port)
}

func TestGenerateVLessInboundSpec_RandomPort(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host: "example.com",
		Port: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
}

func TestGenerateVLessInboundSpec_Randomness(t *testing.T) {
	spec1, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	spec2, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	// UUIDs should be different
	users1 := spec1.Extensions["users"].([]map[string]any)
	users2 := spec2.Extensions["users"].([]map[string]any)
	assert.NotEqual(t, users1[0]["uuid"], users2[0]["uuid"])
}

func TestGenerateVLessInboundSpec_InvalidTransport(t *testing.T) {
	_, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transport")
}

func TestGenerateVLessInboundSpec_InvalidSecurity(t *testing.T) {
	_, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:     "example.com",
		Security: "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid security")
}

// Test xhttp + reality: flow should be empty
func TestGenerateVLessInboundSpec_XHTTP_Reality_NoFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "xhttp",
		Security:  "reality",
	})
	require.NoError(t, err)

	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "reality", security)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "xhttp", transport)

	// Flow should NOT be set for xhttp + reality
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"]
	assert.False(t, ok, "flow should not be set for xhttp + reality")
	assert.Nil(t, flow)
}

// Test splithttp + reality: flow should be empty
func TestGenerateVLessInboundSpec_SplitHTTP_Reality_NoFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "splithttp",
		Security:  "reality",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "splithttp", transport)

	// Flow should NOT be set for splithttp + reality
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"]
	assert.False(t, ok, "flow should not be set for splithttp + reality")
	assert.Nil(t, flow)
}

// Test h3 + reality: flow should be empty
func TestGenerateVLessInboundSpec_H3_Reality_NoFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "h3",
		Security:  "reality",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "h3", transport)

	// Flow should NOT be set for h3 + reality
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"]
	assert.False(t, ok, "flow should not be set for h3 + reality")
	assert.Nil(t, flow)
}

// Test tcp + reality: flow SHOULD be set (xtls-rprx-vision)
func TestGenerateVLessInboundSpec_TCP_Reality_WithFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "tcp",
		Security:  "reality",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "tcp", transport)

	// Flow SHOULD be set for tcp + reality
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"].(string)
	require.True(t, ok)
	assert.Equal(t, "xtls-rprx-vision", flow)
}

// Test xtls + xhttp: flow SHOULD be set (xtls-rprx-vision)
func TestGenerateVLessInboundSpec_XHTTP_XTLS_WithFlow(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:      "example.com",
		Transport: "xhttp",
		Security:  "xtls",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "xhttp", transport)

	// Flow SHOULD be set for xhttp + xtls (not reality)
	users := spec.Extensions["users"].([]map[string]any)
	flow, ok := users[0]["flow"].(string)
	require.True(t, ok)
	assert.Equal(t, "xtls-rprx-vision", flow)
}

func TestGenerateVLessInboundSpec_WithCert(t *testing.T) {
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:     "example.com",
		Security: "tls",
		CertFile: "/etc/certs/example.com.crt",
		KeyFile:  "/etc/certs/example.com.key",
	})
	require.NoError(t, err)

	certFile, ok := spec.Extensions["cert_file"].(string)
	require.True(t, ok, "cert_file should be set")
	assert.Equal(t, "/etc/certs/example.com.crt", certFile)

	keyFile, ok := spec.Extensions["key_file"].(string)
	require.True(t, ok, "key_file should be set")
	assert.Equal(t, "/etc/certs/example.com.key", keyFile)
}

func TestGenerateVLessInboundSpec_WithCertReality_NoCertInExtensions(t *testing.T) {
	// For reality, cert_file/key_file should NOT be added even if provided
	spec, err := GenerateVLessInboundSpec(GenerateVLessParams{
		Host:              "example.com",
		Security:          "reality",
		CertFile:          "/etc/certs/example.com.crt",
		KeyFile:           "/etc/certs/example.com.key",
		RealityPrivateKey: "dGVzdHByaXZhdGVrZXkxMjM0NTY3ODkwYWJjZGVm",
		RealityPublicKey:  "dGVzdHByaXZhdGVrZXkxMjM0NTY3ODkwYWJjZGVm",
	})
	require.NoError(t, err)

	_, hasCertFile := spec.Extensions["cert_file"]
	assert.False(t, hasCertFile, "cert_file should not be set for reality")
	_, hasKeyFile := spec.Extensions["key_file"]
	assert.False(t, hasKeyFile, "key_file should not be set for reality")
}
