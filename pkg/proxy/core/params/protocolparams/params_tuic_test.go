package protocolparams

import (
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

const tuicTestUUID = "00112233-4455-6677-8899-aabbccddeeff"

func tuicBaseRaw() map[string]any {
	return map[string]any{
		"protocol":  "tuic",
		"port":      uint32(443),
		"uuid":      tuicTestUUID,
		"password":  "secret",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestParseTUIC_Basic(t *testing.T) {
	pp, err := Parse(tuicBaseRaw())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Protocol != contracts.ProtocolTUIC {
		t.Errorf("Protocol = %q, want tuic", pp.Protocol)
	}
	if pp.TUIC == nil {
		t.Fatal("TUIC is nil")
	}
	if pp.TUIC.UUID != tuicTestUUID || pp.TUIC.Password != "secret" {
		t.Errorf("uuid/password not propagated: %+v", pp.TUIC)
	}
	if pp.Transport != nil {
		t.Errorf("Transport must be nil for tuic, got %+v", pp.Transport)
	}
	if pp.Security == nil || pp.Security.Kind != contracts.SecurityTLS {
		t.Fatalf("Security must default to tls, got %+v", pp.Security)
	}
	if pp.Security.TLS == nil ||
		pp.Security.TLS.CertFile != "/tmp/cert.pem" || pp.Security.TLS.KeyFile != "/tmp/key.pem" {
		t.Errorf("cert/key not propagated: %+v", pp.Security.TLS)
	}
}

func TestParseTUIC_AllowsEmptyCertPair(t *testing.T) {
	// Mirror parseHysteria2 behaviour: parser is permissive on empty cert
	// material, the adapter (inbound.validateTuic) enforces presence.
	raw := map[string]any{
		"protocol": "tuic",
		"port":     uint32(443),
		"uuid":     tuicTestUUID,
		"password": "secret",
	}
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security == nil || pp.Security.TLS == nil {
		t.Fatal("Security.TLS must be set even with empty cert/key")
	}
}

func TestParseTUIC_MissingUUID(t *testing.T) {
	raw := tuicBaseRaw()
	delete(raw, "uuid")
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseTUIC_MissingPassword(t *testing.T) {
	raw := tuicBaseRaw()
	delete(raw, "password")
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseTUIC_InvalidUUID(t *testing.T) {
	// mihomo Alpha listener silently zeros invalid uuids in users:; reject
	// at the parser so misconfigured deployments fail loud.
	raw := tuicBaseRaw()
	raw["uuid"] = "not-a-uuid"
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired for invalid uuid, got %v", err)
	}
}

func TestParseTUIC_RejectTransport(t *testing.T) {
	raw := tuicBaseRaw()
	raw["transport"] = "ws"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseTUIC_RejectSecurityNone(t *testing.T) {
	raw := tuicBaseRaw()
	raw["security"] = "none"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseTUIC_RejectSecurityReality(t *testing.T) {
	raw := tuicBaseRaw()
	raw["security"] = "reality"
	raw["reality_target"] = "www.microsoft.com:443"
	raw["reality_server_names"] = []string{"www.microsoft.com"}
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseTUIC_CongestionController_Whitelist(t *testing.T) {
	for _, cc := range []string{"bbr", "cubic", "new_reno"} {
		t.Run(cc, func(t *testing.T) {
			raw := tuicBaseRaw()
			raw["congestion_controller"] = cc
			pp, err := Parse(raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if pp.TUIC.CongestionController != cc {
				t.Errorf("CongestionController = %q, want %q", pp.TUIC.CongestionController, cc)
			}
		})
	}
}

func TestParseTUIC_CongestionController_Empty(t *testing.T) {
	raw := tuicBaseRaw()
	raw["congestion_controller"] = ""
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.TUIC.CongestionController != "" {
		t.Errorf("empty CongestionController should pass through (profilegen fills bbr); got %q",
			pp.TUIC.CongestionController)
	}
}

func TestParseTUIC_CongestionController_Reject(t *testing.T) {
	raw := tuicBaseRaw()
	raw["congestion_controller"] = "reno" // canonical is new_reno; reject other spellings
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseTUIC_UDPRelayMode_Whitelist(t *testing.T) {
	for _, mode := range []string{"native", "quic"} {
		t.Run(mode, func(t *testing.T) {
			raw := tuicBaseRaw()
			raw["udp_relay_mode"] = mode
			pp, err := Parse(raw)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if pp.TUIC.UDPRelayMode != mode {
				t.Errorf("UDPRelayMode = %q, want %q", pp.TUIC.UDPRelayMode, mode)
			}
		})
	}
}

func TestParseTUIC_UDPRelayMode_Reject(t *testing.T) {
	raw := tuicBaseRaw()
	raw["udp_relay_mode"] = "stream" // not a valid mihomo mode
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseTUIC_ZeroRTT_Bool(t *testing.T) {
	raw := tuicBaseRaw()
	raw["zero_rtt_handshake"] = true
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pp.TUIC.ZeroRTTHandshake {
		t.Error("ZeroRTTHandshake should be true")
	}
}

func TestParseTUIC_ZeroRTT_String(t *testing.T) {
	raw := tuicBaseRaw()
	raw["zero_rtt_handshake"] = "true"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pp.TUIC.ZeroRTTHandshake {
		t.Error("ZeroRTTHandshake should accept string 'true'")
	}
}

func TestParseTUIC_DisableSNI_Bool(t *testing.T) {
	raw := tuicBaseRaw()
	raw["disable_sni"] = true
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pp.TUIC.DisableSNI {
		t.Error("DisableSNI should be true")
	}
}

func TestParseTUIC_DisableSNI_String(t *testing.T) {
	// extra_params arrives as map[string]string from the RPC layer; the
	// HTTP handler emits "true" not bool true. parseTUIC must accept both.
	raw := tuicBaseRaw()
	raw["disable_sni"] = "true"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pp.TUIC.DisableSNI {
		t.Error("DisableSNI should accept string 'true'")
	}
}

func TestParseTUIC_DisableSNI_Default(t *testing.T) {
	// Absence ≠ false; verify the typed field defaults to false when the key
	// is missing (no ambiguous "missing vs explicit-false" trap).
	raw := tuicBaseRaw()
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.TUIC.DisableSNI {
		t.Error("DisableSNI should default to false")
	}
}

func TestParseTUIC_HeartbeatInterval_PassThrough(t *testing.T) {
	raw := tuicBaseRaw()
	raw["heartbeat_interval"] = "10s"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.TUIC.HeartbeatInterval != "10s" {
		t.Errorf("HeartbeatInterval = %q, want 10s", pp.TUIC.HeartbeatInterval)
	}
}

func TestParseTUIC_TLSExtras(t *testing.T) {
	raw := tuicBaseRaw()
	raw["sni"] = "tuic.example.com"
	raw["alpn"] = []string{"h3"}
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security.TLS.SNI != "tuic.example.com" {
		t.Errorf("SNI = %q", pp.Security.TLS.SNI)
	}
	if len(pp.Security.TLS.ALPN) != 1 || pp.Security.TLS.ALPN[0] != "h3" {
		t.Errorf("ALPN = %+v", pp.Security.TLS.ALPN)
	}
}

func TestParseTUIC_CongestionController_CaseAndWhitespace(t *testing.T) {
	raw := tuicBaseRaw()
	raw["congestion_controller"] = "  BBR  "
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.TUIC.CongestionController != "bbr" {
		t.Errorf("CongestionController = %q, want lowercased/trimmed bbr", pp.TUIC.CongestionController)
	}
}

// TestParseTUIC_IsReadOnly mirrors the per-protocol read-only check added
// in Phase 7 (params_anytls_test.go). Backfilled here so the "Parse
// mutates raw" trap is caught at the Phase 6 entry point too, not just
// when the integration test happens to exercise tuic with full flag
// coverage.
func TestParseTUIC_IsReadOnly(t *testing.T) {
	raw := tuicBaseRaw()
	raw["congestion_controller"] = "bbr"
	raw["udp_relay_mode"] = "native"
	raw["zero_rtt_handshake"] = true
	raw["heartbeat_interval"] = "10s"
	raw["disable_sni"] = true

	before := make(map[string]any, len(raw))
	for k, v := range raw {
		before[k] = v
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(raw) != len(before) {
		t.Errorf("Parse mutated raw map (size changed): %v -> %v", before, raw)
	}
	for k, want := range before {
		if got := raw[k]; got != want {
			t.Errorf("Parse mutated raw[%q]: %v -> %v", k, want, got)
		}
	}
}
