package protocolparams

import (
	"errors"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func hy2BaseRaw() map[string]any {
	return map[string]any{
		"protocol":  "hysteria2",
		"port":      uint32(443),
		"password":  "secret",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
	}
}

func TestParseHysteria2_Basic(t *testing.T) {
	pp, err := Parse(hy2BaseRaw())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Protocol != contracts.ProtocolHysteria2 {
		t.Errorf("Protocol = %q, want hysteria2", pp.Protocol)
	}
	if pp.Hysteria2 == nil {
		t.Fatal("Hysteria2 is nil")
	}
	if pp.Hysteria2.Password != "secret" {
		t.Errorf("Password = %q", pp.Hysteria2.Password)
	}
	if pp.Transport != nil {
		t.Errorf("Transport must be nil for hy2, got %+v", pp.Transport)
	}
	if pp.Security == nil || pp.Security.Kind != contracts.SecurityTLS {
		t.Fatalf("Security must default to tls, got %+v", pp.Security)
	}
	if pp.Security.TLS == nil {
		t.Fatal("Security.TLS is nil")
	}
	if pp.Security.TLS.CertFile != "/tmp/cert.pem" || pp.Security.TLS.KeyFile != "/tmp/key.pem" {
		t.Errorf("cert/key not propagated: %+v", pp.Security.TLS)
	}
}

func TestParseHysteria2_AllowsEmptyCertPair(t *testing.T) {
	// FillDefaults runs before Parse; if a caller bypasses FillDefaults with
	// no cert material, parseTLS still accepts the empty pair and lets the
	// adapter decide whether to reject. The mihomo adapter does enforce
	// presence in inbound.Validate; the parser layer stays permissive.
	raw := map[string]any{
		"protocol": "hysteria2",
		"port":     uint32(443),
		"password": "secret",
	}
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security == nil || pp.Security.TLS == nil {
		t.Fatal("Security.TLS must be set even with empty cert/key")
	}
	if pp.Security.TLS.CertFile != "" || pp.Security.TLS.KeyFile != "" {
		t.Errorf("expected empty cert/key, got %+v", pp.Security.TLS)
	}
}

func TestParseHysteria2_MissingPassword(t *testing.T) {
	raw := hy2BaseRaw()
	delete(raw, "password")
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got %v", err)
	}
}

func TestParseHysteria2_RejectTransport(t *testing.T) {
	raw := hy2BaseRaw()
	raw["transport"] = "ws"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseHysteria2_RejectSecurityNone(t *testing.T) {
	raw := hy2BaseRaw()
	raw["security"] = "none"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseHysteria2_RejectSecurityReality(t *testing.T) {
	raw := hy2BaseRaw()
	raw["security"] = "reality"
	raw["reality_target"] = "www.microsoft.com:443"
	raw["reality_server_names"] = []string{"www.microsoft.com"}
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseHysteria2_ObfsSalamander_OK(t *testing.T) {
	raw := hy2BaseRaw()
	raw["obfs"] = "salamander"
	raw["obfs_password"] = "obfsec"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Hysteria2.Obfs != "salamander" {
		t.Errorf("Obfs = %q", pp.Hysteria2.Obfs)
	}
	if pp.Hysteria2.ObfsPassword != "obfsec" {
		t.Errorf("ObfsPassword = %q", pp.Hysteria2.ObfsPassword)
	}
}

func TestParseHysteria2_ObfsSalamander_MissingPassword(t *testing.T) {
	raw := hy2BaseRaw()
	raw["obfs"] = "salamander"
	_, err := Parse(raw)
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired for obfs_password, got %v", err)
	}
}

func TestParseHysteria2_ObfsPasswordWithoutObfs(t *testing.T) {
	raw := hy2BaseRaw()
	raw["obfs_password"] = "stray"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination, got %v", err)
	}
}

func TestParseHysteria2_ObfsUnknown(t *testing.T) {
	raw := hy2BaseRaw()
	raw["obfs"] = "snowflake"
	raw["obfs_password"] = "ignored"
	_, err := Parse(raw)
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("expected ErrInvalidCombination for unknown obfs, got %v", err)
	}
}

func TestParseHysteria2_BandwidthAndMasquerade(t *testing.T) {
	raw := hy2BaseRaw()
	raw["up"] = "50 Mbps"
	raw["down"] = "100 Mbps"
	raw["masquerade"] = "https://www.microsoft.com"
	raw["ignore_client_bandwidth"] = true
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Hysteria2.Up != "50 Mbps" || pp.Hysteria2.Down != "100 Mbps" {
		t.Errorf("up/down not propagated: %+v", pp.Hysteria2)
	}
	if pp.Hysteria2.Masquerade != "https://www.microsoft.com" {
		t.Errorf("Masquerade = %q", pp.Hysteria2.Masquerade)
	}
	if !pp.Hysteria2.IgnoreClientBandwidth {
		t.Error("IgnoreClientBandwidth should be true")
	}
}

func TestParseHysteria2_IgnoreClientBandwidth_String(t *testing.T) {
	raw := hy2BaseRaw()
	raw["ignore_client_bandwidth"] = "true"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !pp.Hysteria2.IgnoreClientBandwidth {
		t.Error("IgnoreClientBandwidth should accept string 'true'")
	}
}

func TestParseHysteria2_IgnoreClientBandwidth_FalseString(t *testing.T) {
	raw := hy2BaseRaw()
	raw["ignore_client_bandwidth"] = "false"
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Hysteria2.IgnoreClientBandwidth {
		t.Error("IgnoreClientBandwidth should be false for string 'false'")
	}
}

// TestParseHysteria2_IgnoreClientBandwidth_Unparseable locks the
// optionalBool contract: a wholly wrong type (int 1) does not flip the
// flag — caller bug stays hidden as a zero-value default rather than
// silently flipping behaviour. parseHysteria2 has no obligation to
// reject this (the package-level convention is permissive), but
// recording the behaviour prevents drift.
func TestParseHysteria2_IgnoreClientBandwidth_Unparseable(t *testing.T) {
	raw := hy2BaseRaw()
	raw["ignore_client_bandwidth"] = 1 // not a bool, not a string — silently dropped
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Hysteria2.IgnoreClientBandwidth {
		t.Error("IgnoreClientBandwidth should default to false for unparseable type")
	}
}

func TestParseHysteria2_TLSExtras(t *testing.T) {
	raw := hy2BaseRaw()
	raw["sni"] = "hy2.example.com"
	raw["alpn"] = []string{"h3"}
	pp, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security.TLS.SNI != "hy2.example.com" {
		t.Errorf("SNI = %q", pp.Security.TLS.SNI)
	}
	if len(pp.Security.TLS.ALPN) != 1 || pp.Security.TLS.ALPN[0] != "h3" {
		t.Errorf("ALPN = %+v", pp.Security.TLS.ALPN)
	}
}

// TestParseHysteria2_IsReadOnly mirrors the per-protocol read-only check
// added in Phase 7 (params_anytls_test.go). Backfilled here so the
// "Parse mutates raw" trap is caught at the Phase 5 entry point too,
// not just when the integration test happens to exercise hy2 with full
// flag coverage.
func TestParseHysteria2_IsReadOnly(t *testing.T) {
	raw := hy2BaseRaw()
	raw["obfs"] = "salamander"
	raw["obfs_password"] = "obf"
	raw["up"] = "50 Mbps"
	raw["down"] = "100 Mbps"
	raw["masquerade"] = "https://example.com"
	raw["ignore_client_bandwidth"] = true

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
