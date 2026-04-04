package profilegen

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateVMessTLSInboundSpec_HostRequired(t *testing.T) {
	// Host is empty - should fail
	_, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host is required")
}

func TestGenerateVMessTLSInboundSpec_DefaultTCP(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	// Validate basic fields
	assert.NotEmpty(t, spec.Tag)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
	assert.Equal(t, contracts.ProtocolVMess, spec.Protocol)

	// Validate extensions - must have TLS (hard constraint)
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok, "security should be set")
	assert.Equal(t, "tls", security, "security must be TLS for trusted channel")

	// Validate transport default
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok, "transport should be set")
	assert.Equal(t, "tcp", transport)

	// Validate server_name
	serverName, ok := spec.Extensions["server_name"].(string)
	require.True(t, ok)
	assert.Equal(t, "example.com", serverName)

	// Validate user exists
	users, ok := spec.Extensions["users"].([]map[string]any)
	require.True(t, ok, "users should be in extensions")
	require.Len(t, users, 1)
	assert.NotEmpty(t, users[0]["uuid"])

	// Verify no security=none
	assert.NotEqual(t, "none", security)
}

func TestGenerateVMessTLSInboundSpec_WithWS(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "ws",
	})
	require.NoError(t, err)

	// Validate transport
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "ws", transport)

	// Validate WS path default
	wsPath, ok := spec.Extensions["ws_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/ws", wsPath)

	// TLS must still be set
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "tls", security)
}

func TestGenerateVMessTLSInboundSpec_WithCustomWSPath(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "ws",
		WSPath:    "/custom/path",
	})
	require.NoError(t, err)

	wsPath, ok := spec.Extensions["ws_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/custom/path", wsPath)
}

func TestGenerateVMessTLSInboundSpec_PortAssignment(t *testing.T) {
	// Test with specific port
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
		Port: 12345,
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(12345), spec.Port)
}

func TestGenerateVMessTLSInboundSpec_RandomPort(t *testing.T) {
	// Port = 0 should generate random port
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
		Port: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
}

func TestGenerateVMessTLSInboundSpec_Randomness(t *testing.T) {
	// Generate two specs and verify they have different UUIDs and tags
	spec1, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	spec2, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	// UUIDs should be different
	users1 := spec1.Extensions["users"].([]map[string]any)
	users2 := spec2.Extensions["users"].([]map[string]any)
	assert.NotEqual(t, users1[0]["uuid"], users2[0]["uuid"])

	// Tags should be different (or at least one should be auto-generated differently)
	assert.NotEqual(t, spec1.Tag, spec2.Tag)
}

func TestGenerateVMessTLSInboundSpec_CustomTag(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
		Tag:  "my-custom-tag",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-custom-tag", spec.Tag)
}

func TestGenerateVMessTLSInboundSpec_InvalidTransport(t *testing.T) {
	_, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transport")
}

func TestGenerateVMessTLSInboundSpec_InvalidPort(t *testing.T) {
	_, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
		Port: 99, // Below valid range
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")

	_, err = GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
		Port: 65536, // Above valid range
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestGenerateVMessTLSInboundSpec_NoInsecure(t *testing.T) {
	// tls_allow_insecure must be false for trusted channel
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	allowInsecure, ok := spec.Extensions["tls_allow_insecure"].(bool)
	require.True(t, ok, "tls_allow_insecure should be set")
	assert.False(t, allowInsecure, "tls_allow_insecure must be false for trusted channel")
}

func TestGenerateVMessTLSInboundSpec_ListenAddr(t *testing.T) {
	// Test default listen address
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)
	// Default should not include listen_addr in extensions (Adapter handles default)
	_, hasDefaultListen := spec.Extensions["listen_addr"]
	assert.False(t, hasDefaultListen, "default listen_addr should not be in extensions")

	// Test custom listen address
	spec2, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:       "example.com",
		ListenAddr: "192.168.1.1",
	})
	require.NoError(t, err)
	listenAddr, ok := spec2.Extensions["listen_addr"].(string)
	require.True(t, ok)
	assert.Equal(t, "192.168.1.1", listenAddr)
}

func TestIsPortAvailable(t *testing.T) {
	// Test invalid port
	assert.False(t, IsPortAvailable(0))
	assert.False(t, IsPortAvailable(99))

	// Test valid but unlikely to be used port
	// Note: this may fail if port is already in use
	result := IsPortAvailable(65432)
	// Just verify it doesn't panic - actual availability depends on system state
	_ = result
}

func TestGetPreferredHost(t *testing.T) {
	// This test just verifies the function doesn't panic
	// Actual return value depends on network configuration
	host := GetPreferredHost()
	t.Logf("Preferred host: %s", host)
}

func TestGenerateVMessTLSInboundSpec_WithCert(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:     "example.com",
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

func TestGenerateVMessTLSInboundSpec_NoCertWhenEmpty(t *testing.T) {
	// When CertFile/KeyFile are empty, cert_file/key_file should not be in extensions
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	_, hasCertFile := spec.Extensions["cert_file"]
	assert.False(t, hasCertFile, "cert_file should not be set when CertFile is empty")
	_, hasKeyFile := spec.Extensions["key_file"]
	assert.False(t, hasKeyFile, "key_file should not be set when KeyFile is empty")
}

func TestGenerateVMessTLSInboundSpec_UserExtensions(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]map[string]any)
	require.Len(t, users, 1)

	user := users[0]
	assert.Equal(t, "auto@vmess.local", user["email"].(string))
	// Check VMess-specific extensions
	uuid, ok := user["uuid"].(string)
	require.True(t, ok, "uuid should be set")
	assert.NotEmpty(t, uuid)

	alterID, ok := user["alter_id"].(uint32)
	require.True(t, ok, "alter_id should be set")
	assert.Equal(t, uint32(0), alterID)
}

func TestGenerateVMessTLSInboundSpec_WithHTTP(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "http",
	})
	require.NoError(t, err)

	// Validate transport is normalized to h2
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "h2", transport, "http should be normalized to h2 internally")

	// Validate HTTP path default
	httpPath, ok := spec.Extensions["http_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/", httpPath)

	// Validate HTTP host defaults to Host
	httpHost, ok := spec.Extensions["http_host"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"example.com"}, httpHost)

	// TLS must still be set
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "tls", security)
}

func TestGenerateVMessTLSInboundSpec_WithCustomHTTPPath(t *testing.T) {
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:       "example.com",
		Transport:  "http",
		HTTPPath:    "/custom/http/path",
		HTTPHost:   []string{"host1.example.com", "host2.example.com"},
	})
	require.NoError(t, err)

	// Validate transport is normalized to h2
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "h2", transport)

	httpPath, ok := spec.Extensions["http_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/custom/http/path", httpPath)

	httpHost, ok := spec.Extensions["http_host"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"host1.example.com", "host2.example.com"}, httpHost)
}

func TestGenerateVMessTLSInboundSpec_WithH2Alias(t *testing.T) {
	// h2 should be normalized to http
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "h2",
	})
	require.NoError(t, err)

	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "h2", transport, "h2 stays as h2")
}

func TestGenerateVMessTLSInboundSpec_HTTPHostDefaultsToHost(t *testing.T) {
	// When HTTPHost is empty but transport is http, should default to Host
	spec, err := GenerateVMessTLSInboundSpec(GenerateVMessTLSParams{
		Host:      "example.com",
		Transport: "http",
	})
	require.NoError(t, err)

	// Validate transport is normalized to h2
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "h2", transport)

	httpHost, ok := spec.Extensions["http_host"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"example.com"}, httpHost)
}
