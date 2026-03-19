package xray

import (
	"encoding/json"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestRenderer_ToProvider_EmptyModel(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(native.JSON) == 0 {
		t.Error("expected non-empty JSON")
	}

	// Verify basic structure
	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Check for required top-level keys
	if _, ok := m["log"]; !ok {
		t.Error("expected log key")
	}
	if _, ok := m["stats"]; !ok {
		t.Error("expected stats key")
	}
	if _, ok := m["policy"]; !ok {
		t.Error("expected policy key")
	}
	if _, ok := m["api"]; !ok {
		t.Error("expected api key")
	}
	if _, ok := m["inbounds"]; !ok {
		t.Error("expected inbounds key")
	}
	if _, ok := m["outbounds"]; !ok {
		t.Error("expected outbounds key")
	}
	if _, ok := m["routing"]; !ok {
		t.Error("expected routing key")
	}
}

func TestRenderer_ToProvider_WithInbounds(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
		Inbounds: []contracts.InboundSpec{
			{
				Tag:      "vmess-in",
				Port:     443,
				Protocol: contracts.ProtocolVMess,
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "tls",
					"server_name": "example.com",
					"alpn":        []string{"h2", "http/1.1"},
				},
			},
			{
				Tag:      "vless-in",
				Port:     8443,
				Protocol: contracts.ProtocolVLess,
				Extensions: map[string]any{
					"transport":   "ws",
					"security":    "tls",
					"server_name": "example.com",
					"ws_path":     "/vless",
				},
			},
		},
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	inbounds := m["inbounds"].([]interface{})
	// Should have API inbound + 2 user inbounds
	if len(inbounds) != 3 {
		t.Errorf("expected 3 inbounds, got %d", len(inbounds))
	}

	// Verify API inbound
	apiInbound := inbounds[0].(map[string]interface{})
	if apiInbound["tag"] != APIInboundTag {
		t.Errorf("expected API inbound tag, got %v", apiInbound["tag"])
	}

	// Verify user inbounds
	found := map[string]bool{}
	for _, in := range inbounds {
		inMap := in.(map[string]interface{})
		tag, _ := inMap["tag"].(string)
		found[tag] = true

		// Verify stream settings for VMess
		if tag == "vmess-in" {
			streamSettings, ok := inMap["streamSettings"].(map[string]interface{})
			if !ok {
				t.Error("expected streamSettings for vmess-in")
			} else {
				if streamSettings["security"] != "tls" {
					t.Errorf("expected security tls, got %v", streamSettings["security"])
				}
			}
		}

		// Verify WS settings for VLESS
		if tag == "vless-in" {
			streamSettings, ok := inMap["streamSettings"].(map[string]interface{})
			if !ok {
				t.Error("expected streamSettings for vless-in")
			} else {
				if streamSettings["network"] != "ws" {
					t.Errorf("expected network ws, got %v", streamSettings["network"])
				}
				wsSettings, ok := streamSettings["wsSettings"].(map[string]interface{})
				if !ok {
					t.Error("expected wsSettings")
				} else if wsSettings["path"] != "/vless" {
					t.Errorf("expected path /vless, got %v", wsSettings["path"])
				}
			}
		}
	}

	if !found["vmess-in"] {
		t.Error("expected vmess-in inbound")
	}
	if !found["vless-in"] {
		t.Error("expected vless-in inbound")
	}
}

func TestRenderer_ToProvider_WithUserBindings(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
		Inbounds: []contracts.InboundSpec{
			{
				Tag:      "vmess-in",
				Port:     443,
				Protocol: contracts.ProtocolVMess,
				Extensions: map[string]any{
					"transport": "tcp",
					"security":  "none",
				},
			},
		},
		UserBindings: map[string][]contracts.UserSpec{
			"vmess-in": {
				{Username: "user1@example.com", Extensions: map[string]any{"uuid": "uuid1", "alter_id": uint32(16)}},
				{Username: "user2@example.com", Extensions: map[string]any{"uuid": "uuid2", "alter_id": uint32(16)}},
			},
		},
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	inbounds := m["inbounds"].([]interface{})

	// Find vmess-in
	var vmessInbound map[string]interface{}
	for _, in := range inbounds {
		inMap := in.(map[string]interface{})
		if inMap["tag"] == "vmess-in" {
			vmessInbound = inMap
			break
		}
	}

	if vmessInbound == nil {
		t.Fatal("expected vmess-in inbound")
	}

	settings := vmessInbound["settings"].(map[string]interface{})
	// Handle both []interface{} and []map[string]interface{} types
	clientsIface := settings["clients"]
	var clients []map[string]interface{}
	switch c := clientsIface.(type) {
	case []interface{}:
		for _, item := range c {
			if m, ok := item.(map[string]interface{}); ok {
				clients = append(clients, m)
			}
		}
	case []map[string]interface{}:
		clients = c
	}

	if len(clients) != 2 {
		t.Errorf("expected 2 clients, got %d", len(clients))
	}

	// Verify client data
	if len(clients) > 0 {
		if clients[0]["email"] != "user1@example.com" {
			t.Errorf("expected email user1@example.com, got %v", clients[0]["email"])
		}
		if clients[0]["id"] != "uuid1" {
			t.Errorf("expected id uuid1, got %v", clients[0]["id"])
		}
	}
}

func TestRenderer_ToProvider_WithTrojanUsers(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
		Inbounds: []contracts.InboundSpec{
			{
				Tag:      "trojan-in",
				Port:     443,
				Protocol: contracts.ProtocolTrojan,
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "tls",
					"server_name": "example.com",
				},
			},
		},
		UserBindings: map[string][]contracts.UserSpec{
			"trojan-in": {
				{Username: "user@example.com", Password: "password123"},
			},
		},
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	inbounds := m["inbounds"].([]interface{})

	var trojanInbound map[string]interface{}
	for _, in := range inbounds {
		inMap := in.(map[string]interface{})
		if inMap["tag"] == "trojan-in" {
			trojanInbound = inMap
			break
		}
	}

	if trojanInbound == nil {
		t.Fatal("expected trojan-in inbound")
	}

	settings := trojanInbound["settings"].(map[string]interface{})
	// Handle both []interface{} and []map[string]interface{} types
	clientsIface := settings["clients"]
	var clients []map[string]interface{}
	switch c := clientsIface.(type) {
	case []interface{}:
		for _, item := range c {
			if m, ok := item.(map[string]interface{}); ok {
				clients = append(clients, m)
			}
		}
	case []map[string]interface{}:
		clients = c
	}

	if len(clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(clients))
	}

	if clients[0]["password"] != "password123" {
		t.Errorf("expected password password123, got %v", clients[0]["password"])
	}
}

func TestRenderer_FromProvider(t *testing.T) {
	renderer := NewRenderer()
	native := NativeConfig{JSON: []byte(`{
		"log": {"loglevel": "warning"},
		"stats": {},
		"policy": {"levels": {"0": {}}},
		"api": {"tag": "api", "services": ["HandlerService"]},
		"inbounds": [
			{
				"tag": "api",
				"port": "62789",
				"listen": "127.0.0.1",
				"protocol": "dokodemo-door"
			},
			{
				"tag": "vmess-in",
				"port": "443",
				"protocol": "vmess",
				"streamSettings": {
					"network": "tcp",
					"security": "tls"
				}
			}
		],
		"outbounds": [
			{"tag": "direct", "protocol": "freedom"}
		],
		"routing": {"domainStrategy": "AsIs", "rules": []}
	}`)}

	model, warnings, err := renderer.FromProvider(native)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if model.Type != contracts.ContainerXray {
		t.Errorf("expected ContainerXray, got %v", model.Type)
	}

	// Should have 1 inbound (skip API)
	if len(model.Inbounds) != 1 {
		t.Errorf("expected 1 inbound, got %d", len(model.Inbounds))
	}

	if len(model.Inbounds) > 0 {
		if model.Inbounds[0].Tag != "vmess-in" {
			t.Errorf("expected tag vmess-in, got %v", model.Inbounds[0].Tag)
		}
		if model.Inbounds[0].Protocol != contracts.ProtocolVMess {
			t.Errorf("expected protocol vmess, got %v", model.Inbounds[0].Protocol)
		}
		// Transport and Security are now in Extensions
		exts := model.Inbounds[0].Extensions
		if transport, _ := exts["transport"].(string); transport != "tcp" {
			t.Errorf("expected transport tcp, got %v", transport)
		}
		if security, _ := exts["security"].(string); security != "tls" {
			t.Errorf("expected security tls, got %v", security)
		}
	}

	if !warnings.Empty() {
		t.Logf("warnings: %v", warnings)
	}
}

func TestRenderer_ToProvider_WithSniffing(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
		Inbounds: []contracts.InboundSpec{
			{
				Tag:      "vmess-in",
				Port:     443,
				Protocol: contracts.ProtocolVMess,
				Extensions: map[string]any{
					"transport":        "tcp",
					"security":         "none",
					"sniffing_enabled": true,
				},
			},
		},
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	inbounds := m["inbounds"].([]interface{})

	// Find vmess-in
	var vmessInbound map[string]interface{}
	for _, in := range inbounds {
		inMap := in.(map[string]interface{})
		if inMap["tag"] == "vmess-in" {
			vmessInbound = inMap
			break
		}
	}

	sniffing, ok := vmessInbound["sniffing"].(map[string]interface{})
	if !ok {
		t.Error("expected sniffing config")
	} else {
		enabled, ok := sniffing["enabled"].(bool)
		if !ok || !enabled {
			t.Error("expected sniffing enabled")
		}
	}
}

func TestRenderer_ToProvider_WithGRPC(t *testing.T) {
	renderer := NewRenderer()
	model := contracts.ContainerModel{
		Type:    contracts.ContainerXray,
		APIPort: 62789,
		Inbounds: []contracts.InboundSpec{
			{
				Tag:      "grpc-in",
				Port:     443,
				Protocol: contracts.ProtocolVMess,
				Extensions: map[string]any{
					"transport":         "grpc",
					"security":          "tls",
					"server_name":       "example.com",
					"grpc_service_name": "vmess",
				},
			},
		},
	}

	native, err := renderer.ToProvider(model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(native.JSON, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	inbounds := m["inbounds"].([]interface{})

	var grpcInbound map[string]interface{}
	for _, in := range inbounds {
		inMap := in.(map[string]interface{})
		if inMap["tag"] == "grpc-in" {
			grpcInbound = inMap
			break
		}
	}

	streamSettings := grpcInbound["streamSettings"].(map[string]interface{})
	if streamSettings["network"] != "grpc" {
		t.Errorf("expected network grpc, got %v", streamSettings["network"])
	}

	grpcSettings := streamSettings["grpcSettings"].(map[string]interface{})
	if grpcSettings["serviceName"] != "vmess" {
		t.Errorf("expected serviceName vmess, got %v", grpcSettings["serviceName"])
	}
}
