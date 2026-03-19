// Package xray provides Xray container implementation.
package xray

import (
	"testing"

	certmgmtdomain "github.com/lureiny/v2raymg/pkg/certmgmt/domain"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
)

func TestFastAddInbound_TagRequired(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test empty tag
	err = exec.FastAddInbound("", map[string]any{"protocol": "vmess"})
	if err == nil {
		t.Fatal("expected error for empty tag")
	}

	// Should return FastAddInboundFailed error
	if !errs.HasCode(err, errs.ErrFastAddInboundFailed) {
		t.Errorf("expected ErrFastAddInboundFailed, got: %v", err)
	}
}

func TestFastAddInbound_ProtocolRequired(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test missing protocol
	err = exec.FastAddInbound("test-inbound", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing protocol")
	}

	// Should return ProtocolRequired error
	if !errs.HasCode(err, errs.ErrFastAddInboundProtocolRequired) {
		t.Errorf("expected ErrFastAddInboundProtocolRequired, got: %v", err)
	}
}

func TestFastAddInbound_InvalidProtocol(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test invalid protocol
	err = exec.FastAddInbound("test-inbound", map[string]any{"protocol": "invalid"})
	if err == nil {
		t.Fatal("expected error for invalid protocol")
	}

	// Should return FastAddInboundFailed error
	if !errs.HasCode(err, errs.ErrFastAddInboundFailed) {
		t.Errorf("expected ErrFastAddInboundFailed, got: %v", err)
	}
}

func TestFastAddInbound_Shadowsocks_MissingParams(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test shadowsocks without method - should now use default method
	err = exec.FastAddInbound("test-ss-default", map[string]any{
		"protocol": "shadowsocks",
	})
	// Now succeeds with auto-generated method and password
	if err != nil {
		t.Errorf("expected success with auto-generated method/password, got: %v", err)
	}

	// Test shadowsocks without password - should now auto-generate
	err = exec.FastAddInbound("test-ss-nopwd", map[string]any{
		"protocol": "shadowsocks",
		"method":   "aes-256-gcm",
	})
	// Now succeeds with auto-generated password
	if err != nil {
		t.Errorf("expected success with auto-generated password, got: %v", err)
	}
}

func TestFastAddInbound_Shadowsocks_InvalidMethod(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test invalid shadowsocks method
	err = exec.FastAddInbound("test-ss", map[string]any{
		"protocol": "shadowsocks",
		"method":   "invalid-method",
		"password": "test-password",
	})
	if err == nil {
		t.Fatal("expected error for invalid shadowsocks method")
	}

	// Should return FastAddInboundFailed error
	if !errs.HasCode(err, errs.ErrFastAddInboundFailed) {
		t.Errorf("expected ErrFastAddInboundFailed, got: %v", err)
	}
}

func TestFastAddInbound_Success_VMess(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test successful vmess inbound
	err = exec.FastAddInbound("test-vmess", map[string]any{
		"protocol": "vmess",
		"port":     uint32(10001),
		"security": "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added
	in, err := exec.GetInboundConfig("test-vmess")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	if in.Protocol() != contracts.ProtocolVMess {
		t.Errorf("protocol = %v, want %v", in.Protocol(), contracts.ProtocolVMess)
	}

	if in.Port() != 10001 {
		t.Errorf("port = %v, want %v", in.Port(), 10001)
	}

	if in.ListenAddr() != "0.0.0.0" {
		t.Errorf("listen = %v, want %v", in.ListenAddr(), "0.0.0.0")
	}
}

func TestFastAddInbound_Success_VLess(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test successful vless inbound
	err = exec.FastAddInbound("test-vless", map[string]any{
		"protocol": "vless",
		"security": "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added
	in, err := exec.GetInboundConfig("test-vless")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	if in.Protocol() != contracts.ProtocolVLess {
		t.Errorf("protocol = %v, want %v", in.Protocol(), contracts.ProtocolVLess)
	}
}

func TestFastAddInbound_Success_Trojan(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test successful trojan inbound (auto-generates password)
	err = exec.FastAddInbound("test-trojan", map[string]any{
		"protocol": "trojan",
		"security": "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added
	in, err := exec.GetInboundConfig("test-trojan")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	if in.Protocol() != contracts.ProtocolTrojan {
		t.Errorf("protocol = %v, want %v", in.Protocol(), contracts.ProtocolTrojan)
	}
}

func TestFastAddInbound_Success_Shadowsocks(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test successful shadowsocks inbound
	err = exec.FastAddInbound("test-ss", map[string]any{
		"protocol": "shadowsocks",
		"method":   "aes-256-gcm",
		"password": "test-password-123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added
	in, err := exec.GetInboundConfig("test-ss")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	if in.Protocol() != contracts.ProtocolShadowsocks {
		t.Errorf("protocol = %v, want %v", in.Protocol(), contracts.ProtocolShadowsocks)
	}
}

func TestFastAddInbound_DefaultValues(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test with minimal params - should use defaults (self_signed to satisfy TLS cert requirement)
	err = exec.FastAddInbound("test-defaults", map[string]any{
		"protocol":    "vmess",
		"self_signed": true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound with defaults
	in, err := exec.GetInboundConfig("test-defaults")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	// Port should be auto-generated (between 4000-5000)
	if in.Port() < 4000 || in.Port() > 5000 {
		t.Errorf("port = %v, expected auto-generated in range 4000-5000", in.Port())
	}

	// Cast to XrayInbound to check security and transport
	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}

	// Security should default to TLS for non-SS
	if xrayIn.Security() != contracts.SecurityTLS {
		t.Errorf("security = %v, want %v (default for non-ss)", xrayIn.Security(), contracts.SecurityTLS)
	}

	// Transport should default to TCP
	if xrayIn.Transport() != contracts.TransportTCP {
		t.Errorf("transport = %v, want %v (default)", xrayIn.Transport(), contracts.TransportTCP)
	}

	// Listen should default to 0.0.0.0
	if in.ListenAddr() != "0.0.0.0" {
		t.Errorf("listen = %v, want %v (default)", in.ListenAddr(), "0.0.0.0")
	}
}

func TestFastAddInbound_SecurityDefaultForSS(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test shadowsocks - security should default to none
	err = exec.FastAddInbound("test-ss-default", map[string]any{
		"protocol": "shadowsocks",
		"method":   "aes-256-gcm",
		"password": "test-password",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in, err := exec.GetInboundConfig("test-ss-default")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	// Cast to XrayInbound to check security
	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}

	// Security should default to none for SS
	if xrayIn.Security() != contracts.SecurityNone {
		t.Errorf("security = %v, want %v (default for shadowsocks)", xrayIn.Security(), contracts.SecurityNone)
	}
}

func TestFastAddInbound_OverrideDefaults(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test overriding defaults
	err = exec.FastAddInbound("test-override", map[string]any{
		"protocol":  "vmess",
		"port":      uint32(20000),
		"listen":    "127.0.0.1",
		"security":  "none",
		"transport": "ws",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	in, err := exec.GetInboundConfig("test-override")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	if in.Port() != 20000 {
		t.Errorf("port = %v, want %v", in.Port(), 20000)
	}
	if in.ListenAddr() != "127.0.0.1" {
		t.Errorf("listen = %v, want %v", in.ListenAddr(), "127.0.0.1")
	}

	// Cast to XrayInbound to check security and transport
	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}

	if xrayIn.Security() != contracts.SecurityNone {
		t.Errorf("security = %v, want %v", xrayIn.Security(), contracts.SecurityNone)
	}
	if xrayIn.Transport() != contracts.TransportWS {
		t.Errorf("transport = %v, want %v", xrayIn.Transport(), contracts.TransportWS)
	}
}

func TestFastAddInbound_DuplicateTag(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Add first inbound
	err = exec.FastAddInbound("test-dup", map[string]any{
		"protocol": "vmess",
		"security": "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Try to add duplicate
	err = exec.FastAddInbound("test-dup", map[string]any{
		"protocol": "vmess",
		"security": "none",
	})
	if err == nil {
		t.Fatal("expected error for duplicate tag")
	}

	// Should return InboundAlreadyExists error
	if !errs.HasCode(err, errs.ErrInboundAlreadyExists) {
		t.Errorf("expected ErrInboundAlreadyExists, got: %v", err)
	}
}

func TestFastAddInbound_PortValidation(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test with port below valid range
	err = exec.FastAddInbound("test-port-low", map[string]any{
		"protocol": "vmess",
		"port":     uint32(50),
	})
	if err == nil {
		t.Fatal("expected error for port below valid range")
	}

	// Test with port above valid range
	err = exec.FastAddInbound("test-port-high", map[string]any{
		"protocol": "vmess",
		"port":     uint32(70000),
	})
	if err == nil {
		t.Fatal("expected error for port above valid range")
	}
}

func TestFastAddInbound_ListInbound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Add multiple inbounds
	_ = exec.FastAddInbound("test-1", map[string]any{"protocol": "vmess", "security": "none"})
	_ = exec.FastAddInbound("test-2", map[string]any{"protocol": "vless", "security": "none"})
	_ = exec.FastAddInbound("test-3", map[string]any{
		"protocol": "shadowsocks",
		"method":   "aes-256-gcm",
		"password": "test",
	})

	// List all inbounds
	inbounds := exec.ListInboundConfigs()
	if len(inbounds) != 3 {
		t.Errorf("expected 3 inbounds, got %d", len(inbounds))
	}

	// Verify tags
	tags := make(map[string]bool)
	for _, in := range inbounds {
		tags[in.Tag()] = true
	}

	if !tags["test-1"] || !tags["test-2"] || !tags["test-3"] {
		t.Error("expected all tags to be present")
	}
}

func TestFastAddInbound_TLS_RequiresCert(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No cert params, no domain, no self_signed → must return ErrCertRequired
	err = exec.FastAddInbound("test-tls-no-cert", map[string]any{
		"protocol": "vmess",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errs.HasCode(err, errs.ErrCertRequired) {
		t.Errorf("expected ErrCertRequired, got: %v", err)
	}
}

func TestFastAddInbound_TLS_CustomCert(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test with custom certificate
	err = exec.FastAddInbound("test-tls-custom", map[string]any{
		"protocol":        "vmess",
		"security":        "tls",
		"certificateFile": "/tmp/custom-cert.pem",
		"keyFile":         "/tmp/custom-key.pem",
		"server_name":     "example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added
	in, err := exec.GetInboundConfig("test-tls-custom")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}
	if xrayIn.Security() != contracts.SecurityTLS {
		t.Errorf("security = %v, want %v", xrayIn.Security(), contracts.SecurityTLS)
	}
}

func TestFastAddInbound_TLS_ExplicitNone(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test with security=none - should NOT generate cert
	err = exec.FastAddInbound("test-no-tls", map[string]any{
		"protocol": "vmess",
		"security": "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added with no security
	in, err := exec.GetInboundConfig("test-no-tls")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}
	if xrayIn.Security() != contracts.SecurityNone {
		t.Errorf("security = %v, want %v", xrayIn.Security(), contracts.SecurityNone)
	}
}

func TestFastAddInbound_SS_NoTLSRequired(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}

	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test Shadowsocks - should default to security=none, no cert needed
	err = exec.FastAddInbound("test-ss-no-tls", map[string]any{
		"protocol": "shadowsocks",
		"method":   "aes-256-gcm",
		"password": "test-password",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify inbound was added with no security (not TLS)
	in, err := exec.GetInboundConfig("test-ss-no-tls")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}

	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}
	if xrayIn.Security() != contracts.SecurityNone {
		t.Errorf("security = %v, want %v (SS should default to none)", xrayIn.Security(), contracts.SecurityNone)
	}
}

// mockCertManager implements CertManagerGetter for testing.
type mockCertManager struct {
	record *certmgmtdomain.CertificateRecord
}

func (m *mockCertManager) GetCert(domain string) *certmgmtdomain.CertificateRecord {
	return m.record
}

func TestFastAddInbound_TLS_SelfSigned(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.FastAddInbound("test-tls-selfsigned", map[string]any{
		"protocol":    "vmess",
		"self_signed": true,
	})
	if err != nil {
		t.Fatalf("expected success with self_signed=true, got: %v", err)
	}

	in, err := exec.GetInboundConfig("test-tls-selfsigned")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}
	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}
	if xrayIn.Security() != contracts.SecurityTLS {
		t.Errorf("security = %v, want %v", xrayIn.Security(), contracts.SecurityTLS)
	}
}

func TestFastAddInbound_TLS_DomainNoCertManager(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// domain provided but no certManager set → ErrCertRequired
	err = exec.FastAddInbound("test-tls-domain-nocm", map[string]any{
		"protocol": "vmess",
		"domain":   "example.com",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errs.HasCode(err, errs.ErrCertRequired) {
		t.Errorf("expected ErrCertRequired, got: %v", err)
	}
}

func TestFastAddInbound_TLS_DomainCertNotFound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
		CertManager:    &mockCertManager{record: nil},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.FastAddInbound("test-tls-domain-notfound", map[string]any{
		"protocol": "vmess",
		"domain":   "example.com",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errs.HasCode(err, errs.ErrCertNotFound) {
		t.Errorf("expected ErrCertNotFound, got: %v", err)
	}
}

func TestFastAddInbound_TLS_DomainCertFound(t *testing.T) {
	cfg := ExecutorConfig{
		BinaryPath:     "/usr/bin/xray",
		ConfigFilePath: "/etc/xray/config.json",
		CertManager: &mockCertManager{
			record: &certmgmtdomain.CertificateRecord{
				Domain:   "example.com",
				CertFile: "/tmp/example.com.crt",
				KeyFile:  "/tmp/example.com.key",
			},
		},
	}
	exec, err := NewExecutor(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = exec.FastAddInbound("test-tls-domain-found", map[string]any{
		"protocol": "vmess",
		"domain":   "example.com",
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	in, err := exec.GetInboundConfig("test-tls-domain-found")
	if err != nil {
		t.Fatalf("failed to get inbound: %v", err)
	}
	xrayIn, ok := in.(*XrayInbound)
	if !ok {
		t.Fatal("failed to cast to XrayInbound")
	}
	if xrayIn.Security() != contracts.SecurityTLS {
		t.Errorf("security = %v, want %v", xrayIn.Security(), contracts.SecurityTLS)
	}
	// Verify cert paths were injected (profilegen writes underscore keys)
	if xrayIn.extra["cert_file"] != "/tmp/example.com.crt" {
		t.Errorf("cert_file = %v, want /tmp/example.com.crt", xrayIn.extra["cert_file"])
	}
	if xrayIn.extra["key_file"] != "/tmp/example.com.key" {
		t.Errorf("key_file = %v, want /tmp/example.com.key", xrayIn.extra["key_file"])
	}
}
