package profilegen

import (
	"encoding/base64"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateShadowsocksInboundSpec_HostRequired(t *testing.T) {
	// Host is empty - should fail
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host is required")
}

func TestGenerateShadowsocksInboundSpec_Default2022Method(t *testing.T) {
	// When method is not specified, default to 2022-blake3-aes-256-gcm
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	// Validate basic fields
	assert.NotEmpty(t, spec.Tag)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
	assert.Equal(t, contracts.ProtocolShadowsocks, spec.Protocol)

	// Validate default method (2022 series)
	method, ok := spec.Extensions["method"].(string)
	require.True(t, ok, "method should be set")
	assert.Equal(t, "2022-blake3-aes-256-gcm", method, "default method should be 2022-blake3-aes-256-gcm")

	// Validate default security (none)
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok, "security should be set")
	assert.Equal(t, "none", security, "default security should be none")

	// Validate default transport (tcp)
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok, "transport should be set")
	assert.Equal(t, "tcp", transport)

	// Validate user exists
	users, ok := spec.Extensions["users"].([]contracts.UserSpec)
	require.True(t, ok, "users should be in extensions")
	require.Len(t, users, 1)
	assert.NotEmpty(t, users[0].Password)
	assert.Equal(t, contracts.ProtocolShadowsocks, users[0].Protocol)

	// Validate user method matches
	userMethod, ok := users[0].Extensions["method"].(string)
	require.True(t, ok, "user method should be set")
	assert.Equal(t, "2022-blake3-aes-256-gcm", userMethod)
}

func TestGenerateShadowsocksInboundSpec_WithCustomMethod(t *testing.T) {
	// When method is specified, preserve user's value
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:   "example.com",
		Method: "aes-256-gcm",
	})
	require.NoError(t, err)

	// Validate method is preserved
	method, ok := spec.Extensions["method"].(string)
	require.True(t, ok)
	assert.Equal(t, "aes-256-gcm", method)

	// Validate user method
	users := spec.Extensions["users"].([]contracts.UserSpec)
	userMethod, ok := users[0].Extensions["method"].(string)
	require.True(t, ok)
	assert.Equal(t, "aes-256-gcm", userMethod)
}

func TestGenerateShadowsocksInboundSpec_With2022Method(t *testing.T) {
	// Test all 2022 methods
	methods := []string{
		"2022-blake3-aes-256-gcm",
		"2022-blake3-aes-128-gcm",
		"2022-blake3-chacha20-poly1305",
	}

	for _, method := range methods {
		spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
			Host:   "example.com",
			Method: method,
		})
		require.NoError(t, err, "method %s should work", method)

		m, ok := spec.Extensions["method"].(string)
		require.True(t, ok)
		assert.Equal(t, method, m, "method %s should be preserved", method)
	}
}

func TestGenerateShadowsocksInboundSpec_WithTLS(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Security: "tls",
	})
	require.NoError(t, err)

	// Validate security
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "tls", security)
}

func TestGenerateShadowsocksInboundSpec_WithGRPC(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:      "example.com",
		Transport: "grpc",
	})
	require.NoError(t, err)

	// Validate transport
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "grpc", transport)

	// Validate grpc_service_name default
	grpcServiceName, ok := spec.Extensions["grpc_service_name"].(string)
	require.True(t, ok)
	assert.Equal(t, "grpc", grpcServiceName)
}

func TestGenerateShadowsocksInboundSpec_WithCustomGRPCServiceName(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:            "example.com",
		Transport:       "grpc",
		GRPCServiceName: "my-ss-service",
	})
	require.NoError(t, err)

	grpcServiceName, ok := spec.Extensions["grpc_service_name"].(string)
	require.True(t, ok)
	assert.Equal(t, "my-ss-service", grpcServiceName)
}

func TestGenerateShadowsocksInboundSpec_WithWS(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:      "example.com",
		Transport: "ws",
	})
	require.NoError(t, err)

	// Validate transport
	transport, ok := spec.Extensions["transport"].(string)
	require.True(t, ok)
	assert.Equal(t, "ws", transport)

	// Validate ws_path default
	wsPath, ok := spec.Extensions["ws_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/", wsPath)
}

func TestGenerateShadowsocksInboundSpec_WithCustomWSPath(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:      "example.com",
		Transport: "ws",
		WSPath:    "/custom-path",
	})
	require.NoError(t, err)

	wsPath, ok := spec.Extensions["ws_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "/custom-path", wsPath)
}

func TestGenerateShadowsocksInboundSpec_PortAssignment(t *testing.T) {
	// Test with specific port
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
		Port: 12345,
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(12345), spec.Port)
}

func TestGenerateShadowsocksInboundSpec_RandomPort(t *testing.T) {
	// Port = 0 should generate random port
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
		Port: 0,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, spec.Port, uint32(100))
	assert.LessOrEqual(t, spec.Port, uint32(65535))
}

func TestGenerateShadowsocksInboundSpec_Randomness(t *testing.T) {
	// Generate two specs and verify they have different passwords and tags
	spec1, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	spec2, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	// Passwords should be different
	users1 := spec1.Extensions["users"].([]contracts.UserSpec)
	users2 := spec2.Extensions["users"].([]contracts.UserSpec)
	assert.NotEqual(t, users1[0].Password, users2[0].Password)

	// Tags should be different (or at least one should be auto-generated differently)
	assert.NotEqual(t, spec1.Tag, spec2.Tag)
}

func TestGenerateShadowsocksInboundSpec_CustomTag(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
		Tag:  "my-custom-tag",
	})
	require.NoError(t, err)
	assert.Equal(t, "my-custom-tag", spec.Tag)
}

func TestGenerateShadowsocksInboundSpec_CustomPassword(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Password: "my-custom-password",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	require.Len(t, users, 1)
	assert.Equal(t, "my-custom-password", users[0].Password)
}

func TestGenerateShadowsocksInboundSpec_PasswordAutoGenerated(t *testing.T) {
	// When password is empty, it should be auto-generated
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Password: "",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	require.Len(t, users, 1)
	assert.NotEmpty(t, users[0].Password, "password should be auto-generated when empty")
}

func TestGenerateShadowsocksInboundSpec_InvalidTransport(t *testing.T) {
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:      "example.com",
		Transport: "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transport")
}

func TestGenerateShadowsocksInboundSpec_InvalidSecurity(t *testing.T) {
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Security: "invalid",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid security")
}

func TestGenerateShadowsocksInboundSpec_InvalidPort(t *testing.T) {
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
		Port: 99, // Below valid range
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")

	_, err = GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
		Port: 65536, // Above valid range
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestGenerateShadowsocksInboundSpec_InvalidMethod(t *testing.T) {
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:   "example.com",
		Method: "invalid-method",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid encryption method")
}

func TestGenerateShadowsocksInboundSpec_ListenAddr(t *testing.T) {
	// Test default listen address
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)
	// Default should not include listen_addr in extensions (Adapter handles default)
	_, hasDefaultListen := spec.Extensions["listen_addr"]
	assert.False(t, hasDefaultListen, "default listen_addr should not be in extensions")

	// Test custom listen address
	spec2, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:       "example.com",
		ListenAddr: "192.168.1.1",
	})
	require.NoError(t, err)
	listenAddr, ok := spec2.Extensions["listen_addr"].(string)
	require.True(t, ok)
	assert.Equal(t, "192.168.1.1", listenAddr)
}

func TestGenerateShadowsocksInboundSpec_UserExtensions(t *testing.T) {
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	require.Len(t, users, 1)

	user := users[0]
	assert.Equal(t, "auto@ss.local", user.Username)
	assert.Equal(t, uint32(0), user.Level)
	// Check Shadowsocks-specific password field
	assert.NotEmpty(t, user.Password, "password should be set")

	method, ok := user.Extensions["method"].(string)
	require.True(t, ok, "method should be set")
	assert.NotEmpty(t, method)
}

func TestGenerateShadowsocksInboundSpec_GRPCServiceNameNotForTCP(t *testing.T) {
	// grpc_service_name should not be included when transport is tcp
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:            "example.com",
		Transport:       "tcp",
		GRPCServiceName: "should-be-ignored",
	})
	require.NoError(t, err)

	_, hasGRPCServiceName := spec.Extensions["grpc_service_name"]
	assert.False(t, hasGRPCServiceName, "grpc_service_name should not be set for tcp transport")
}

func TestGenerateShadowsocksInboundSpec_WSPathNotForTCP(t *testing.T) {
	// ws_path should not be included when transport is tcp
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:      "example.com",
		Transport: "tcp",
		WSPath:    "/should-be-ignored",
	})
	require.NoError(t, err)

	_, hasWSPath := spec.Extensions["ws_path"]
	assert.False(t, hasWSPath, "ws_path should not be set for tcp transport")
}

func TestGenerateShadowsocksInboundSpec_AdapterCompatible(t *testing.T) {
	// Verify that the generated spec can be processed by the existing adapter
	// This test ensures compatibility with the existing xray.Adapter.ToProvider()
	// Use a valid base64-encoded 32-byte key for 2022 method
	validKey := base64.StdEncoding.EncodeToString(make([]byte, 32)) // 32 bytes of zero

	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Port:     12345,
		Tag:      "test-ss",
		Method:   "2022-blake3-aes-256-gcm",
		Password: validKey,
	})
	require.NoError(t, err)

	// Static assertions for key fields that adapter expects
	assert.Equal(t, contracts.ProtocolShadowsocks, spec.Protocol)
	assert.Equal(t, "test-ss", spec.Tag)
	assert.Equal(t, uint32(12345), spec.Port)

	// Adapter expects: users in extensions with method and password
	users, ok := spec.Extensions["users"].([]contracts.UserSpec)
	require.True(t, ok, "users should be present")
	require.Len(t, users, 1)
	assert.Equal(t, validKey, users[0].Password)
	assert.Equal(t, "2022-blake3-aes-256-gcm", users[0].Extensions["method"])

	// Adapter expects: method in extensions (for buildDefaultSettings)
	method, ok := spec.Extensions["method"].(string)
	require.True(t, ok)
	assert.Equal(t, "2022-blake3-aes-256-gcm", method)

	// Adapter expects: security in extensions
	security, ok := spec.Extensions["security"].(string)
	require.True(t, ok)
	assert.Equal(t, "none", security)
}

func TestGenerateShadowsocksInboundSpec_2022KeyLength256(t *testing.T) {
	// Default method (2022-blake3-aes-256-gcm) should generate key that decodes to 32 bytes
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host: "example.com",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	password := users[0].Password

	decoded, err := base64.StdEncoding.DecodeString(password)
	require.NoError(t, err, "password should be valid base64")
	assert.Len(t, decoded, 32, "2022-blake3-aes-256-gcm should have 32-byte key")
}

func TestGenerateShadowsocksInboundSpec_2022KeyLength128(t *testing.T) {
	// 2022-blake3-aes-128-gcm should generate key that decodes to 16 bytes
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:   "example.com",
		Method: "2022-blake3-aes-128-gcm",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	password := users[0].Password

	decoded, err := base64.StdEncoding.DecodeString(password)
	require.NoError(t, err, "password should be valid base64")
	assert.Len(t, decoded, 16, "2022-blake3-aes-128-gcm should have 16-byte key")
}

func TestGenerateShadowsocksInboundSpec_2022KeyLengthChaCha20(t *testing.T) {
	// 2022-blake3-chacha20-poly1305 should generate key that decodes to 32 bytes
	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:   "example.com",
		Method: "2022-blake3-chacha20-poly1305",
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	password := users[0].Password

	decoded, err := base64.StdEncoding.DecodeString(password)
	require.NoError(t, err, "password should be valid base64")
	assert.Len(t, decoded, 32, "2022-blake3-chacha20-poly1305 should have 32-byte key")
}

func TestGenerateShadowsocksInboundSpec_Invalid2022PasswordNotBase64(t *testing.T) {
	// Invalid 2022 password (not valid base64) should fail
	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Method:   "2022-blake3-aes-256-gcm",
		Password: "not-valid-base64!!!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid 2022 password")
}

func TestGenerateShadowsocksInboundSpec_Invalid2022PasswordLength(t *testing.T) {
	// Invalid 2022 password (valid base64 but wrong length) should fail
	// Generate a 16-byte key (valid base64 but too short for aes-256)
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(i)
	}
	shortKey := base64.StdEncoding.EncodeToString(b)

	_, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Method:   "2022-blake3-aes-256-gcm",
		Password: shortKey,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid 2022 password")
	assert.Contains(t, err.Error(), "requires 32 bytes")
}

func TestGenerateShadowsocksInboundSpec_Valid2022Password(t *testing.T) {
	// Valid 2022 password should pass validation
	// Generate a 32-byte key for aes-256
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i)
	}
	validKey := base64.StdEncoding.EncodeToString(b)

	spec, err := GenerateShadowsocksInboundSpec(GenerateShadowsocksParams{
		Host:     "example.com",
		Method:   "2022-blake3-aes-256-gcm",
		Password: validKey,
	})
	require.NoError(t, err)

	users := spec.Extensions["users"].([]contracts.UserSpec)
	assert.Equal(t, validKey, users[0].Password)
}
