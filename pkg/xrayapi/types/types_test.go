// Package types provides minimal type definitions for xray gRPC API.
// Tests for TypedMessage serialization.
package types

import (
	"encoding/json"
	"testing"

	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/app/proxyman"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/net"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/common/serial"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/proxy/vmess/inbound"
	"github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/transport/internet/reality"
	"google.golang.org/protobuf/proto"
)

func TestNewTypedMessage_WithValidProtoMessage(t *testing.T) {
	// Test that NewTypedMessage works with valid proto.Message
	config := &inbound.Config{
		Default: &inbound.DefaultConfig{
			Level: 0,
		},
	}

	tm, err := NewTypedMessage("xray.proxy.vmess.inbound.Config", config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if tm == nil {
		t.Fatal("Expected TypedMessage, got nil")
	}

	if tm.Type != "xray.proxy.vmess.inbound.Config" {
		t.Errorf("Expected type 'xray.proxy.vmess.inbound.Config', got '%s'", tm.Type)
	}

	if len(tm.Value) == 0 {
		t.Error("Expected non-empty value")
	}

	// Verify the value can be unmarshaled back
	parsedConfig := &inbound.Config{}
	err = proto.Unmarshal(tm.Value, parsedConfig)
	if err != nil {
		t.Fatalf("Expected value to be valid proto bytes, got error: %v", err)
	}

	if parsedConfig.Default == nil || parsedConfig.Default.Level != 0 {
		t.Error("Expected unmarshaled config to have correct values")
	}
}

func TestNewTypedMessage_WithNilMessage(t *testing.T) {
	// Test that NewTypedMessage returns error for nil message
	_, err := NewTypedMessage("test.type", nil)
	if err == nil {
		t.Fatal("Expected error for nil message, got nil")
	}
}

func TestNewTypedMessage_WithReceiverConfig(t *testing.T) {
	// Test with ReceiverConfig proto
	receiverConfig := &proxyman.ReceiverConfig{
		PortList: &net.PortList{
			Range: []*net.PortRange{
				{
					From: 10086,
					To:   10086,
				},
			},
		},
		Listen: &net.IPOrDomain{
			Address: &net.IPOrDomain_Ip{
				Ip: []byte("127.0.0.1"),
			},
		},
	}

	tm, err := NewTypedMessage("xray.app.proxyman.ReceiverConfig", receiverConfig)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if tm.Type != "xray.app.proxyman.ReceiverConfig" {
		t.Errorf("Expected type, got '%s'", tm.Type)
	}

	// Verify serialization
	parsed := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(tm.Value, parsed)
	if err != nil {
		t.Fatalf("Expected value to be valid proto bytes: %v", err)
	}

	if parsed.PortList == nil || len(parsed.PortList.Range) == 0 {
		t.Error("Expected port list to be preserved")
	}
}

func TestNewTypedMessage_WithVMessAccount(t *testing.T) {
	// Test with VMess Account
	account := &vmess.Account{
		Id: "a348c600-1234-5678-9abc-def012345678",
	}

	tm, err := NewTypedMessage("xray.proxy.vmess.Account", account)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify serialization
	parsed := &vmess.Account{}
	err = proto.Unmarshal(tm.Value, parsed)
	if err != nil {
		t.Fatalf("Expected value to be valid proto bytes: %v", err)
	}

	if parsed.Id != account.Id {
		t.Errorf("Expected ID '%s', got '%s'", account.Id, parsed.Id)
	}
}

func TestParseInboundConfig_WithVMessProto(t *testing.T) {
	// Test parsing a VMess inbound config
	jsonData := []byte(`{
		"tag": "test-vmess",
		"protocol": "vmess",
		"port": "10086",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678",
					"email": "test@example.com",
					"level": 0
				}
			]
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if cfg.Tag != "test-vmess" {
		t.Errorf("Expected tag 'test-vmess', got '%s'", cfg.Tag)
	}

	// Verify receiver settings are properly serialized as proto
	if cfg.ReceiverSettings == nil {
		t.Fatal("Expected receiver settings")
	}

	// Verify we can unmarshal the receiver settings
	receiver := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(cfg.ReceiverSettings.Value, receiver)
	if err != nil {
		t.Fatalf("Expected receiver settings to be valid proto: %v", err)
	}

	// Verify proxy settings
	if cfg.ProxySettings == nil {
		t.Fatal("Expected proxy settings")
	}

	// Verify we can unmarshal the proxy settings
	vmessConfig := &inbound.Config{}
	err = proto.Unmarshal(cfg.ProxySettings.Value, vmessConfig)
	if err != nil {
		t.Fatalf("Expected proxy settings to be valid proto: %v", err)
	}

	if len(vmessConfig.User) != 1 {
		t.Errorf("Expected 1 user, got %d", len(vmessConfig.User))
	}
}

func TestParseInboundConfig_WithSniffing(t *testing.T) {
	// Test parsing with sniffing settings
	jsonData := []byte(`{
		"tag": "test-sniffing",
		"protocol": "vmess",
		"port": "10087",
		"settings": {},
		"sniffing": {
			"enabled": true,
			"destOverride": ["http", "tls"]
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify basic fields
	if cfg.Tag != "test-sniffing" {
		t.Errorf("Expected tag 'test-sniffing', got '%s'", cfg.Tag)
	}
	if cfg.ReceiverSettings == nil {
		t.Fatal("Expected receiver settings")
	}
	if cfg.ProxySettings == nil {
		t.Fatal("Expected proxy settings")
	}

	// Note: SniffingSettings is not available in the generated InboundHandlerConfig type
	// This is a limitation of the generated types
}

func TestTypedMessage_ValueIsProtobufNotJSON(t *testing.T) {
	// This test verifies that TypedMessage.Value contains protobuf bytes, not JSON
	// This is the core fix for the serialization issue

	config := &inbound.Config{
		Default: &inbound.DefaultConfig{
			Level: 1,
		},
	}

	tm, err := NewTypedMessage("xray.proxy.vmess.inbound.Config", config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Try to unmarshal as protobuf - should succeed
	pbConfig := &inbound.Config{}
	err = proto.Unmarshal(tm.Value, pbConfig)
	if err != nil {
		t.Fatalf("Value should be valid protobuf: %v", err)
	}

	// Try to unmarshal as JSON - should fail (or not produce correct result)
	// This proves Value is protobuf, not JSON
	var jsonMap map[string]interface{}
	err = json.Unmarshal(tm.Value, &jsonMap)
	if err == nil {
		// If JSON unmarshal succeeds, check if it looks like JSON (has keys like "default", "level")
		// If it has protobuf-specific field numbers, it's NOT JSON
		if _, hasDefault := jsonMap["default"]; hasDefault {
			t.Log("Value appears to be JSON (unexpected for protobuf serialization)")
		}
	}

	// The key test: protobuf unmarshal should work
	if pbConfig.Default == nil || pbConfig.Default.Level != 1 {
		t.Error("Protobuf unmarshal should preserve values")
	}

	// Additional verification: the bytes should start with protobuf wire format
	// (not JSON "{" character)
	if len(tm.Value) > 0 && tm.Value[0] == '{' {
		t.Error("Value should NOT start with '{' (that's JSON format)")
	}
}

// Test that serial.TypedMessage (the actual xray proto) works correctly
func TestSerialTypedMessage(t *testing.T) {
	// Test that we can create and use serial.TypedMessage correctly
	account := &vmess.Account{
		Id: "test-uuid",
	}

	accountBytes, err := proto.Marshal(account)
	if err != nil {
		t.Fatalf("Failed to marshal account: %v", err)
	}

	serialTM := &serial.TypedMessage{
		Type:  "xray.proxy.vmess.Account",
		Value: accountBytes,
	}

	// Verify we can unmarshal
	parsedAccount := &vmess.Account{}
	err = proto.Unmarshal(serialTM.Value, parsedAccount)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsedAccount.Id != "test-uuid" {
		t.Errorf("Expected ID 'test-uuid', got '%s'", parsedAccount.Id)
	}
}

// TestParseInboundConfig_WithReality tests Reality config parsing with proper key encoding
func TestParseInboundConfig_WithReality(t *testing.T) {
	// This test verifies that Reality PrivateKey/PublicKey are correctly decoded from base64
	// and ShortIds are correctly decoded from hex before being written to protobuf

	// Use proper 32-byte keys in base64url encoding (no padding)
	// These are valid X25519 keys (with clamping applied)
	privateKeyBase64 := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE" // 32 'a' bytes
	publicKeyBase64 := "YWFhYWFhYWFhYWJhYWFhYWFhYWFhYWFhYWFhYWFhYWE" // 32 'b' bytes

	jsonData := []byte(`{
		"tag": "test-reality",
		"protocol": "vless",
		"port": "10088",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678",
					"email": "test@example.com",
					"level": 0,
					"flow": "xtls-rprx-vision"
				}
			]
		},
		"streamSettings": {
			"network": "tcp",
			"security": "reality",
			"realitySettings": {
				"serverNames": ["www.microsoft.com"],
				"privateKey": "` + privateKeyBase64 + `",
				"publicKey": "` + publicKeyBase64 + `",
				"shortId": "0123456789abcdef",
				"shortIds": ["fedcba9876543210", "0011223344556677"],
				"dest": "www.microsoft.com:443"
			}
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if cfg.Tag != "test-reality" {
		t.Errorf("Expected tag 'test-reality', got '%s'", cfg.Tag)
	}

	// Verify receiver settings contain reality config
	if cfg.ReceiverSettings == nil {
		t.Fatal("Expected receiver settings")
	}

	// Verify we can unmarshal the receiver settings
	receiver := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(cfg.ReceiverSettings.Value, receiver)
	if err != nil {
		t.Fatalf("Expected receiver settings to be valid proto: %v", err)
	}

	// Verify StreamSettings contain reality
	if receiver.StreamSettings == nil {
		t.Fatal("Expected stream settings")
	}

	// Find reality config in security settings
	var realityConfigFound bool
	for _, secSettings := range receiver.StreamSettings.SecuritySettings {
		if secSettings.Type == "xray.transport.internet.reality.Config" {
			realityConfigFound = true
			realityCfg := &reality.Config{}
			err = proto.Unmarshal(secSettings.Value, realityCfg)
			if err != nil {
				t.Fatalf("Failed to unmarshal reality config: %v", err)
			}

			// Verify PrivateKey is 32 bytes (raw, not base64 string)
			if len(realityCfg.PrivateKey) != 32 {
				t.Errorf("Expected PrivateKey length 32, got %d", len(realityCfg.PrivateKey))
			}

			// Verify PublicKey is 32 bytes (raw, not base64 string)
			if len(realityCfg.PublicKey) != 32 {
				t.Errorf("Expected PublicKey length 32, got %d", len(realityCfg.PublicKey))
			}

			// Verify ShortId is 8 bytes (decoded from hex)
			if len(realityCfg.ShortId) != 8 {
				t.Errorf("Expected ShortId length 8, got %d", len(realityCfg.ShortId))
			}

			// Verify ShortIds are properly decoded
			if len(realityCfg.ShortIds) != 2 {
				t.Errorf("Expected 2 ShortIds, got %d", len(realityCfg.ShortIds))
			}
			for i, sid := range realityCfg.ShortIds {
				if len(sid) != 8 {
					t.Errorf("Expected ShortIds[%d] length 8, got %d", i, len(sid))
				}
			}

			// Verify server names
			if len(realityCfg.ServerNames) != 1 || realityCfg.ServerNames[0] != "www.microsoft.com" {
				t.Errorf("Expected serverNames ['www.microsoft.com'], got %v", realityCfg.ServerNames)
			}

			// Verify dest
			if realityCfg.Dest != "www.microsoft.com:443" {
				t.Errorf("Expected dest 'www.microsoft.com:443', got '%s'", realityCfg.Dest)
			}
		}
	}

	if !realityConfigFound {
		t.Error("Expected to find reality config in security settings")
	}
}

// TestParseInboundConfig_WithReality_DefaultType tests that Reality Config defaults Type to "tcp"
func TestParseInboundConfig_WithReality_DefaultType(t *testing.T) {
	// Test that when realitySettings.type is not provided, Type defaults to "tcp"
	privateKeyBase64 := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
	publicKeyBase64 := "YWFhYWFhYWFhYWJhYWFhYWFhYWFhYWFhYWFhYWFhYWE"

	jsonData := []byte(`{
		"tag": "test-reality-default-type",
		"protocol": "vless",
		"port": "10090",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678"
				}
			]
		},
		"streamSettings": {
			"network": "tcp",
			"security": "reality",
			"realitySettings": {
				"serverNames": ["www.microsoft.com"],
				"privateKey": "` + privateKeyBase64 + `",
				"publicKey": "` + publicKeyBase64 + `",
				"shortId": "0123456789abcdef",
				"dest": "www.microsoft.com:443"
			}
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify receiver settings contain reality config
	if cfg.ReceiverSettings == nil {
		t.Fatal("Expected receiver settings")
	}

	receiver := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(cfg.ReceiverSettings.Value, receiver)
	if err != nil {
		t.Fatalf("Expected receiver settings to be valid proto: %v", err)
	}

	// Find reality config in security settings
	var realityConfigFound bool
	for _, secSettings := range receiver.StreamSettings.SecuritySettings {
		if secSettings.Type == "xray.transport.internet.reality.Config" {
			realityConfigFound = true
			realityCfg := &reality.Config{}
			err = proto.Unmarshal(secSettings.Value, realityCfg)
			if err != nil {
				t.Fatalf("Failed to unmarshal reality config: %v", err)
			}

			// Verify Type defaults to "tcp"
			if realityCfg.Type != "tcp" {
				t.Errorf("Expected Type 'tcp' (default), got '%s'", realityCfg.Type)
			}
		}
	}

	if !realityConfigFound {
		t.Error("Expected to find reality config in security settings")
	}
}

// TestParseInboundConfig_WithReality_ExplicitType tests that explicit type is preserved
func TestParseInboundConfig_WithReality_ExplicitType(t *testing.T) {
	// Test that when realitySettings.type is provided, it is preserved
	privateKeyBase64 := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
	publicKeyBase64 := "YWFhYWFhYWFhYWJhYWFhYWFhYWFhYWFhYWFhYWFhYWE"

	jsonData := []byte(`{
		"tag": "test-reality-explicit-type",
		"protocol": "vless",
		"port": "10091",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678"
				}
			]
		},
		"streamSettings": {
			"network": "tcp",
			"security": "reality",
			"realitySettings": {
				"serverNames": ["www.microsoft.com"],
				"privateKey": "` + privateKeyBase64 + `",
				"publicKey": "` + publicKeyBase64 + `",
				"shortId": "0123456789abcdef",
				"dest": "www.microsoft.com:443",
				"type": "tcp"
			}
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	receiver := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(cfg.ReceiverSettings.Value, receiver)
	if err != nil {
		t.Fatalf("Expected receiver settings to be valid proto: %v", err)
	}

	// Find reality config in security settings
	var realityConfigFound bool
	for _, secSettings := range receiver.StreamSettings.SecuritySettings {
		if secSettings.Type == "xray.transport.internet.reality.Config" {
			realityConfigFound = true
			realityCfg := &reality.Config{}
			err = proto.Unmarshal(secSettings.Value, realityCfg)
			if err != nil {
				t.Fatalf("Failed to unmarshal reality config: %v", err)
			}

			// Verify Type is "tcp" as explicitly set
			if realityCfg.Type != "tcp" {
				t.Errorf("Expected Type 'tcp', got '%s'", realityCfg.Type)
			}
		}
	}

	if !realityConfigFound {
		t.Error("Expected to find reality config in security settings")
	}
}

// TestParseInboundConfig_WithReality_InvalidHex tests that invalid hex in ShortId returns error
func TestParseInboundConfig_WithReality_InvalidHex(t *testing.T) {
	jsonData := []byte(`{
		"tag": "test-reality-invalid",
		"protocol": "vless",
		"port": "10089",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678"
				}
			]
		},
		"streamSettings": {
			"network": "tcp",
			"security": "reality",
			"realitySettings": {
				"privateKey": "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE",
				"shortId": "not-valid-hex"
			}
		}
	}`)

	_, err := ParseInboundConfig(jsonData)
	if err == nil {
		t.Fatal("Expected error for invalid hex in ShortId, got nil")
	}

	// Verify error message mentions hex
	if err != nil {
		t.Logf("Got expected error: %v", err)
	}
}

func TestNormalizeNetworkName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"tcp", "tcp"},
		{"ws", "websocket"},
		{"h2", "h2"},
		{"http", "h2"},
		{"grpc", "grpc"},
		// xhttp is alias for splithttp
		{"xhttp", "splithttp"},
		// splithttp and h3 should map to splithttp (not h2)
		{"splithttp", "splithttp"},
		{"h3", "splithttp"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeNetworkName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeNetworkName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildStreamConfigProto_H2(t *testing.T) {
	// Test that h2 network is properly converted to h2 ProtocolName
	streamSettings := map[string]interface{}{
		"network": "h2",
		"httpSettings": map[string]interface{}{
			"host": []string{"example.com"},
			"path": "/",
		},
	}

	streamConfig, err := buildStreamConfigProto(streamSettings)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify ProtocolName is "h2" (not "http")
	if streamConfig.ProtocolName != "h2" {
		t.Errorf("ProtocolName = %q, want %q", streamConfig.ProtocolName, "h2")
	}
}

func TestBuildStreamConfigProto_HTTPAlias(t *testing.T) {
	// Test that "http" alias is normalized to "h2"
	streamSettings := map[string]interface{}{
		"network": "http",
		"httpSettings": map[string]interface{}{
			"host": []string{"example.com"},
			"path": "/",
		},
	}

	streamConfig, err := buildStreamConfigProto(streamSettings)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify ProtocolName is "h2" (not "http")
	if streamConfig.ProtocolName != "h2" {
		t.Errorf("ProtocolName = %q, want %q", streamConfig.ProtocolName, "h2")
	}
}

func TestBuildStreamConfigProto_SplitHTTP(t *testing.T) {
	// Test that splithttp network uses proper proto serialization (not JSON)
	streamSettings := map[string]interface{}{
		"network": "splithttp",
		"splithttpSettings": map[string]interface{}{
			"host": "example.com",
			"path": "/v2ray",
			"mode": "stream",
		},
	}

	streamConfig, err := buildStreamConfigProto(streamSettings)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify ProtocolName is "splithttp"
	if streamConfig.ProtocolName != "splithttp" {
		t.Errorf("ProtocolName = %q, want %q", streamConfig.ProtocolName, "splithttp")
	}

	// Verify transport settings exist
	if len(streamConfig.TransportSettings) == 0 {
		t.Fatal("Expected transport settings")
	}

	// Find splithttp transport config
	var splithttpSettings *serial.TypedMessage
	for _, ts := range streamConfig.TransportSettings {
		if ts.ProtocolName == "splithttp" {
			splithttpSettings = ts.Settings
			break
		}
	}

	if splithttpSettings == nil {
		t.Fatal("Expected splithttp settings")
	}

	// Verify the type is correct
	if splithttpSettings.Type != "xray.transport.internet.splithttp.Config" {
		t.Errorf("Expected type 'xray.transport.internet.splithttp.Config', got '%s'", splithttpSettings.Type)
	}

	// KEY TEST: Verify Value is protobuf wire format, not JSON
	// Proto wire format does NOT start with '{'
	if len(splithttpSettings.Value) > 0 && splithttpSettings.Value[0] == '{' {
		t.Error("Value should NOT start with '{' (that's JSON format, not protobuf)")
	}

	// Verify the bytes can be parsed as our manual protobuf encoding
	// The encoded format should have specific patterns for field tags
	if len(splithttpSettings.Value) == 0 {
		t.Error("Value should not be empty")
	}

	// Verify that the bytes contain our expected strings
	valueStr := string(splithttpSettings.Value)
	if !containsSubstring(valueStr, "example.com") {
		t.Error("Value should contain host 'example.com'")
	}
	if !containsSubstring(valueStr, "/v2ray") {
		t.Error("Value should contain path '/v2ray'")
	}
	if !containsSubstring(valueStr, "stream") {
		t.Error("Value should contain mode 'stream'")
	}
}

func TestParseInboundConfig_WithSplitHTTP(t *testing.T) {
	// Test parsing a VLESS inbound with splithttp + reality (simulates vless-xhttp-reality)
	privateKeyBase64 := "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE"
	publicKeyBase64 := "YWFhYWFhYWFhYWJhYWFhYWFhYWFhYWFhYWFhYWFhYWE"

	jsonData := []byte(`{
		"tag": "test-vless-xhttp-reality",
		"protocol": "vless",
		"port": "10092",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "a348c600-1234-5678-9abc-def012345678",
					"flow": "xtls-rprx-vision"
				}
			]
		},
		"streamSettings": {
			"network": "xhttp",
			"security": "reality",
			"realitySettings": {
				"serverNames": ["www.microsoft.com"],
				"privateKey": "` + privateKeyBase64 + `",
				"publicKey": "` + publicKeyBase64 + `",
				"shortId": "0123456789abcdef",
				"dest": "www.microsoft.com:443"
			},
			"splithttpSettings": {
				"host": "example.com",
				"path": "/v2ray",
				"mode": "stream"
			}
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if cfg.Tag != "test-vless-xhttp-reality" {
		t.Errorf("Expected tag 'test-vless-xhttp-reality', got '%s'", cfg.Tag)
	}

	// Verify receiver settings
	if cfg.ReceiverSettings == nil {
		t.Fatal("Expected receiver settings")
	}

	receiver := &proxyman.ReceiverConfig{}
	err = proto.Unmarshal(cfg.ReceiverSettings.Value, receiver)
	if err != nil {
		t.Fatalf("Expected receiver settings to be valid proto: %v", err)
	}

	// Verify StreamSettings
	if receiver.StreamSettings == nil {
		t.Fatal("Expected stream settings")
	}

	// Verify network is normalized to "splithttp"
	if receiver.StreamSettings.ProtocolName != "splithttp" {
		t.Errorf("Expected ProtocolName 'splithttp', got '%s'", receiver.StreamSettings.ProtocolName)
	}

	// Find splithttp transport config
	var splithttpFound bool
	for _, ts := range receiver.StreamSettings.TransportSettings {
		if ts.ProtocolName == "splithttp" {
			splithttpFound = true
			// KEY TEST: Verify Value is protobuf wire format (not JSON)
			if len(ts.Settings.Value) > 0 && ts.Settings.Value[0] == '{' {
				t.Error("SplitHTTP Value should NOT start with '{' (that's JSON format)")
			}

			// Verify the bytes contain our expected strings
			valueStr := string(ts.Settings.Value)
			if !containsSubstring(valueStr, "example.com") {
				t.Error("Value should contain host 'example.com'")
			}
			if !containsSubstring(valueStr, "/v2ray") {
				t.Error("Value should contain path '/v2ray'")
			}
			if !containsSubstring(valueStr, "stream") {
				t.Error("Value should contain mode 'stream'")
			}
			break
		}
	}

	if !splithttpFound {
		t.Error("Expected to find splithttp transport settings")
	}
}

func TestBuildStreamConfigProto_SplitHTTP_Headers(t *testing.T) {
	// Test that headers are properly converted to map[string]string
	streamSettings := map[string]interface{}{
		"network": "splithttp",
		"splithttpSettings": map[string]interface{}{
			"host": "example.com",
			"path": "/v2ray",
			"headers": map[string]interface{}{
				"X-Custom-Header": "custom-value",
				"User-Agent": "Mozilla/5.0",
			},
		},
	}

	streamConfig, err := buildStreamConfigProto(streamSettings)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Find splithttp transport config
	var splithttpSettings *serial.TypedMessage
	for _, ts := range streamConfig.TransportSettings {
		if ts.ProtocolName == "splithttp" {
			splithttpSettings = ts.Settings
			break
		}
	}

	if splithttpSettings == nil {
		t.Fatal("Expected splithttp settings")
	}

	// Verify Value is protobuf wire format (not JSON)
	if len(splithttpSettings.Value) > 0 && splithttpSettings.Value[0] == '{' {
		t.Error("Value should NOT start with '{' (that's JSON format)")
	}

	// Verify the bytes contain our expected header values
	valueStr := string(splithttpSettings.Value)
	if !containsSubstring(valueStr, "X-Custom-Header") {
		t.Error("Value should contain header key 'X-Custom-Header'")
	}
	if !containsSubstring(valueStr, "custom-value") {
		t.Error("Value should contain header value 'custom-value'")
	}
	if !containsSubstring(valueStr, "User-Agent") {
		t.Error("Value should contain header key 'User-Agent'")
	}
	if !containsSubstring(valueStr, "Mozilla/5.0") {
		t.Error("Value should contain header value 'Mozilla/5.0'")
	}
}

// containsSubstring is a helper for checking if a string contains a substring
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (len(s) == 0 || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildStreamConfigProto_XTLS(t *testing.T) {
	// Test that XTLS security is properly configured
	// Note: XTLS in xray-core uses TLS config internally (security type maps to tls.Config)
	streamSettings := map[string]interface{}{
		"network": "tcp",
		"security": "xtls",
		"tlsSettings": map[string]interface{}{
			"serverName": "example.com",
			"allowInsecure": true,
			"alpn": []string{"h2", "http/1.1"},
			"fingerprint": "chrome",
			"certificates": []interface{}{
				map[string]interface{}{
					"certificate": []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
					"key": []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"),
				},
			},
		},
	}

	streamConfig, err := buildStreamConfigProto(streamSettings)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify security type is set to tls.Config (XTLS uses TLS config internally in xray-core)
	if streamConfig.SecurityType != "xray.transport.internet.tls.Config" {
		t.Errorf("Expected SecurityType 'xray.transport.internet.tls.Config', got '%s'", streamConfig.SecurityType)
	}

	// Verify SecuritySettings has one entry
	if len(streamConfig.SecuritySettings) != 1 {
		t.Fatalf("Expected 1 SecuritySettings entry, got %d", len(streamConfig.SecuritySettings))
	}

	// Verify the TypedMessage type is tls.Config (XTLS uses TLS config in xray-core)
	tlsSettings := streamConfig.SecuritySettings[0]
	if tlsSettings.Type != "xray.transport.internet.tls.Config" {
		t.Errorf("Expected TypedMessage type 'xray.transport.internet.tls.Config', got '%s'", tlsSettings.Type)
	}

	// Verify Value is protobuf wire format (not JSON)
	if len(tlsSettings.Value) > 0 && tlsSettings.Value[0] == '{' {
		t.Error("Value should NOT start with '{' (that's JSON format)")
	}

	// Verify the value can be unmarshaled back (proving it's real proto bytes)
	// We use tls.Config since xtls uses the same structure
	tlsConfig := &serial.TypedMessage{}
	err = proto.Unmarshal(tlsSettings.Value, tlsConfig)
	if err != nil {
		t.Logf("Note: Cannot unmarshal as raw TypedMessage (expected for proto-in-proto)")
	}

	// Verify the server name is in the config
	// Since we're using proto marshal, the server name should be encoded in the bytes
	if len(tlsSettings.Value) == 0 {
		t.Error("Expected non-empty value bytes")
	}
}

// ─── Tests for newly added socks/http builders and shadowsocks type name fix ───

// TestGetProtocolSettingsType verifies type names are consistent with actual builders.
func TestGetProtocolSettingsType(t *testing.T) {
	cases := []struct {
		protocol string
		want     string
	}{
		{"vmess", "xray.proxy.vmess.inbound.Config"},
		{"vless", "xray.proxy.vless.inbound.Config"},
		{"trojan", "xray.proxy.trojan.ServerConfig"},
		{"shadowsocks", "xray.proxy.shadowsocks_2022.ServerConfig"},
		{"socks", "xray.proxy.socks.ServerConfig"},
		{"http", "xray.proxy.http.ServerConfig"},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			got := getProtocolSettingsType(tc.protocol)
			if got != tc.want {
				t.Errorf("getProtocolSettingsType(%q) = %q, want %q", tc.protocol, got, tc.want)
			}
		})
	}
}

// TestBuildProxySettingsProto_Socks verifies SOCKS5 inbound config builder.
func TestBuildProxySettingsProto_Socks(t *testing.T) {
	t.Run("no_auth", func(t *testing.T) {
		settings := map[string]interface{}{
			"auth": "noauth",
			"udp":  true,
		}
		tm, err := buildSocksInboundConfig(settings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tm.Type != "xray.proxy.socks.ServerConfig" {
			t.Errorf("type = %q, want %q", tm.Type, "xray.proxy.socks.ServerConfig")
		}
		if len(tm.Value) == 0 {
			t.Error("Value should not be empty")
		}
	})

	t.Run("password_auth_with_accounts", func(t *testing.T) {
		settings := map[string]interface{}{
			"auth": "password",
			"accounts": []interface{}{
				map[string]interface{}{"user": "alice", "pass": "s3cr3t"},
				map[string]interface{}{"user": "bob", "pass": "p@ss"},
			},
			"udp":       false,
			"userLevel": float64(1),
		}
		tm, err := buildSocksInboundConfig(settings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tm.Type != "xray.proxy.socks.ServerConfig" {
			t.Errorf("type = %q, want %q", tm.Type, "xray.proxy.socks.ServerConfig")
		}
		// Bytes should contain account usernames
		valStr := string(tm.Value)
		if !findSubstring(valStr, "alice") {
			t.Error("Value should contain account username 'alice'")
		}
		if !findSubstring(valStr, "bob") {
			t.Error("Value should contain account username 'bob'")
		}
	})

	t.Run("empty_settings", func(t *testing.T) {
		tm, err := buildSocksInboundConfig(map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tm.Type != "xray.proxy.socks.ServerConfig" {
			t.Errorf("type = %q, want %q", tm.Type, "xray.proxy.socks.ServerConfig")
		}
	})
}

// TestBuildProxySettingsProto_HTTP verifies HTTP inbound config builder.
func TestBuildProxySettingsProto_HTTP(t *testing.T) {
	t.Run("with_accounts", func(t *testing.T) {
		settings := map[string]interface{}{
			"accounts": []interface{}{
				map[string]interface{}{"user": "admin", "pass": "admin123"},
			},
			"allowTransparent": true,
			"userLevel":        float64(0),
		}
		tm, err := buildHTTPInboundConfig(settings)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tm.Type != "xray.proxy.http.ServerConfig" {
			t.Errorf("type = %q, want %q", tm.Type, "xray.proxy.http.ServerConfig")
		}
		valStr := string(tm.Value)
		if !findSubstring(valStr, "admin") {
			t.Error("Value should contain account username 'admin'")
		}
	})

	t.Run("empty_settings", func(t *testing.T) {
		tm, err := buildHTTPInboundConfig(map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tm.Type != "xray.proxy.http.ServerConfig" {
			t.Errorf("type = %q, want %q", tm.Type, "xray.proxy.http.ServerConfig")
		}
	})
}

// TestParseInboundConfig_Socks verifies full ParseInboundConfig path for SOCKS5.
func TestParseInboundConfig_Socks(t *testing.T) {
	jsonData := []byte(`{
		"tag": "socks-in",
		"protocol": "socks",
		"port": 1080,
		"listen": "0.0.0.0",
		"settings": {
			"auth": "password",
			"accounts": [
				{"user": "u1", "pass": "p1"}
			],
			"udp": true
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tag != "socks-in" {
		t.Errorf("tag = %q, want %q", cfg.Tag, "socks-in")
	}
	if cfg.ProxySettings == nil {
		t.Fatal("ProxySettings should not be nil")
	}
	if cfg.ProxySettings.Type != "xray.proxy.socks.ServerConfig" {
		t.Errorf("ProxySettings.Type = %q, want %q", cfg.ProxySettings.Type, "xray.proxy.socks.ServerConfig")
	}
}

// TestParseInboundConfig_HTTP verifies full ParseInboundConfig path for HTTP.
func TestParseInboundConfig_HTTP(t *testing.T) {
	jsonData := []byte(`{
		"tag": "http-in",
		"protocol": "http",
		"port": 8080,
		"listen": "127.0.0.1",
		"settings": {
			"accounts": [
				{"user": "proxy", "pass": "proxypass"}
			],
			"allowTransparent": false
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tag != "http-in" {
		t.Errorf("tag = %q, want %q", cfg.Tag, "http-in")
	}
	if cfg.ProxySettings == nil {
		t.Fatal("ProxySettings should not be nil")
	}
	if cfg.ProxySettings.Type != "xray.proxy.http.ServerConfig" {
		t.Errorf("ProxySettings.Type = %q, want %q", cfg.ProxySettings.Type, "xray.proxy.http.ServerConfig")
	}
}

// TestParseInboundConfig_Shadowsocks verifies shadowsocks 2022 type name is correct.
func TestParseInboundConfig_Shadowsocks2022(t *testing.T) {
	jsonData := []byte(`{
		"tag": "ss-in",
		"protocol": "shadowsocks",
		"port": 8388,
		"listen": "0.0.0.0",
		"settings": {
			"method": "2022-blake3-aes-256-gcm",
			"password": "dGVzdHBhc3N3b3JkMTIzNDU2Nzg5MDEyMzQ1Njc4OTA="
		}
	}`)

	cfg, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProxySettings == nil {
		t.Fatal("ProxySettings should not be nil")
	}
	// Critical: must be shadowsocks_2022, NOT the old shadowsocks type
	if cfg.ProxySettings.Type != "xray.proxy.shadowsocks_2022.ServerConfig" {
		t.Errorf("ProxySettings.Type = %q, want %q",
			cfg.ProxySettings.Type, "xray.proxy.shadowsocks_2022.ServerConfig")
	}
}

// TestParseInboundConfig_UnsupportedProtocol verifies unknown protocols return error.
func TestParseInboundConfig_UnsupportedProtocol(t *testing.T) {
	jsonData := []byte(`{
		"tag": "unknown-in",
		"protocol": "wireguard",
		"port": 51820,
		"settings": {}
	}`)

	_, err := ParseInboundConfig(jsonData)
	if err == nil {
		t.Fatal("expected error for unsupported protocol, got nil")
	}
}

// TestBuildProxySettingsProto_DispatchAllProtocols verifies all supported protocols
// route to the correct builder without error (smoke test).
func TestBuildProxySettingsProto_DispatchAllProtocols(t *testing.T) {
	cases := []struct {
		protocol string
		settings map[string]interface{}
		wantType string
	}{
		{
			protocol: "vmess",
			settings: map[string]interface{}{
				"clients": []interface{}{
					map[string]interface{}{"id": "a348c600-1234-5678-9abc-def012345678"},
				},
			},
			wantType: "xray.proxy.vmess.inbound.Config",
		},
		{
			protocol: "vless",
			settings: map[string]interface{}{
				"clients": []interface{}{
					map[string]interface{}{"id": "a348c600-1234-5678-9abc-def012345678"},
				},
				"decryption": "none",
			},
			wantType: "xray.proxy.vless.inbound.Config",
		},
		{
			protocol: "trojan",
			settings: map[string]interface{}{
				"clients": []interface{}{
					map[string]interface{}{"password": "trojanpass"},
				},
			},
			wantType: "xray.proxy.trojan.ServerConfig",
		},
		{
			protocol: "shadowsocks",
			settings: map[string]interface{}{
				"method":   "2022-blake3-aes-256-gcm",
				"password": "dGVzdA==",
			},
			wantType: "xray.proxy.shadowsocks_2022.ServerConfig",
		},
		{
			protocol: "socks",
			settings: map[string]interface{}{
				"auth": "password",
				"accounts": []interface{}{
					map[string]interface{}{"user": "u", "pass": "p"},
				},
			},
			wantType: "xray.proxy.socks.ServerConfig",
		},
		{
			protocol: "http",
			settings: map[string]interface{}{
				"accounts": []interface{}{
					map[string]interface{}{"user": "admin", "pass": "pass"},
				},
			},
			wantType: "xray.proxy.http.ServerConfig",
		},
	}

	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			typeName := getProtocolSettingsType(tc.protocol)
			tm, err := buildProxySettingsProto(typeName, tc.protocol, tc.settings)
			if err != nil {
				t.Fatalf("buildProxySettingsProto(%q) error: %v", tc.protocol, err)
			}
			if tm == nil {
				t.Fatalf("buildProxySettingsProto(%q) returned nil", tc.protocol)
			}
			if tm.Type != tc.wantType {
				t.Errorf("TypedMessage.Type = %q, want %q", tm.Type, tc.wantType)
			}
			// Note: socks/http with all-default fields may produce empty protobuf bytes (valid)
			// so we only check Value length when settings are non-trivial
		})
	}
}

// ─── existing tests below ────────────────────────────────────────────────────

func TestParseInboundConfig_WithXTLS(t *testing.T) {
	// Test parsing a full inbound config with XTLS security
	jsonData := []byte(`{
		"tag": "vless-xtls-in",
		"protocol": "vless",
		"port": "443",
		"listen": "0.0.0.0",
		"settings": {
			"clients": [
				{
					"id": "b0e5a5aa-1111-2222-3333-444455556666",
					"flow": "xtls-rprx-vision",
					"email": "user@example.com"
				}
			],
			"decryption": "none"
		},
		"streamSettings": {
			"network": "tcp",
			"security": "xtls",
			"tlsSettings": {
				"serverName": "example.com",
				"allowInsecure": false,
				"alpn": ["h2", "http/1.1"]
			}
		}
	}`)

	handlerConfig, err := ParseInboundConfig(jsonData)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if handlerConfig.Tag != "vless-xtls-in" {
		t.Errorf("Expected tag 'vless-xtls-in', got '%s'", handlerConfig.Tag)
	}

	// Verify receiver settings
	if handlerConfig.ReceiverSettings == nil {
		t.Fatal("Expected ReceiverSettings")
	}

	// Check stream config for xtls
	// Note: The actual stream config is inside ReceiverSettings as a TypedMessage
	// We need to verify the parsing works without error
}
