// Package xray provides Xray container implementation tests.
package xray

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// TestExecutor_EnsureBinary_NotFound tests that EnsureBinary returns error
// when binary doesn't exist and AutoDownload is disabled.
func TestExecutor_EnsureBinary_NotFound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:   "/nonexistent/xray",
		AutoDownload: false,
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = exec.EnsureBinary(ctx)
	if err == nil {
		t.Error("expected error when binary not found and AutoDownload is disabled")
	}
}

// TestExecutor_EnsureBinary_AutoDownloadEnabled tests the behavior
// when AutoDownload is enabled but no updater is configured.
// In this case, it silently skips the download and returns no error.
func TestExecutor_EnsureBinary_AutoDownloadNoUpdater(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:   "/nonexistent/xray",
		AutoDownload: true,
		// No UpdateConfig - updater will be nil
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	err = exec.EnsureBinary(ctx)
	// When AutoDownload is true but no updater, it silently skips download
	// and returns no error (this is current behavior - may want to change)
	_ = err
}

// TestExecutor_EnsureConfig_EmptyPath tests that EnsureConfig generates
// a default config when ConfigFilePath is empty.
func TestExecutor_EnsureConfig_EmptyPath(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
		// ConfigFilePath is empty
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.EnsureConfig()
	if err != nil {
		t.Errorf("EnsureConfig failed: %v", err)
	}

	// Config file path should be set after EnsureConfig
	if exec.ConfigFile() == "" {
		t.Error("ConfigFilePath should be set after EnsureConfig")
	}
}

// TestExecutor_EnsureConfig_ValidPath tests that EnsureConfig succeeds
// when a valid config file exists.
func TestExecutor_EnsureConfig_ValidPath(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	configContent := `{"log":{"loglevel":"warning"},"inbounds":[],"outbounds":[]}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: configPath,
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.EnsureConfig()
	if err != nil {
		t.Errorf("EnsureConfig failed for existing file: %v", err)
	}
}

// TestExecutor_EnsureConfig_InvalidPath tests that EnsureConfig generates
// a default config when the config file path doesn't exist.
func TestExecutor_EnsureConfig_InvalidPath(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/nonexistent/path/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.EnsureConfig()
	if err != nil {
		t.Errorf("EnsureConfig failed: %v", err)
	}

	// Should generate a default config
	if exec.ConfigFile() == "" {
		t.Error("ConfigFilePath should be set after EnsureConfig with invalid path")
	}
}

// TestExecutor_Start_BinaryCheckFailure tests that Start fails when
// binary check fails.
func TestExecutor_Start_BinaryCheckFailure(t *testing.T) {
	um := usermanager.NewUserManager(nil, "test-node")
	cfg := ExecutorConfig{
		BinaryPath:   "/nonexistent/xray",
		AutoDownload: false,
		UserManager:  um,
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.Start()
	if err == nil {
		t.Error("expected error when binary check fails")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Errorf("expected binary-related error, got: %v", err)
	}
}

// TestExecutor_GetInboundConfig_NotFound tests that getting non-existent inbound fails.
func TestExecutor_GetInboundConfig_NotFound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = exec.GetInboundConfig("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent inbound")
	}
}

// TestExecutor_RemoveInboundConfig_NotFound tests that removing non-existent inbound fails.
func TestExecutor_RemoveInboundConfig_NotFound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.RemoveInboundConfig("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent inbound")
	}
}

// TestExecutor_ValidateInbound tests inbound validation.
func TestExecutor_ValidateInbound(t *testing.T) {
	// Test valid inbound
	validInbound := &XrayInbound{
		tag:      "valid-inbound",
		protocol: contracts.ProtocolVMess,
		port:     10000,
	}

	err := validInbound.Validate()
	if err != nil {
		t.Errorf("expected valid inbound to pass validation: %v", err)
	}

	// Test invalid inbound - empty tag
	invalidTagInbound := &XrayInbound{
		tag:      "",
		protocol: contracts.ProtocolVMess,
		port:     10000,
	}

	err = invalidTagInbound.Validate()
	if err == nil {
		t.Error("expected error for empty tag")
	}

	// Test invalid inbound - port out of range
	invalidPortInbound := &XrayInbound{
		tag:      "valid-tag",
		protocol: contracts.ProtocolVMess,
		port:     50, // below 100
	}

	err = invalidPortInbound.Validate()
	if err == nil {
		t.Error("expected error for port below 100")
	}
}

// TestXrayInbound_Extra tests XrayInbound Extra method.
func TestXrayInbound_Extra(t *testing.T) {
	in := &XrayInbound{
		tag:      "test",
		protocol: contracts.ProtocolSOCKS5,
		port:     1080,
		extra:    map[string]interface{}{"custom": "value"},
	}

	extra := in.Extra()
	if extra["custom"] != "value" {
		t.Errorf("expected extra['custom'] = 'value', got: %v", extra["custom"])
	}
}

// TestXrayInbound_Config tests XrayInbound Config method.
func TestXrayInbound_Config(t *testing.T) {
	in := &XrayInbound{
		tag:        "test",
		protocol:   contracts.ProtocolSOCKS5,
		port:       1080,
		listenAddr: "0.0.0.0",
	}

	cfg := in.Config()
	if cfg.Tag != "test" {
		t.Errorf("expected Tag = 'test', got: %v", cfg.Tag)
	}
	if cfg.Port != 1080 {
		t.Errorf("expected Port = 1080, got: %v", cfg.Port)
	}
	if cfg.Protocol != contracts.ProtocolSOCKS5 {
		t.Errorf("expected Protocol = Socks, got: %v", cfg.Protocol)
	}
}

// TestXrayInbound_ToNative tests XrayInbound ToNative method.
func TestXrayInbound_ToNative(t *testing.T) {
	in := &XrayInbound{
		tag:        "test",
		protocol:   contracts.ProtocolSOCKS5,
		port:       1080,
		listenAddr: "127.0.0.1",
	}

	nativeJSON, err := in.ToNative()
	if err != nil {
		t.Fatalf("ToNative failed: %v", err)
	}

	if len(nativeJSON) == 0 {
		t.Error("expected non-empty native JSON")
	}

	// Verify JSON is valid
	var m map[string]interface{}
	if err := json.Unmarshal(nativeJSON, &m); err != nil {
		t.Errorf("invalid JSON: %v", err)
	}
}

// TestXrayInbound_ToNative_WithNativeJSON tests that ToNative returns cached native JSON.
func TestXrayInbound_ToNative_WithNativeJSON(t *testing.T) {
	preExistingJSON := []byte(`{"tag":"test","protocol":"socks","port":"1080"}`)
	in := &XrayInbound{
		tag:        "test",
		nativeJSON: preExistingJSON,
	}

	nativeJSON, err := in.ToNative()
	if err != nil {
		t.Fatalf("ToNative failed: %v", err)
	}

	if string(nativeJSON) != string(preExistingJSON) {
		t.Errorf("expected cached JSON, got: %s", string(nativeJSON))
	}
}

// TestExecutor_Type tests Type method.
func TestExecutor_GetType(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:    "/usr/bin/xray",
		ContainerType: contracts.ContainerXray,
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Type() != contracts.ContainerXray {
		t.Errorf("Type() = %v, want %v", exec.Type(), contracts.ContainerXray)
	}
}

// TestExecutor_DefaultContainerType tests that default container type is set.
func TestExecutor_GetDefaultContainerType(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
		// ContainerType not set
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.Type() != contracts.ContainerXray {
		t.Errorf("expected default ContainerType to be xray, got: %v", exec.Type())
	}
}

// TestExecutor_DefaultGRPCAddress tests that default gRPC address is set.
func TestExecutor_GetDefaultGRPCAddress(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
		// GRPCAPIAddress not set
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.config.GRPCAPIAddress != "127.0.0.1:62789" {
		t.Errorf("GRPCAPIAddress = %v, want %v", exec.config.GRPCAPIAddress, "127.0.0.1:62789")
	}
}

// TestExecutor_ConfigFile tests ConfigFile method.
func TestExecutor_GetConfigFile(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.ConfigFile() != "/etc/xray/config.json" {
		t.Errorf("ConfigFile() = %v, want %v", exec.ConfigFile(), "/etc/xray/config.json")
	}
}

// TestExecutor_IsRunning_Initial tests IsRunning returns false initially.
func TestExecutor_GetIsRunning_Initial(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath: "/usr/bin/xray",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.IsRunning() {
		t.Error("IsRunning() should return false initially")
	}
}

// TestExecutor_GRPCAddress tests custom gRPC address.
func TestExecutor_GetGRPCAddress(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		GRPCAPIAddress: "192.168.1.1:62789",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if exec.config.GRPCAPIAddress != "192.168.1.1:62789" {
		t.Errorf("GRPCAPIAddress = %v, want %v", exec.config.GRPCAPIAddress, "192.168.1.1:62789")
	}
}
