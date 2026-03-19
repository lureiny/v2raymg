package xray

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestAdapter_ToProvider_ValidSpec(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-in",
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport": "tcp",
			"security":  "none",
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(native.JSON) == 0 {
		t.Error("expected non-empty JSON")
	}

	// Verify the JSON contains expected fields
	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["protocol"] != "vmess" {
		t.Errorf("expected protocol vmess, got %v", m["protocol"])
	}
	if m["tag"] != "test-in" {
		t.Errorf("expected tag test-in, got %v", m["tag"])
	}
}

func TestAdapter_ToProvider_WithTLS(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-tls",
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport":   "tcp",
			"security":    "tls",
			"server_name": "example.com",
			"alpn":        []string{"h2", "http/1.1"},
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}
	if streamSettings["security"] != "tls" {
		t.Errorf("expected security tls, got %v", streamSettings["security"])
	}
}

func TestAdapter_ToProvider_WithWebSocket(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-ws",
		Port:     8080,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport": "ws",
			"security":  "none",
			"ws_path":   "/ws",
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}
	if streamSettings["network"] != "ws" {
		t.Errorf("expected network ws, got %v", streamSettings["network"])
	}
	wsSettings, ok := streamSettings["wsSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected wsSettings")
	}
	if wsSettings["path"] != "/ws" {
		t.Errorf("expected path /ws, got %v", wsSettings["path"])
	}
}

func TestAdapter_ToProvider_InvalidSpec(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "", // Invalid: empty tag
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport": "tcp",
			"security":  "none",
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestAdapter_ValidateInbound(t *testing.T) {
	adapter := NewAdapter()

	// Valid spec
	spec := contracts.InboundSpec{
		Tag:      "test",
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport": "tcp",
			"security":  "none",
		},
	}

	if err := adapter.ValidateInbound(spec); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Invalid spec
	spec.Tag = ""
	if err := adapter.ValidateInbound(spec); err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

func TestAdapter_MapUsers(t *testing.T) {
	adapter := NewAdapter()
	users := []contracts.UserSpec{
		{Username: "user1@example.com", Extensions: map[string]any{"uuid": "uuid1"}},
		{Username: "user2@example.com", Extensions: map[string]any{"uuid": "uuid2"}},
	}

	userMaps, err := adapter.MapUsers(users, contracts.ProtocolVMess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(userMaps) != 2 {
		t.Errorf("expected 2 users, got %d", len(userMaps))
	}

	if userMaps[0]["email"] != "user1@example.com" {
		t.Errorf("expected email user1@example.com, got %v", userMaps[0]["email"])
	}
}

func TestAdapter_FromProvider(t *testing.T) {
	adapter := NewAdapter()
	native := NativeInbound{JSON: []byte(`{
		"tag": "test",
		"port": "443",
		"listen": "0.0.0.0",
		"protocol": "vmess",
		"streamSettings": {
			"network": "ws",
			"security": "tls"
		},
		"sniffing": {
			"enabled": true
		}
	}`)}

	spec, warnings, err := adapter.FromProvider(native)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.Tag != "test" {
		t.Errorf("expected tag test, got %v", spec.Tag)
	}
	if spec.Port != 443 {
		t.Errorf("expected port 443, got %d", spec.Port)
	}
	if spec.Protocol != contracts.ProtocolVMess {
		t.Errorf("expected protocol vmess, got %v", spec.Protocol)
	}
	// Check extensions
	if transport, _ := spec.Extensions["transport"].(string); transport != "ws" {
		t.Errorf("expected transport ws, got %v", transport)
	}
	if security, _ := spec.Extensions["security"].(string); security != "tls" {
		t.Errorf("expected security tls, got %v", security)
	}

	// Warnings should be empty for known fields
	if !warnings.Empty() {
		t.Logf("warnings: %v", warnings)
	}
}

func TestAdapter_ToProvider_WithSniffingAdvanced(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-sniffing",
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport":                 "tcp",
			"sniffing_enabled":          true,
			"sniffing_dest_override":    []string{"http", "tls", "quic"},
			"sniffing_metadata_only":    true,
			"sniffing_domains_excluded": []string{"example.com", "test.com"},
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify sniffing config
	sniffing, ok := m["sniffing"].(map[string]interface{})
	if !ok {
		t.Fatal("expected sniffing config")
	}
	if enabled, ok := sniffing["enabled"].(bool); !ok || !enabled {
		t.Errorf("expected enabled=true, got %v", enabled)
	}
	if destOverride, ok := sniffing["destOverride"].([]interface{}); !ok || len(destOverride) != 3 {
		t.Errorf("expected 3 destOverride, got %v", destOverride)
	}
	if metadataOnly, ok := sniffing["metadataOnly"].(bool); !ok || !metadataOnly {
		t.Errorf("expected metadataOnly=true, got %v", metadataOnly)
	}
	if domainsExcluded, ok := sniffing["domainsExcluded"].([]interface{}); !ok || len(domainsExcluded) != 2 {
		t.Errorf("expected 2 domainsExcluded, got %v", domainsExcluded)
	}
}

func TestAdapter_ToProvider_WithReality(t *testing.T) {
	adapter := NewAdapter()
	// Generate a valid key pair for testing
	privateKey, publicKey, err := GenerateRealityKeyPairWithPublic()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	spec := contracts.InboundSpec{
		Tag:      "test-reality",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":              "tcp",
			"security":               "reality",
			"reality_target":         "www.microsoft.com:443",
			"reality_server_names":   []string{"www.microsoft.com", "www.apple.com"},
			"reality_short_ids":      []string{"0123456789abcdef"},
			"reality_private_key":    privateKey,
			"reality_public_key":     publicKey,
			"reality_show":           true,
			"reality_xver":           2,
			"reality_max_client_ver": "1.0.0",
			"reality_min_client_ver": "0.1.0",
			"reality_max_time_diff":  int64(60000),
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}
	if streamSettings["security"] != "reality" {
		t.Errorf("expected security reality, got %v", streamSettings["security"])
	}

	realitySettings, ok := streamSettings["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected realitySettings")
	}

	// Verify required fields
	if target, ok := realitySettings["target"].(string); !ok || target != "www.microsoft.com:443" {
		t.Errorf("expected target www.microsoft.com:443, got %v", target)
	}
	if serverNames, ok := realitySettings["serverNames"].([]interface{}); !ok || len(serverNames) != 2 {
		t.Errorf("expected 2 serverNames, got %v", serverNames)
	} else {
		if serverNames[0] != "www.microsoft.com" {
			t.Errorf("expected serverNames[0] www.microsoft.com, got %v", serverNames[0])
		}
	}
	if shortIds, ok := realitySettings["shortIds"].([]interface{}); !ok || len(shortIds) != 1 {
		t.Errorf("expected 1 shortId, got %v", shortIds)
	}
	if privateKey, ok := realitySettings["privateKey"].(string); !ok || privateKey == "" {
		t.Error("expected privateKey")
	}
	// Verify optional fields
	if show, ok := realitySettings["show"].(bool); !ok || !show {
		t.Errorf("expected show=true, got %v", show)
	}
	if xver, ok := realitySettings["xver"].(float64); !ok || xver != 2 {
		t.Errorf("expected xver=2, got %v", xver)
	}
}

func TestAdapter_ToProvider_RealityWithVMess_ShouldFail(t *testing.T) {
	adapter := NewAdapter()
	// Reality with VMess should fail - only VLESS is supported
	spec := contracts.InboundSpec{
		Tag:      "test-reality-vmess",
		Port:     443,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for Reality with VMess")
	}
	// Verify the error message
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
}

func TestAdapter_ToProvider_RealityWithTrojan_ShouldFail(t *testing.T) {
	adapter := NewAdapter()
	// Reality with Trojan should fail - only VLESS is supported
	spec := contracts.InboundSpec{
		Tag:      "test-reality-trojan",
		Port:     443,
		Protocol: contracts.ProtocolTrojan,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for Reality with Trojan")
	}
	// Verify the error message
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
}

func TestAdapter_ToProvider_RealityWithInvalidShortIdLength(t *testing.T) {
	adapter := NewAdapter()
	// Use 12 hex characters (6 bytes) - this should fail validation
	spec := contracts.InboundSpec{
		Tag:      "test-reality-invalid-shortid",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_short_ids":    []string{"0123456789ab"}, // 12 hex - invalid!
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for invalid shortId length, got nil")
	}
	// Verify error message strings.Contains expected details
	errMsg := err.Error()
	if !strings.Contains(errMsg, "reality_short_ids") || !strings.Contains(errMsg, "length") {
		t.Errorf("expected error message to mention 'reality_short_ids' and 'length', got: %v", errMsg)
	}
	t.Logf("Got expected error: %v", err)
}

func TestAdapter_ToProvider_RealityWithInvalidShortIdNonHex(t *testing.T) {
	adapter := NewAdapter()
	// Use non-hex characters - this should fail validation
	spec := contracts.InboundSpec{
		Tag:      "test-reality-invalid-hex",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_short_ids":    []string{"0123456789abcxyz"}, // contains xyz - invalid!
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for non-hex shortId, got nil")
	}
	// Verify error message strings.Contains expected details
	errMsg := err.Error()
	if !strings.Contains(errMsg, "reality_short_ids") || !strings.Contains(errMsg, "non-hex") {
		t.Errorf("expected error message to mention 'reality_short_ids' and 'non-hex', got: %v", errMsg)
	}
	t.Logf("Got expected error: %v", err)
}

func TestAdapter_ToProvider_RealityWithValidBackwardCompatShortId(t *testing.T) {
	adapter := NewAdapter()
	// Use single reality_short_id (backward compat) with valid 16 hex
	spec := contracts.InboundSpec{
		Tag:      "test-reality-backward-compat",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_short_id":     "0123456789abcdef", // 16 hex - valid!
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error for valid backward compat shortId: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}

	realitySettings, ok := streamSettings["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected realitySettings")
	}

	if shortId, ok := realitySettings["shortId"].(string); !ok || shortId != "0123456789abcdef" {
		t.Errorf("expected shortId 0123456789abcdef, got %v", shortId)
	}
}

func TestAdapter_ToProvider_RealityWithInvalidBackwardCompatShortId(t *testing.T) {
	adapter := NewAdapter()
	// Use single reality_short_id (backward compat) with invalid length
	spec := contracts.InboundSpec{
		Tag:      "test-reality-invalid-backward-compat",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_short_id":     "0123456789ab", // 12 hex - invalid!
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error for invalid backward compat shortId, got nil")
	}
	// Verify error message strings.Contains expected details
	errMsg := err.Error()
	if !strings.Contains(errMsg, "reality_short_id") || !strings.Contains(errMsg, "length") {
		t.Errorf("expected error message to mention 'reality_short_id' and 'length', got: %v", errMsg)
	}
	t.Logf("Got expected error: %v", err)
}

func TestGenerateRealityKeyPair(t *testing.T) {
	// Test the key generation function
	privateKey, err := GenerateRealityKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	if len(privateKey) == 0 {
		t.Fatal("expected non-empty private key")
	}
	// The key should be base64 encoded, so length should be ~44 chars (32 bytes -> 44 base64 chars)
	if len(privateKey) != 44 {
		t.Logf("Warning: expected base64 length 44, got %d", len(privateKey))
	}
	t.Logf("Generated private key: %s...", privateKey[:20])
}

func TestGenerateRealityKeyPairWithPublic(t *testing.T) {
	// Test the key generation with public key
	privateKey, publicKey, err := GenerateRealityKeyPairWithPublic()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	if len(privateKey) == 0 || len(publicKey) == 0 {
		t.Fatal("expected non-empty keys")
	}
	t.Logf("Generated private key: %s...", privateKey[:20])
	t.Logf("Generated public key:  %s...", publicKey[:20])
}

// Test Reality key pair policy: external keys must be provided together
func TestAdapter_ToProvider_Reality_OnlyPrivateKey_ShouldFail(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-reality-only-private",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_private_key":  "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234",
			// Missing reality_public_key - should fail
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error when only private key is provided")
	}
	if !strings.Contains(err.Error(), "reality_private_key and reality_public_key must be provided together") {
		t.Errorf("expected error message about providing keys together, got: %v", err)
	}
	t.Logf("Got expected error: %v", err)
}

func TestAdapter_ToProvider_Reality_OnlyPublicKey_ShouldFail(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-reality-only-public",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_public_key":   "ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234ABCD1234",
			// Missing reality_private_key - should fail
		},
	}

	_, err := adapter.ToProvider(spec)
	if err == nil {
		t.Fatal("expected error when only public key is provided")
	}
	if !strings.Contains(err.Error(), "reality_private_key and reality_public_key must be provided together") {
		t.Errorf("expected error message about providing keys together, got: %v", err)
	}
	t.Logf("Got expected error: %v", err)
}

func TestAdapter_ToProvider_Reality_AutoGenerateKeyPair(t *testing.T) {
	adapter := NewAdapter()
	// Do NOT provide any keys - should auto-generate
	spec := contracts.InboundSpec{
		Tag:      "test-reality-auto-gen",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			// No keys provided - should auto-generate
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}

	realitySettings, ok := streamSettings["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected realitySettings")
	}

	// Verify private key was auto-generated
	privateKey, ok := realitySettings["privateKey"].(string)
	if !ok || privateKey == "" {
		t.Fatal("expected auto-generated private key")
	}
	t.Logf("Auto-generated private key: %s...", privateKey[:20])
}

// TestAdapter_ToProvider_Reality_AutoGenerateShortId tests that shortId is auto-generated
// when not provided.
func TestAdapter_ToProvider_Reality_AutoGenerateShortId(t *testing.T) {
	adapter := NewAdapter()
	// Provide keys but NOT shortId - should auto-generate shortId
	privateKey, publicKey, err := GenerateRealityKeyPairWithPublic()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	spec := contracts.InboundSpec{
		Tag:      "test-reality-auto-sid",
		Port:     443,
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"transport":            "tcp",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			// No shortId provided - should auto-generate
			"reality_private_key": privateKey,
			"reality_public_key":  publicKey,
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify shortIds is auto-generated
	streamSettings, ok := m["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected streamSettings")
	}
	realitySettings, ok := streamSettings["realitySettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected realitySettings")
	}
	shortIds, ok := realitySettings["shortIds"].([]interface{})
	if !ok || len(shortIds) != 1 {
		t.Fatalf("expected 1 auto-generated shortId, got %v", shortIds)
	}
	sid, ok := shortIds[0].(string)
	if !ok || len(sid) != 16 {
		t.Fatalf("expected 16-char hex shortId, got %v", sid)
	}
	// Verify it's valid hex
	for _, c := range sid {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			t.Fatalf("expected hex characters, got %c in %s", c, sid)
		}
	}
	t.Logf("Auto-generated shortId: %s", sid)

	// Verify shortId is stored in spec.Extensions for persistence
	if spec.Extensions["reality_short_ids"] == nil {
		t.Fatal("expected reality_short_ids in spec.Extensions for persistence")
	}
}

func TestAdapter_ToProvider_WithFallbacksAdvanced(t *testing.T) {
	adapter := NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "test-fallbacks",
		Port:     443,
		Protocol: contracts.ProtocolTrojan,
		Extensions: map[string]any{
			"transport": "tcp",
			"fallbacks": []FallbackSpec{
				{
					Dest: "80",
					Type: "tcp",
				},
				{
					Dest: "443",
					Alpn: "h2",
					Type: "unix",
				},
				{
					Dest: "/var/run/docker.sock",
					Type: "unix",
					Path: "/proxy",
				},
			},
		},
	}

	native, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify fallbacks config
	fallbacks, ok := m["fallbacks"].([]interface{})
	if !ok {
		t.Fatal("expected fallbacks config")
	}
	if len(fallbacks) != 3 {
		t.Fatalf("expected 3 fallbacks, got %d", len(fallbacks))
	}

	// Check first fallback
	fb1 := fallbacks[0].(map[string]interface{})
	if fb1["dest"] != "80" {
		t.Errorf("expected dest 80, got %v", fb1["dest"])
	}
	if fb1["type"] != "tcp" {
		t.Errorf("expected type tcp, got %v", fb1["type"])
	}

	// Check second fallback with alpn
	fb2 := fallbacks[1].(map[string]interface{})
	if fb2["alpn"] != "h2" {
		t.Errorf("expected alpn h2, got %v", fb2["alpn"])
	}
	if fb2["type"] != "unix" {
		t.Errorf("expected type unix, got %v", fb2["type"])
	}

	// Check third fallback with path
	fb3 := fallbacks[2].(map[string]interface{})
	if fb3["path"] != "/proxy" {
		t.Errorf("expected path /proxy, got %v", fb3["path"])
	}
}
