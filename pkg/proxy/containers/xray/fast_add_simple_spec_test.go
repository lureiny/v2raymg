// Package xray provides Xray container implementation.
package xray

import (
	"encoding/json"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// buildFastAddSimpleSpec must emit credentials via extensions["users"], the only
// key Adapter.ToProvider reads. This white-box test runs without a live xray
// binary (unlike the FastAddInbound integration tests) so CI always exercises
// it. Before the fix, simple-path wrote extensions["uuid"]/["password"], which
// the adapter ignored, yielding settings.clients == [] (a dead inbound).
func TestBuildFastAddSimpleSpec_ProducesClients(t *testing.T) {
	cases := []struct {
		protocol contracts.Protocol
		params   map[string]any
		credKey  string // native client field that must be non-empty
	}{
		{contracts.ProtocolVMess, map[string]any{"uuid": "11111111-1111-1111-1111-111111111111"}, "id"},
		{contracts.ProtocolVLess, map[string]any{"uuid": "22222222-2222-2222-2222-222222222222"}, "id"},
		{contracts.ProtocolTrojan, map[string]any{"password": "s3cret"}, "password"},
	}

	adapter := NewAdapter()
	for _, tc := range cases {
		t.Run(string(tc.protocol), func(t *testing.T) {
			spec, err := buildFastAddSimpleSpec("t-"+string(tc.protocol), 20000, tc.protocol, tc.params)
			if err != nil {
				t.Fatalf("buildFastAddSimpleSpec: %v", err)
			}
			native, err := adapter.ToProvider(spec)
			if err != nil {
				t.Fatalf("ToProvider: %v", err)
			}
			var raw map[string]any
			if err := json.Unmarshal(native.JSON, &raw); err != nil {
				t.Fatalf("unmarshal native json: %v", err)
			}
			settings, ok := raw["settings"].(map[string]any)
			if !ok {
				t.Fatalf("no settings in native json: %s", native.JSON)
			}
			clients, ok := settings["clients"].([]any)
			if !ok || len(clients) == 0 {
				t.Fatalf("expected non-empty settings.clients, got: %v", settings["clients"])
			}
			first, _ := clients[0].(map[string]any)
			if v, _ := first[tc.credKey].(string); v == "" {
				t.Fatalf("expected non-empty client %q, got clients[0]=%v", tc.credKey, first)
			}
		})
	}
}

// TestBuildFastAddSimpleSpec_GeneratesCredentialWhenAbsent ensures a credential
// is auto-generated when the caller omits it, so the resulting inbound is never
// credential-less.
func TestBuildFastAddSimpleSpec_GeneratesCredentialWhenAbsent(t *testing.T) {
	adapter := NewAdapter()
	for _, p := range []contracts.Protocol{contracts.ProtocolVMess, contracts.ProtocolVLess, contracts.ProtocolTrojan} {
		spec, err := buildFastAddSimpleSpec("t-gen-"+string(p), 20001, p, map[string]any{})
		if err != nil {
			t.Fatalf("%s: buildFastAddSimpleSpec: %v", p, err)
		}
		native, err := adapter.ToProvider(spec)
		if err != nil {
			t.Fatalf("%s: ToProvider: %v", p, err)
		}
		var raw map[string]any
		_ = json.Unmarshal(native.JSON, &raw)
		settings, _ := raw["settings"].(map[string]any)
		clients, ok := settings["clients"].([]any)
		if !ok || len(clients) == 0 {
			t.Fatalf("%s: expected auto-generated credential, got empty clients", p)
		}
	}
}

// TestCredentialLessInbound checks the guard predicate: vmess/vless keyed on
// uuid, trojan on password, others always allowed.
func TestCredentialLessInbound(t *testing.T) {
	if !credentialLessInbound(contracts.ProtocolVMess, "", "") {
		t.Error("vmess with empty uuid should be credential-less")
	}
	if credentialLessInbound(contracts.ProtocolVMess, "uuid", "") {
		t.Error("vmess with uuid should be ok")
	}
	if !credentialLessInbound(contracts.ProtocolTrojan, "", "") {
		t.Error("trojan with empty password should be credential-less")
	}
	if credentialLessInbound(contracts.ProtocolTrojan, "", "pw") {
		t.Error("trojan with password should be ok")
	}
	// socks5/http/shadowsocks are never flagged (per-user or settings-level auth)
	if credentialLessInbound(contracts.ProtocolShadowsocks, "", "") {
		t.Error("shadowsocks must not be flagged by this guard")
	}
	if credentialLessInbound(contracts.ProtocolSOCKS5, "", "") {
		t.Error("socks must not be flagged by this guard")
	}
}
