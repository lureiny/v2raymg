package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

func TestGetUserSubscriptions_SS_Plain_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "ss-plain", map[string]any{
		"protocol": "shadowsocks",
		"port":     20400,
		"password": "supersecret",
		"cipher":   "2022-blake3-aes-256-gcm",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]
	if spec.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol = %q, want shadowsocks", spec.Protocol)
	}
	if spec.Password != "supersecret" {
		t.Errorf("Password = %q", spec.Password)
	}
	if spec.Extensions["method"] != "2022-blake3-aes-256-gcm" {
		t.Errorf("Extensions[method] = %q", spec.Extensions["method"])
	}
	node, err := codec.DecodeShadowsocks(spec.URI)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if node.Method != "2022-blake3-aes-256-gcm" {
		t.Errorf("URI method = %q", node.Method)
	}
	if node.Password != "supersecret" {
		t.Errorf("URI password = %q", node.Password)
	}
}

func TestGetUserSubscriptions_SS_ObfsPlugin_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("bob", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "bob", "ss-obfs", map[string]any{
		"protocol":    "shadowsocks",
		"port":        20401,
		"password":    "obfssecret",
		"cipher":      "aes-256-gcm",
		"plugin":      "obfs",
		"plugin_mode": "http",
		"plugin_host": "cdn.example.com",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "bob"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]
	if spec.Extensions["plugin"] != "obfs" {
		t.Errorf("Extensions[plugin] = %q, want obfs", spec.Extensions["plugin"])
	}
	if spec.Extensions["plugin_mode"] != "http" {
		t.Errorf("Extensions[plugin_mode] = %q, want http", spec.Extensions["plugin_mode"])
	}
	if spec.Extensions["plugin_host"] != "cdn.example.com" {
		t.Errorf("Extensions[plugin_host] = %q", spec.Extensions["plugin_host"])
	}
	node, err := codec.DecodeShadowsocks(spec.URI)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if node.Plugin != "obfs" {
		t.Errorf("URI plugin = %q, want obfs", node.Plugin)
	}
	if node.PluginOpts["mode"] != "http" {
		t.Errorf("URI plugin-opts mode = %q", node.PluginOpts["mode"])
	}
}

func TestGetUserSubscriptions_SS_ShadowTLS_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("carol", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "carol", "ss-stls", map[string]any{
		"protocol":        "shadowsocks",
		"port":            20402,
		"password":        "stlssecret",
		"cipher":          "aes-256-gcm",
		"plugin":          "shadow-tls",
		"plugin_host":     "www.microsoft.com",
		"plugin_password": "stls-pw",
		"plugin_version":  "3",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "carol"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	spec := specs[0]
	if spec.Extensions["plugin"] != "shadow-tls" {
		t.Errorf("Extensions[plugin] = %q, want shadow-tls", spec.Extensions["plugin"])
	}
	if spec.Extensions["plugin_host"] != "www.microsoft.com" {
		t.Errorf("Extensions[plugin_host] = %q", spec.Extensions["plugin_host"])
	}
	if spec.Extensions["plugin_password"] != "stls-pw" {
		t.Errorf("Extensions[plugin_password] = %q", spec.Extensions["plugin_password"])
	}
	if spec.Extensions["plugin_version"] != "3" {
		t.Errorf("Extensions[plugin_version] = %q", spec.Extensions["plugin_version"])
	}
}

func TestGetUserSubscriptions_SS_V2RayPlugin_TLS_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("dave", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "dave", "ss-v2ray", map[string]any{
		"protocol":    "shadowsocks",
		"port":        20403,
		"password":    "v2raysecret",
		"cipher":      "aes-256-gcm",
		"plugin":      "v2ray-plugin",
		"plugin_mode": "websocket",
		"plugin_tls":  true,
		"plugin_path": "/v2ray",
		"plugin_host": "cdn.example.com",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "dave"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]
	if spec.Extensions["plugin"] != "v2ray-plugin" {
		t.Errorf("Extensions[plugin] = %q, want v2ray-plugin", spec.Extensions["plugin"])
	}
	if spec.Extensions["plugin_tls"] != true {
		t.Errorf("Extensions[plugin_tls] = %v, want true", spec.Extensions["plugin_tls"])
	}
	if spec.Extensions["plugin_mode"] != "websocket" {
		t.Errorf("Extensions[plugin_mode] = %q, want websocket", spec.Extensions["plugin_mode"])
	}
	if spec.Extensions["plugin_path"] != "/v2ray" {
		t.Errorf("Extensions[plugin_path] = %q, want /v2ray", spec.Extensions["plugin_path"])
	}
}

func TestFillSSSubscriptionSpec_Legacy_SharedCred(t *testing.T) {
	// Legacy path: inbound has no ProtocolParams (pre-Phase-4 record).
	inb := NewMihomoInbound("ss-legacy", contracts.ProtocolShadowsocks, 8388,
		MihomoSharedCred{Password: "legacypw", Cipher: "aes-128-gcm"})

	spec := contracts.SubscriptionSpec{
		Host:       "vps.example.com",
		Port:       8388,
		NodeName:   "ss-legacy",
		InboundTag: inb.Tag(),
		Extensions: map[string]any{},
	}
	if err := fillSSSubscriptionSpec(&spec, inb); err != nil {
		t.Fatalf("fillSSSubscriptionSpec legacy: %v", err)
	}
	if spec.Extensions["method"] != "aes-128-gcm" {
		t.Errorf("legacy Extensions[method] = %q", spec.Extensions["method"])
	}
	if _, ok := spec.Extensions["plugin"]; ok {
		t.Error("legacy path should not emit plugin extension")
	}
	if spec.Password != "legacypw" {
		t.Errorf("legacy Password = %q", spec.Password)
	}
}

func TestFillSSSubscriptionSpec_Legacy_DefaultCipher(t *testing.T) {
	// Legacy path without cipher: should default to defaultSSMethod.
	inb := NewMihomoInbound("ss-nomethod", contracts.ProtocolShadowsocks, 8388,
		MihomoSharedCred{Password: "pw"})

	spec := contracts.SubscriptionSpec{
		Host:       "vps.example.com",
		Port:       8388,
		NodeName:   "ss-nomethod",
		InboundTag: inb.Tag(),
		Extensions: map[string]any{},
	}
	if err := fillSSSubscriptionSpec(&spec, inb); err != nil {
		t.Fatalf("fillSSSubscriptionSpec: %v", err)
	}
	if spec.Extensions["method"] != defaultSSMethod {
		t.Errorf("method = %q, want %q", spec.Extensions["method"], defaultSSMethod)
	}
}
