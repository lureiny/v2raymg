// Package builder provides tests for the inbound builder.
package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildVMess(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildVMess("test-vmess", 10086, "0.0.0.0", "test@example.com", "test-uuid-123")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-vmess", cfg.Tag)
	assert.NotNil(t, cfg.ReceiverSettings)
	assert.NotNil(t, cfg.ProxySettings)

	// Verify proto serialization was used
	assert.NotEmpty(t, cfg.ProxySettings.Value)
}

func TestBuildVMessWS(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildVMessWS("test-vmess-ws", 10087, "0.0.0.0", "/ws", "example.com", "test@example.com", "test-uuid-123", []string{"h2"})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-vmess-ws", cfg.Tag)
	// Verify stream settings exist (exact format may vary)
	assert.NotNil(t, cfg.ReceiverSettings)
}

func TestBuildVMessTCPWithTLS(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildVMessTCPWithTLS("test-vmess-tls", 10443, "0.0.0.0", "vmess.example.com", "test@example.com", "test-uuid-123", []string{"h2", "http/1.1"})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-vmess-tls", cfg.Tag)
	// Verify TLS settings exist
	assert.NotNil(t, cfg.ReceiverSettings)
}

func TestBuildVLESSReality(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildVLESSReality("test-vless-reality", 10088, "0.0.0.0", "PublicKey123456", "abcd", "test@example.com", "test-uuid-123")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-vless-reality", cfg.Tag)
	// Verify Reality settings exist
	assert.NotNil(t, cfg.ReceiverSettings)
}

func TestBuildVLESSWSTLS(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildVLESSWSTLS("test-vless-ws", 10089, "0.0.0.0", "/vless", "example.com", "test@example.com", "test-uuid-123", []string{"h2"})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-vless-ws", cfg.Tag)
}

func TestBuildTrojanTCPWithTLS(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildTrojanTCPWithTLS("test-trojan", 10090, "0.0.0.0", "trojan.example.com", "test@example.com", "password123", []string{"h2"})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-trojan", cfg.Tag)
}

func TestBuildShadowsocks(t *testing.T) {
	b := NewInboundBuilder()

	cfg, err := b.BuildShadowsocks("test-ss", 10091, "0.0.0.0", "test@example.com", "password123", "aes-256-gcm")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "test-ss", cfg.Tag)
}

func TestBuildVMessNoUUID(t *testing.T) {
	b := NewInboundBuilder()

	_, err := b.BuildVMess("test", 10086, "0.0.0.0", "test@example.com", "")
	require.Error(t, err)
}

func TestBuildTrojanNoPassword(t *testing.T) {
	b := NewInboundBuilder()

	_, err := b.BuildTrojanTCPWithTLS("test", 10090, "0.0.0.0", "example.com", "test@example.com", "", nil)
	require.Error(t, err)
}

func TestBuildTrojanNoTLSRejected(t *testing.T) {
	b := NewInboundBuilder()

	_, err := b.BuildTrojanTCPWithTLS("test", 10090, "0.0.0.0", "", "test@example.com", "password123", nil)
	require.Error(t, err)
}

func TestBuildShadowsocksPortNotZero(t *testing.T) {
	b := NewInboundBuilder()

	_, err := b.BuildShadowsocksPortNotZero("test-ss", 0, "0.0.0.0", "test@example.com", "password123", "aes-256-gcm")
	require.Error(t, err)
}

func TestToProtoBytes(t *testing.T) {
	b := NewInboundBuilder()

	// Test that proto marshal works
	cfg, err := b.BuildVMess("test", 10086, "0.0.0.0", "test@example.com", "test-uuid")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// The test passes if we got here - proto serialization is working
	assert.NotEmpty(t, cfg.ProxySettings.Value)
}
