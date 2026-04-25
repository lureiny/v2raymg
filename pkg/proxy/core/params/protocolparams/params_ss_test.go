package protocolparams

import (
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestParseSS_Basic(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "2022-blake3-aes-256-gcm",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol = %q, want shadowsocks", pp.Protocol)
	}
	if pp.SS == nil {
		t.Fatal("SS is nil")
	}
	if pp.SS.Password != "secret" {
		t.Errorf("Password = %q", pp.SS.Password)
	}
	if pp.SS.Cipher != "2022-blake3-aes-256-gcm" {
		t.Errorf("Cipher = %q", pp.SS.Cipher)
	}
	if !pp.SS.UDP {
		t.Error("UDP should default to true")
	}
	if pp.Transport != nil {
		t.Error("Transport should be nil for SS")
	}
	if pp.Security != nil {
		t.Error("Security should be nil for SS")
	}
}

func TestParseSS_Alias_ss(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol": "ss",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
	})
	if err != nil {
		t.Fatalf("Parse with alias ss: %v", err)
	}
	if pp.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol = %q, want shadowsocks", pp.Protocol)
	}
}

func TestParseSS_UDP_False(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
		"udp":      false,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.UDP {
		t.Error("UDP should be false when explicitly set to false")
	}
}

func TestParseSS_UDP_StringFalse(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
		"udp":      "false",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.UDP {
		t.Error("UDP should be false when set to string false")
	}
}

func TestParseSS_MissingPassword(t *testing.T) {
	_, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"cipher":   "aes-256-gcm",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseSS_MissingCipher(t *testing.T) {
	_, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseSS_Plugin_ObfsLocal(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol":    "shadowsocks",
		"port":        uint32(8388),
		"password":    "secret",
		"cipher":      "aes-256-gcm",
		"plugin":      "obfs",
		"plugin_mode": "http",
		"plugin_host": "cdn.example.com",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.Plugin != "obfs" {
		t.Errorf("Plugin = %q, want obfs", pp.SS.Plugin)
	}
	if pp.SS.PluginOpts["mode"] != "http" {
		t.Errorf("PluginOpts[mode] = %q, want http", pp.SS.PluginOpts["mode"])
	}
	if pp.SS.PluginOpts["host"] != "cdn.example.com" {
		t.Errorf("PluginOpts[host] = %q", pp.SS.PluginOpts["host"])
	}
}

func TestParseSS_Plugin_V2RayPlugin(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol":    "shadowsocks",
		"port":        uint32(8388),
		"password":    "secret",
		"cipher":      "aes-256-gcm",
		"plugin":      "v2ray-plugin",
		"plugin_mode": "websocket",
		"plugin_path": "/ws",
		"plugin_host": "cdn.example.com",
		"plugin_tls":  true,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.Plugin != "v2ray-plugin" {
		t.Errorf("Plugin = %q", pp.SS.Plugin)
	}
	if pp.SS.PluginOpts["mode"] != "websocket" {
		t.Errorf("PluginOpts[mode] = %q", pp.SS.PluginOpts["mode"])
	}
	if pp.SS.PluginOpts["path"] != "/ws" {
		t.Errorf("PluginOpts[path] = %q", pp.SS.PluginOpts["path"])
	}
	if pp.SS.PluginOpts["tls"] != "true" {
		t.Errorf("PluginOpts[tls] = %q, want true", pp.SS.PluginOpts["tls"])
	}
}

func TestParseSS_Plugin_ShadowTLS(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol":        "shadowsocks",
		"port":            uint32(8388),
		"password":        "secret",
		"cipher":          "aes-256-gcm",
		"plugin":          "shadow-tls",
		"plugin_host":     "www.microsoft.com",
		"plugin_password": "shadow-password",
		"plugin_version":  "3",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.Plugin != "shadow-tls" {
		t.Errorf("Plugin = %q", pp.SS.Plugin)
	}
	if pp.SS.PluginOpts["host"] != "www.microsoft.com" {
		t.Errorf("PluginOpts[host] = %q", pp.SS.PluginOpts["host"])
	}
	if pp.SS.PluginOpts["password"] != "shadow-password" {
		t.Errorf("PluginOpts[password] = %q", pp.SS.PluginOpts["password"])
	}
	if pp.SS.PluginOpts["version"] != "3" {
		t.Errorf("PluginOpts[version] = %q", pp.SS.PluginOpts["version"])
	}
}

func TestParseSS_NoPlugin_PluginOptsNil(t *testing.T) {
	pp, err := Parse(map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.SS.Plugin != "" {
		t.Errorf("Plugin should be empty when not set, got %q", pp.SS.Plugin)
	}
	if pp.SS.PluginOpts != nil {
		t.Errorf("PluginOpts should be nil when no plugin, got %v", pp.SS.PluginOpts)
	}
}
