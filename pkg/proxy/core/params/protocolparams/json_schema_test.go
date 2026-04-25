package protocolparams

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestProtocolParamsJSONTagSchema is a golden-shape regression guard.
//
// ProtocolParams and its nested structs are persisted to InboundStore via
// MihomoInbound.ToNative. Without JSON tags the on-disk shape was Go's
// default PascalCase ("Protocol", "VLESS", "UUID", "ShortIDs"), which
// means any field rename silently breaks deserialisation of existing
// records. This test asserts the exact set of top-level JSON keys we
// promise to write to disk — renames must come with a migration plan
// and an updated golden shape here.
func TestProtocolParamsJSONTagSchema(t *testing.T) {
	// Deliberately populate every field we care about persisting, so that
	// omitempty doesn't hide a missing tag. Values don't need to be
	// semantically valid — we're asserting shape, not behaviour.
	pp := &ProtocolParams{
		Protocol:   contracts.ProtocolVLess,
		Tag:        "t",
		Port:       443,
		ListenAddr: "127.0.0.1",
		Transport: &TransportSpec{
			Kind:            contracts.TransportWS,
			WSPath:          "/p",
			WSHost:          "h",
			GRPCServiceName: "g",
			HTTPUpgradePath: "/u",
			HTTPUpgradeHost: "uh",
			XHTTPPath:       "/xh",
			XHTTPHost:       "xh",
			XHTTPMode:       "auto",
			HTTPPath:        "/h",
			HTTPHost:        []string{"a", "b"},
		},
		Security: &SecuritySpec{
			Kind: contracts.SecurityTLS,
			// Seed every field — including bool omitempty fields — so
			// the strict-key check in assertKeySet catches any missing
			// json tag. Phase 1 review caught this pattern (P0-2);
			// Phase 2 review caught a SkipCertVerify regression of it.
			// Rule for seed maintenance: bool fields = true,
			// string omitempty = non-empty, slices = non-nil-non-empty.
			TLS: &TLSSpec{
				SNI:              "sni",
				ALPN:             []string{"h2"},
				CertFile:         "/c",
				KeyFile:          "/k",
				CertSource:       "file",
				MinVersion:       "1.2",
				RejectUnknownSNI: true,
				UTLSFingerprint:  "chrome",
				SkipCertVerify:   true,
			},
			Reality: &RealitySpec{
				Target:       "example.com:443",
				ServerNames:  []string{"example.com"},
				ShortIDs:     []string{"aabbccddeeff0011"},
				PrivateKey:   "priv",
				PublicKey:    "pub",
				MinClientVer: "1.8.0",
				MaxTimeDiff:  "60",
			},
		},
		VLESS:     &VLESSParams{UUID: "u", Flow: "f", Decryption: "none"},
		VMess:     &VMessParams{UUID: "u", AlterID: 1, Cipher: "auto"},
		Trojan:    &TrojanParams{Password: "p"},
		SS:        &SSParams{Password: "p", Cipher: "aes-256-gcm", UDP: true, Plugin: "x", PluginOpts: map[string]string{"a": "b"}},
		Hysteria2: &Hysteria2Params{Password: "p", Obfs: "salamander", ObfsPassword: "op", Up: "50 Mbps", Down: "100 Mbps", Masquerade: "https://example.com", IgnoreClientBandwidth: true},
		TUIC:      &TUICParams{UUID: "u", Password: "p", CongestionController: "bbr", UDPRelayMode: "native", ZeroRTTHandshake: true, HeartbeatInterval: "10s"},
		AnyTLS:    &AnyTLSParams{Password: "p", PaddingScheme: "scheme", IdleSessionCheckInterval: "30s", IdleSessionTimeout: "60s", MinIdleSession: 3},
	}

	data, err := json.Marshal(pp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Top-level snake_case keys. Any rename or missing tag shows up here.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("unmarshal top-level: %v", err)
	}
	wantTop := []string{
		"protocol", "tag", "port", "listen_addr",
		"transport", "security",
		"vless", "vmess", "trojan", "ss", "hysteria2", "tuic", "anytls",
	}
	assertKeySet(t, "ProtocolParams", top, wantTop)

	// Nested: transport
	var transport map[string]json.RawMessage
	if err := json.Unmarshal(top["transport"], &transport); err != nil {
		t.Fatalf("unmarshal transport: %v", err)
	}
	assertKeySet(t, "TransportSpec", transport, []string{
		"kind",
		"ws_path", "ws_host",
		"grpc_service_name",
		"httpupgrade_path", "httpupgrade_host",
		"xhttp_path", "xhttp_host", "xhttp_mode",
		"http_path", "http_host",
	})

	// Nested: security
	var security map[string]json.RawMessage
	if err := json.Unmarshal(top["security"], &security); err != nil {
		t.Fatalf("unmarshal security: %v", err)
	}
	assertKeySet(t, "SecuritySpec", security, []string{"kind", "tls", "reality"})

	// Nested: tls
	var tls map[string]json.RawMessage
	if err := json.Unmarshal(security["tls"], &tls); err != nil {
		t.Fatalf("unmarshal tls: %v", err)
	}
	assertKeySet(t, "TLSSpec", tls, []string{
		"sni", "alpn", "cert_file", "key_file", "cert_source",
		"min_version", "reject_unknown_sni", "utls_fingerprint",
		"skip_cert_verify",
	})

	// Nested: reality
	var reality map[string]json.RawMessage
	if err := json.Unmarshal(security["reality"], &reality); err != nil {
		t.Fatalf("unmarshal reality: %v", err)
	}
	assertKeySet(t, "RealitySpec", reality, []string{
		"target", "server_names", "short_ids",
		"private_key", "public_key", "min_client_ver", "max_time_diff",
	})

	// Nested: per-protocol
	assertNestedKeys(t, top["vless"], "VLESSParams", []string{"uuid", "flow", "decryption"})
	assertNestedKeys(t, top["vmess"], "VMessParams", []string{"uuid", "alter_id", "cipher"})
	assertNestedKeys(t, top["trojan"], "TrojanParams", []string{"password"})
	assertNestedKeys(t, top["ss"], "SSParams", []string{"password", "cipher", "udp", "plugin", "plugin_opts"})
	assertNestedKeys(t, top["hysteria2"], "Hysteria2Params", []string{
		"password", "obfs", "obfs_password", "up", "down", "masquerade", "ignore_client_bandwidth",
	})
	assertNestedKeys(t, top["tuic"], "TUICParams", []string{
		"uuid", "password", "congestion_controller", "udp_relay_mode", "zero_rtt_handshake", "heartbeat_interval",
	})
	assertNestedKeys(t, top["anytls"], "AnyTLSParams", []string{
		"password", "padding_scheme", "idle_session_check_interval", "idle_session_timeout", "min_idle_session",
	})
}

func assertKeySet(t *testing.T, label string, got map[string]json.RawMessage, want []string) {
	t.Helper()
	gotKeys := make(map[string]struct{}, len(got))
	for k := range got {
		gotKeys[k] = struct{}{}
	}
	wantKeys := make(map[string]struct{}, len(want))
	for _, k := range want {
		wantKeys[k] = struct{}{}
	}
	// Missing expected keys
	for k := range wantKeys {
		if _, ok := gotKeys[k]; !ok {
			t.Errorf("%s: missing expected json key %q (got keys: %v)", label, k, keysOf(gotKeys))
		}
	}
	// Unexpected new keys — flag them so accidentally Exported field
	// renames / additions are noticed.
	for k := range gotKeys {
		if _, ok := wantKeys[k]; !ok {
			t.Errorf("%s: unexpected json key %q (update schema test if intentional)", label, k)
		}
	}
}

func assertNestedKeys(t *testing.T, raw json.RawMessage, label string, want []string) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: unmarshal: %v", label, err)
	}
	assertKeySet(t, label, m, want)
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestProtocolParamsJSONRoundTrip asserts marshal → unmarshal restores
// every field. Guards against a tag being written but the unmarshal
// path being broken by a typo.
func TestProtocolParamsJSONRoundTrip(t *testing.T) {
	orig := &ProtocolParams{
		Protocol:   contracts.ProtocolVLess,
		Tag:        "t",
		Port:       443,
		ListenAddr: "127.0.0.1",
		Transport:  &TransportSpec{Kind: contracts.TransportWS, WSPath: "/p"},
		Security: &SecuritySpec{
			Kind:    contracts.SecurityReality,
			Reality: &RealitySpec{Target: "example.com:443", ServerNames: []string{"example.com"}, ShortIDs: []string{"aabbccddeeff0011"}, PrivateKey: "priv", PublicKey: "pub"},
		},
		VLESS: &VLESSParams{UUID: "u", Flow: "xtls-rprx-vision", Decryption: "none"},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ProtocolParams
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(orig, &back) {
		t.Errorf("round-trip mismatch.\n orig: %+v\n back: %+v", orig, back)
	}
}
