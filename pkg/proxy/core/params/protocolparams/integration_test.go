package protocolparams_test

// Integration-level test for the FillDefaults → Parse pipeline. Phase 0
// only has the parser stubs; this file asserts the contract that matters
// before any protocol branch is wired up:
//
//  1. FillDefaults populates the credential + cert fields a caller would
//     otherwise have to fill manually.
//  2. Parse is read-only. Even when Parse returns an error for an
//     unsupported/stubbed protocol, the raw map the caller passed in
//     remains untouched (mihomo's adapter still reads from the same map,
//     so mutation here would silently corrupt downstream behaviour).
//
// Subsequent phases extend this file with happy-path assertions (Parse
// returns a populated *ProtocolParams with the right sub-spec).

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

func TestIntegrationFillDefaultsThenParseVMess(t *testing.T) {
	raw := map[string]any{
		"protocol": "vmess",
		"port":     uint32(12345),
		"tag":      "test-inbound",
	}
	if err := params.FillDefaults(raw, "vmess", nil, ""); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if _, ok := raw["uuid"].(string); !ok {
		t.Fatalf("FillDefaults should have filled uuid")
	}

	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VMess == nil || pp.VMess.UUID == "" {
		t.Fatalf("VMess sub-spec not populated: %+v", pp)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map.\n before: %+v\n after:  %+v", before, raw)
	}
}

func TestIntegrationFillDefaultsThenParseTrojan(t *testing.T) {
	dir := t.TempDir()
	raw := map[string]any{
		"protocol":    "trojan",
		"port":        uint32(443),
		"tag":         "trojan-inbound",
		"self_signed": true,
		"server_name": "trojan.test",
	}
	if err := params.FillDefaults(raw, "trojan", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if raw["password"] == nil || raw["password"] == "" {
		t.Fatalf("FillDefaults should have filled trojan password")
	}
	if raw["cert_file"] == nil || raw["key_file"] == nil {
		t.Fatalf("FillDefaults should have materialised cert_file/key_file for trojan")
	}

	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Trojan == nil || pp.Trojan.Password == "" {
		t.Fatalf("Trojan sub-spec not populated: %+v", pp)
	}
	if pp.Security == nil || pp.Security.TLS == nil {
		t.Fatalf("Trojan TLS spec not populated: %+v", pp.Security)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map.\n before: %+v\n after:  %+v", before, raw)
	}
}

func TestIntegrationFillDefaultsThenParseTrojanReality(t *testing.T) {
	raw := map[string]any{
		"protocol":              "trojan",
		"port":                  uint32(443),
		"tag":                   "trojan-reality",
		"password":              "p",
		"security":              "reality",
		"reality_target":        "www.microsoft.com:443",
		"reality_server_names":  []string{"www.microsoft.com"},
		"reality_short_ids":     []string{"aabbccddeeff0011"},
		"grpc_service_name":     "TrojanTunnel",
		"utls_fingerprint":      "chrome",
		"unrelated_passthrough": "kept",
	}
	if err := params.FillDefaults(raw, "trojan", nil, t.TempDir()); err != nil {
		t.Fatalf("FillDefaults should not require cert source for trojan+reality: %v", err)
	}
	if _, ok := raw["cert_file"]; ok {
		t.Fatalf("FillDefaults materialised cert_file for trojan+reality: %+v", raw)
	}
	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Trojan == nil || pp.Security == nil || pp.Security.Reality == nil {
		t.Fatalf("Trojan reality sub-spec not populated: %+v", pp)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map.\n before: %+v\n after:  %+v", before, raw)
	}
}

func TestIntegrationFillDefaultsThenParseHysteria2(t *testing.T) {
	dir := t.TempDir()
	raw := map[string]any{
		"protocol":    "hysteria2",
		"port":        uint32(443),
		"self_signed": true,
		"server_name": "hy2.test",
	}
	if err := params.FillDefaults(raw, "hysteria2", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	// Credentials
	if raw["password"] == nil || raw["password"] == "" {
		t.Fatalf("FillDefaults should have filled hysteria2 password")
	}
	// TLS material (needsTLS path)
	if raw["cert_file"] == nil || raw["key_file"] == nil {
		t.Fatalf("FillDefaults should have materialised cert_file/key_file for hysteria2")
	}
	if raw["cert_source"] != "self_signed" {
		t.Errorf("cert_source = %v, want self_signed", raw["cert_source"])
	}

	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Hysteria2 == nil {
		t.Fatal("Parse did not populate Hysteria2Params")
	}
	if pp.Hysteria2.Password == "" {
		t.Error("Hysteria2.Password empty after Parse")
	}
	if pp.Security == nil || pp.Security.TLS == nil {
		t.Fatal("Parse did not populate Security.TLS for hy2")
	}
	if pp.Security.TLS.CertFile == "" || pp.Security.TLS.KeyFile == "" {
		t.Errorf("TLS cert/key not propagated: cert=%q key=%q",
			pp.Security.TLS.CertFile, pp.Security.TLS.KeyFile)
	}
	if pp.Transport != nil {
		t.Errorf("Transport must be nil for hy2, got %+v", pp.Transport)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map")
	}
}

func TestIntegrationFillDefaultsThenParseTUIC(t *testing.T) {
	dir := t.TempDir()
	raw := map[string]any{
		"protocol":    "tuic",
		"port":        uint32(8443),
		"self_signed": true,
	}
	if err := params.FillDefaults(raw, "tuic", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if raw["uuid"] == nil || raw["password"] == nil {
		t.Fatalf("FillDefaults should have filled tuic uuid+password, got: %+v", raw)
	}

	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse after FillDefaults: %v", err)
	}
	if pp.TUIC == nil || pp.TUIC.UUID == "" || pp.TUIC.Password == "" {
		t.Fatalf("TUIC slot not populated: %+v", pp.TUIC)
	}
	if pp.Security == nil || pp.Security.Kind != contracts.SecurityTLS ||
		pp.Security.TLS == nil || pp.Security.TLS.CertFile == "" || pp.Security.TLS.KeyFile == "" {
		t.Fatalf("Security/TLS not propagated from self-signed cert resolution: %+v", pp.Security)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map")
	}
}

func TestIntegrationFillDefaultsThenParseAnyTLS(t *testing.T) {
	dir := t.TempDir()
	raw := map[string]any{
		"protocol":    "anytls",
		"port":        uint32(1443),
		"self_signed": true,
	}
	if err := params.FillDefaults(raw, "anytls", nil, dir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}
	if raw["password"] == nil {
		t.Fatalf("FillDefaults should have filled anytls password")
	}

	before := copyMap(raw)
	pp, err := protocolparams.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.AnyTLS == nil {
		t.Fatalf("Parse: AnyTLS slot is nil")
	}
	if pp.AnyTLS.Password == "" {
		t.Fatalf("Parse: AnyTLS.Password lost")
	}
	if pp.Transport != nil {
		t.Errorf("Parse: AnyTLS must have nil Transport (TCP-native), got %+v", pp.Transport)
	}
	if pp.Security == nil || pp.Security.Kind != contracts.SecurityTLS {
		t.Errorf("Parse: AnyTLS requires Security.Kind=tls, got %+v", pp.Security)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw map")
	}
}

// TestParseIsReadOnlyAcrossAllProtocols loops the 7 protocols in the
// ProtocolParams scope and asserts Parse never mutates the raw map. Even
// when the Phase 0 stub returns an error, the caller's map should be
// immutable — mihomo's FastAddInbound continues to read from the same
// map after Parse errors in current code, so any mutation here corrupts
// the downstream path.
func TestParseIsReadOnlyAcrossAllProtocols(t *testing.T) {
	cases := []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic", "anytls"}
	for _, proto := range cases {
		t.Run(proto, func(t *testing.T) {
			raw := map[string]any{
				"protocol": proto,
				"port":     uint32(1080),
				"uuid":     "deadbeef",
				"password": "p",
				"cipher":   "aes-256-gcm",
			}
			before := copyMap(raw)
			_, _ = protocolparams.Parse(raw)
			if !reflect.DeepEqual(raw, before) {
				t.Errorf("%s: Parse mutated raw map\n before: %+v\n after: %+v", proto, before, raw)
			}
		})
	}
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
