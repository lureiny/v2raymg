package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

func makeSSTCInbound(t *testing.T, tag string, params map[string]any) *MihomoInbound {
	t.Helper()
	pp, err := protocolparams.Parse(params)
	if err != nil {
		t.Fatalf("protocolparams.Parse: %v", err)
	}
	pp.Tag = tag
	inb, err := FromProtocolParams(pp)
	if err != nil {
		t.Fatalf("FromProtocolParams: %v", err)
	}
	return inb
}

func TestFillSSListener_PlainAES(t *testing.T) {
	inb := makeSSTCInbound(t, "ss-plain", map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
	})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener: %v", err)
	}
	if m["password"] != "secret" {
		t.Errorf("password = %q", m["password"])
	}
	if m["cipher"] != "aes-256-gcm" {
		t.Errorf("cipher = %q", m["cipher"])
	}
	if udp, ok := m["udp"].(bool); !ok || !udp {
		t.Errorf("udp = %v, want true", m["udp"])
	}
	if _, ok := m["plugin"]; ok {
		t.Error("plugin should not be set for plain SS")
	}
}

func TestFillSSListener_AEAD2022(t *testing.T) {
	inb := makeSSTCInbound(t, "ss-2022", map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret32byteslong00000000000000",
		"cipher":   "2022-blake3-aes-256-gcm",
	})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener: %v", err)
	}
	if m["cipher"] != "2022-blake3-aes-256-gcm" {
		t.Errorf("cipher = %q", m["cipher"])
	}
}

func TestFillSSListener_UDPFalse(t *testing.T) {
	inb := makeSSTCInbound(t, "ss-noUDP", map[string]any{
		"protocol": "shadowsocks",
		"port":     uint32(8388),
		"password": "secret",
		"cipher":   "aes-256-gcm",
		"udp":      false,
	})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener: %v", err)
	}
	if _, ok := m["udp"]; ok {
		t.Errorf("udp key should be absent when udp=false, got %v", m["udp"])
	}
}

func TestFillSSListener_ObfsPlugin(t *testing.T) {
	inb := makeSSTCInbound(t, "ss-obfs", map[string]any{
		"protocol":    "shadowsocks",
		"port":        uint32(8388),
		"password":    "secret",
		"cipher":      "aes-256-gcm",
		"plugin":      "obfs",
		"plugin_mode": "http",
		"plugin_host": "cdn.example.com",
	})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener: %v", err)
	}
	if m["plugin"] != "obfs" {
		t.Errorf("plugin = %q, want obfs", m["plugin"])
	}
	opts, ok := m["plugin-opts"].(map[string]any)
	if !ok {
		t.Fatalf("plugin-opts = %T, want map[string]any", m["plugin-opts"])
	}
	if opts["mode"] != "http" {
		t.Errorf("plugin-opts mode = %q", opts["mode"])
	}
	if opts["host"] != "cdn.example.com" {
		t.Errorf("plugin-opts host = %q", opts["host"])
	}
}

func TestFillSSListener_ShadowTLS_SkipsPlugin(t *testing.T) {
	// shadow-tls is a separate proxy layer; the mihomo SS listener
	// should NOT emit plugin/plugin-opts for it.
	inb := makeSSTCInbound(t, "ss-stls", map[string]any{
		"protocol":        "shadowsocks",
		"port":            uint32(8388),
		"password":        "secret",
		"cipher":          "aes-256-gcm",
		"plugin":          "shadow-tls",
		"plugin_host":     "www.microsoft.com",
		"plugin_password": "shadow-pw",
		"plugin_version":  "3",
	})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener: %v", err)
	}
	if _, ok := m["plugin"]; ok {
		t.Errorf("shadow-tls should not emit plugin in mihomo listener, got plugin=%v", m["plugin"])
	}
}

func TestFillSSListener_Legacy_SharedCred(t *testing.T) {
	inb := NewMihomoInbound("ss-legacy", contracts.ProtocolShadowsocks, 8388,
		MihomoSharedCred{Password: "pw", Cipher: "aes-128-gcm"})
	m := map[string]any{}
	if err := fillSSListener(m, inb); err != nil {
		t.Fatalf("fillSSListener legacy: %v", err)
	}
	if m["password"] != "pw" {
		t.Errorf("legacy password = %q", m["password"])
	}
	if m["cipher"] != "aes-128-gcm" {
		t.Errorf("legacy cipher = %q", m["cipher"])
	}
	if _, ok := m["udp"]; ok {
		t.Error("legacy path should not emit udp key")
	}
}
