package converter

import (
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"gopkg.in/yaml.v3"
)

// --- convertVLess tests ---

func TestConvertVLess_TLS_WS(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "hk.example.com",
		Port:     443,
		Password: "uuid-vless",
		NodeName: "HK",
		Extensions: map[string]any{
			"security":    "tls",
			"transport":   "ws",
			"server_name": "hk.example.com",
			"ws_path":     "/ws",
			"ws_host":     "hk.example.com",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Type != "vless" {
		t.Errorf("type = %q, want vless", p.Type)
	}
	if !p.TLS {
		t.Error("expected TLS=true")
	}
	if p.Servername != "hk.example.com" {
		t.Errorf("servername = %q, want hk.example.com", p.Servername)
	}
	if p.Network != "ws" {
		t.Errorf("network = %q, want ws", p.Network)
	}
	if p.WSOpts == nil || p.WSOpts.Path != "/ws" {
		t.Error("expected ws-opts.path=/ws")
	}
	if p.WSOpts.Headers == nil || p.WSOpts.Headers.Host != "hk.example.com" {
		t.Error("expected ws-opts.headers.Host=hk.example.com")
	}
}

func TestConvertVLess_TLS_GRPC(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "us.example.com",
		Port:     443,
		Password: "uuid-vless",
		NodeName: "US",
		Extensions: map[string]any{
			"security":          "tls",
			"transport":         "grpc",
			"grpc_service_name": "my-service",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Network != "grpc" {
		t.Errorf("network = %q, want grpc", p.Network)
	}
	if p.GrpcOpts == nil || p.GrpcOpts.GrpcServiceName != "my-service" {
		t.Error("expected grpc-opts.grpc-service-name=my-service")
	}
}

func TestConvertVLess_Reality(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vless",
		NodeName: "SG",
		Extensions: map[string]any{
			"security":           "reality",
			"transport":          "tcp",
			"server_name":        "sg.example.com",
			"reality_public_key": "pubkey123",
			"reality_short_ids":  []string{"abc123", "def456"},
			"flow":               "xtls-rprx-vision",
			"utls_fingerprint":   "chrome",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if !p.TLS {
		t.Error("expected TLS=true for reality")
	}
	if p.RealityOpts == nil {
		t.Fatal("expected reality-opts")
	}
	if p.RealityOpts.PublicKey != "pubkey123" {
		t.Errorf("public-key = %q, want pubkey123", p.RealityOpts.PublicKey)
	}
	if p.RealityOpts.ShortID != "abc123" {
		t.Errorf("short-id = %q, want abc123 (first)", p.RealityOpts.ShortID)
	}
	if p.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %q, want xtls-rprx-vision", p.Flow)
	}
	if p.ClientFingerprint != "chrome" {
		t.Errorf("client-fingerprint = %q, want chrome", p.ClientFingerprint)
	}
}

func TestConvertVLess_XHTTP(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "jp.example.com",
		Port:     443,
		Password: "uuid-vless",
		NodeName: "JP",
		Extensions: map[string]any{
			"security":   "tls",
			"transport":  "xhttp",
			"xhttp_path": "/xhttp",
			"xhttp_host": []string{"jp.example.com"},
			"xhttp_mode": "stream-one",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Network != "xhttp" {
		t.Errorf("network = %q, want xhttp", p.Network)
	}
	if p.XHTTPOpts == nil {
		t.Fatal("expected xhttp-opts")
	}
	if p.XHTTPOpts.Path != "/xhttp" {
		t.Errorf("path = %q, want /xhttp", p.XHTTPOpts.Path)
	}
	if p.XHTTPOpts.Mode != "stream-one" {
		t.Errorf("mode = %q, want stream-one", p.XHTTPOpts.Mode)
	}
}

func TestConvertVLess_Splithttp_MapsToXHTTP(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "jp.example.com",
		Port:     443,
		Password: "uuid-vless",
		NodeName: "JP",
		Extensions: map[string]any{
			"security":  "tls",
			"transport": "splithttp",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Network != "xhttp" {
		t.Errorf("splithttp should map to network=xhttp, got %q", p.Network)
	}
}

func TestConvertVLess_HTTPUpgrade_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"security":  "tls",
			"transport": "httpupgrade",
		},
	}
	if p := c.convertVLess(spec); p != nil {
		t.Error("expected nil for httpupgrade transport (not supported by Mihomo VLESS)")
	}
}

func TestConvertVLess_XTLS_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"security": "xtls",
		},
	}
	if p := c.convertVLess(spec); p != nil {
		t.Error("expected nil for xtls security (not supported by Mihomo VLESS)")
	}
}

// --- convertVMess tests ---

func TestConvertVMess_HTTPUpgrade_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"transport": "httpupgrade",
		},
	}
	if p := c.convertVMess(spec); p != nil {
		t.Error("expected nil for httpupgrade transport (not supported by Mihomo VMess)")
	}
}

func TestConvertVMess_XHTTP_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"transport": "xhttp",
		},
	}
	if p := c.convertVMess(spec); p != nil {
		t.Error("expected nil for xhttp transport (not supported by Mihomo VMess)")
	}
}

func TestConvertVMess_KCP_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"transport": "mkcp",
		},
	}
	if p := c.convertVMess(spec); p != nil {
		t.Error("expected nil for mkcp transport (not supported by Mihomo VMess)")
	}
}

func TestConvertVMess_QUIC_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"transport": "quic",
		},
	}
	if p := c.convertVMess(spec); p != nil {
		t.Error("expected nil for quic transport (not supported by Mihomo VMess)")
	}
}

func TestConvertVMess_ClientFingerprint(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"security":         "tls",
			"transport":        "tcp",
			"utls_fingerprint": "firefox",
		},
	}
	p := c.convertVMess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != "firefox" {
		t.Errorf("client-fingerprint = %q, want firefox", p.ClientFingerprint)
	}
}

// TestConvertVMess_Reality covers the Phase 2 review P0: convertVMess
// must emit tls=true + RealityOpts for security=reality. Without this,
// the clash client falls back to plain TLS and the handshake fails.
// Mirrors TestConvertVLess_Reality intent.
func TestConvertVMess_Reality(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vmess",
		NodeName: "SG",
		Extensions: map[string]any{
			"security":           "reality",
			"transport":          "tcp",
			"server_name":        "sg.example.com",
			"reality_public_key": "vmessPubKey",
			"reality_short_ids":  []string{"abc123", "def456"},
			"utls_fingerprint":   "chrome",
		},
	}
	p := c.convertVMess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if !p.TLS {
		t.Error("expected TLS=true for reality (mihomo client requirement)")
	}
	if p.RealityOpts == nil {
		t.Fatal("expected reality-opts populated")
	}
	if p.RealityOpts.PublicKey != "vmessPubKey" {
		t.Errorf("public-key = %q, want vmessPubKey", p.RealityOpts.PublicKey)
	}
	if p.RealityOpts.ShortID != "abc123" {
		t.Errorf("short-id = %q, want abc123 (first)", p.RealityOpts.ShortID)
	}
	if p.Servername != "sg.example.com" {
		t.Errorf("servername = %q, want sg.example.com", p.Servername)
	}
	if p.ClientFingerprint != "chrome" {
		t.Errorf("client-fingerprint = %q, want chrome", p.ClientFingerprint)
	}
}

// TestConvertVMess_LegacyAlterID covers the Phase 2 review P1-1:
// AlterID > 0 (legacy MD5-auth VMess) must reach the converter via
// Extensions["alter_id"] so the clash client uses legacy mode. Without
// this transit the clash output silently downgrades AEAD-only.
//
// Note: alterId > 0 is legacy v2/v3 mode (non-AEAD); AEAD is the modern
// default at alterId=0. Real-world traffic almost never sets alterId>0,
// but we keep the field plumbed correctly for callers that need it.
func TestConvertVMess_LegacyAlterID(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "example.com",
		Port:     443,
		Password: "uuid",
		Extensions: map[string]any{
			"security":  "tls",
			"transport": "tcp",
			"alter_id":  64,
		},
	}
	p := c.convertVMess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.AlterId == nil {
		t.Fatal("AlterId pointer nil")
	}
	if *p.AlterId != 64 {
		t.Errorf("AlterId = %d, want 64", *p.AlterId)
	}
}

// --- convertTrojan tests ---

func TestConvertTrojan_Reality(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "trojan-pwd",
		NodeName: "SG",
		Extensions: map[string]any{
			"security":           "reality",
			"server_name":        "example.com",
			"reality_public_key": "realPubKey",
			"reality_short_ids":  "shortid1",
			"utls_fingerprint":   "safari",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy for reality Trojan")
	}
	if !p.TLS {
		t.Error("expected TLS=true for reality Trojan")
	}
	if p.RealityOpts == nil {
		t.Fatal("expected reality-opts")
	}
	if p.RealityOpts.PublicKey != "realPubKey" {
		t.Errorf("public-key = %q, want realPubKey", p.RealityOpts.PublicKey)
	}
	if p.RealityOpts.ShortID != "shortid1" {
		t.Errorf("short-id = %q, want shortid1", p.RealityOpts.ShortID)
	}
	if p.ClientFingerprint != "safari" {
		t.Errorf("client-fingerprint = %q, want safari", p.ClientFingerprint)
	}
}

// security=none is codec-normalized to "" before reaching the converter.
// If it somehow slips through (e.g. caller constructs spec directly), the
// converter treats it as plain TLS — trojan wire level requires TLS anyway.
func TestConvertTrojan_None_TreatedAsDefault(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "pwd",
		Extensions: map[string]any{
			"security": "none",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy (trojan is TLS regardless of security hint)")
	}
	if !p.TLS {
		t.Error("expected TLS=true")
	}
}

func TestConvertTrojan_HTTPUpgrade_ReturnsNil(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "pwd",
		Extensions: map[string]any{
			"security":  "tls",
			"transport": "httpupgrade",
		},
	}
	if p := c.convertTrojan(spec); p != nil {
		t.Error("expected nil for httpupgrade transport (not supported by Mihomo Trojan)")
	}
}

func TestConvertTrojan_Flow(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "pwd",
		NodeName: "SG",
		Extensions: map[string]any{
			"security":    "tls",
			"flow":        "xtls-rprx-vision",
			"server_name": "example.com",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %q, want xtls-rprx-vision", p.Flow)
	}
}

func TestConvertTrojan_XTLSSecurity_ProducesNode(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "pwd",
		Extensions: map[string]any{
			"security": "xtls",
			"flow":     "xtls-rprx-vision",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy: xtls should map to TLS+flow in mihomo")
	}
	if !p.TLS {
		t.Error("expected TLS=true")
	}
	if p.Flow != "xtls-rprx-vision" {
		t.Errorf("flow = %q, want xtls-rprx-vision", p.Flow)
	}
}

func TestConvertTrojan_SkipCertVerify(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "example.com",
		Port:     443,
		Password: "pwd",
		Extensions: map[string]any{
			"security":         "tls",
			"skip_cert_verify": true,
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if !p.SkipCertVerify {
		t.Error("expected skip-cert-verify=true")
	}
}

func TestConvertHysteria2_SNI(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolHysteria2,
		Host:     "hy2.example.com",
		Port:     443,
		Password: "pass",
		Extensions: map[string]any{
			"server_name": "hy2.example.com",
		},
	}
	p := c.convertHysteria2(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.SNI != "hy2.example.com" {
		t.Errorf("sni = %q, want hy2.example.com", p.SNI)
	}
}

func TestConvertHysteria2_SkipCertVerify_FromExtension(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolHysteria2,
		Host:     "hy2.example.com",
		Port:     443,
		Password: "pass",
		Extensions: map[string]any{
			"skip_cert_verify": true,
		},
	}
	p := c.convertHysteria2(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if !p.SkipCertVerify {
		t.Error("expected skip-cert-verify=true from extension")
	}
}

func TestConvertHysteria2_ObfsSalamander(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolHysteria2,
		Host:     "hy2.example.com",
		Port:     443,
		Password: "pass",
		Extensions: map[string]any{
			"obfs":          "salamander",
			"obfs_password": "shh",
		},
	}
	p := c.convertHysteria2(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Obfs != "salamander" {
		t.Errorf("Obfs = %q, want salamander", p.Obfs)
	}
	if p.ObfsPassword != "shh" {
		t.Errorf("ObfsPassword = %q, want shh", p.ObfsPassword)
	}
}

func TestConvertHysteria2_UpDown(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolHysteria2,
		Host:     "hy2.example.com",
		Port:     443,
		Password: "pass",
		Extensions: map[string]any{
			"up":   "50 Mbps",
			"down": "100 Mbps",
		},
	}
	p := c.convertHysteria2(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Up != "50 Mbps" {
		t.Errorf("Up = %q, want 50 Mbps", p.Up)
	}
	if p.Down != "100 Mbps" {
		t.Errorf("Down = %q, want 100 Mbps", p.Down)
	}
}

// Masquerade is server-only on mihomo's hysteria2 client schema; the
// converter must NOT propagate it to ClashProxy even when Extensions
// carries it (the listener emits the value but the client config drops it).
//
// We validate by yaml-marshalling the converter output and asserting the
// `masquerade` key is absent. Earlier versions only spot-checked `Up`/`Down`,
// which gave a false sense of safety: a future "ClashProxy.Masquerade
// string `yaml:\"masquerade\"`" plus a one-line write in convertHysteria2
// would silently slip through. The marshal-then-Contains check fails the
// moment any field on ClashProxy serialises a `masquerade:` key.
func TestConvertHysteria2_DropsMasquerade(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolHysteria2,
		Host:     "hy2.example.com",
		Port:     443,
		Password: "pass",
		Extensions: map[string]any{
			"masquerade": "https://www.microsoft.com",
		},
	}
	p := c.convertHysteria2(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	out, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	if strings.Contains(string(out), "masquerade") {
		t.Errorf("ClashProxy yaml output must not contain 'masquerade' (server-only field); got:\n%s", out)
	}
}

// --- buildRealityOpts tests ---

func TestBuildRealityOpts_StringShortID(t *testing.T) {
	ext := map[string]any{
		"reality_public_key": "pubkey",
		"reality_short_ids":  "id1",
	}
	opts := buildRealityOpts(ext)
	if opts == nil {
		t.Fatal("expected non-nil")
	}
	if opts.ShortID != "id1" {
		t.Errorf("short-id = %q, want id1", opts.ShortID)
	}
}

func TestBuildRealityOpts_SliceShortID(t *testing.T) {
	ext := map[string]any{
		"reality_public_key": "pubkey",
		"reality_short_ids":  []string{"first", "second"},
	}
	opts := buildRealityOpts(ext)
	if opts == nil {
		t.Fatal("expected non-nil")
	}
	if opts.ShortID != "first" {
		t.Errorf("short-id = %q, want first (first element)", opts.ShortID)
	}
}

func TestBuildRealityOpts_NoPubKey_ReturnsNil(t *testing.T) {
	ext := map[string]any{
		"reality_short_ids": "id1",
	}
	if opts := buildRealityOpts(ext); opts != nil {
		t.Error("expected nil when public-key is missing")
	}
}

// --- defaultRealityFingerprint fallback tests ---

func TestConvertVLess_Reality_DefaultFingerprint(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vless",
		Extensions: map[string]any{
			"security":           "reality",
			"reality_public_key": "pubkeyVL",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != defaultRealityFingerprint {
		t.Errorf("client-fingerprint = %q, want %q (reality fallback)", p.ClientFingerprint, defaultRealityFingerprint)
	}
}

func TestConvertVLess_Reality_ExplicitFingerprint(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVLess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vless",
		Extensions: map[string]any{
			"security":           "reality",
			"reality_public_key": "pubkeyVL",
			"utls_fingerprint":   "safari",
		},
	}
	p := c.convertVLess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != "safari" {
		t.Errorf("client-fingerprint = %q, want safari (caller override)", p.ClientFingerprint)
	}
}

func TestConvertVMess_Reality_DefaultFingerprint(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vmess",
		Extensions: map[string]any{
			"security":           "reality",
			"reality_public_key": "pubkeyVM",
		},
	}
	p := c.convertVMess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != defaultRealityFingerprint {
		t.Errorf("client-fingerprint = %q, want %q (reality fallback)", p.ClientFingerprint, defaultRealityFingerprint)
	}
}

func TestConvertVMess_TLS_NoFingerprintSet(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolVMess,
		Host:     "sg.example.com",
		Port:     443,
		Password: "uuid-vmess",
		Extensions: map[string]any{
			"security": "tls",
		},
	}
	p := c.convertVMess(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != "" {
		t.Errorf("client-fingerprint = %q, want empty for plain tls", p.ClientFingerprint)
	}
}

func TestConvertTrojan_Reality_DefaultFingerprint(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "sg.example.com",
		Port:     443,
		Password: "trojan-pw",
		Extensions: map[string]any{
			"security":           "reality",
			"reality_public_key": "pubkeyTR",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != defaultRealityFingerprint {
		t.Errorf("client-fingerprint = %q, want %q (reality fallback)", p.ClientFingerprint, defaultRealityFingerprint)
	}
}

func TestConvertTrojan_TLS_NoFingerprintSet(t *testing.T) {
	c := &ClashConverter{}
	spec := contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTrojan,
		Host:     "sg.example.com",
		Port:     443,
		Password: "trojan-pw",
		Extensions: map[string]any{
			"security": "tls",
		},
	}
	p := c.convertTrojan(spec)
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.ClientFingerprint != "" {
		t.Errorf("client-fingerprint = %q, want empty for plain tls", p.ClientFingerprint)
	}
}

// --- convertTuic tests (Phase 6) ---

const tuicConvertTestUUID = "00112233-4455-6677-8899-aabbccddeeff"

func tuicBaseSpec() contracts.SubscriptionSpec {
	return contracts.SubscriptionSpec{
		Protocol: contracts.ProtocolTUIC,
		Host:     "tuic.example.com",
		Port:     443,
		Password: "tuic-pw",
		Extensions: map[string]any{
			"uuid": tuicConvertTestUUID,
		},
	}
}

func TestConvertTuic_BasicShape(t *testing.T) {
	c := &ClashConverter{}
	p := c.convertTuic(tuicBaseSpec())
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.Type != "tuic" {
		t.Errorf("Type = %q, want tuic", p.Type)
	}
	if p.UUID != tuicConvertTestUUID {
		t.Errorf("UUID = %q", p.UUID)
	}
	if p.Password != "tuic-pw" {
		t.Errorf("Password = %q", p.Password)
	}
	if !p.UDP {
		t.Error("UDP should be true (TUIC is QUIC, UDP transport)")
	}
}

func TestConvertTuic_SNI(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["server_name"] = "tuic.example.com"
	p := c.convertTuic(spec)
	if p.SNI != "tuic.example.com" {
		t.Errorf("SNI = %q", p.SNI)
	}
}

func TestConvertTuic_SkipCertVerify_FromExtension(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["skip_cert_verify"] = true
	p := c.convertTuic(spec)
	if !p.SkipCertVerify {
		t.Error("expected skip-cert-verify=true from extension")
	}
}

func TestConvertTuic_SkipCertVerify_FromURI(t *testing.T) {
	// URI fallback for specs constructed without going through codec.Decode
	// (mirrors the hy2 insecure=1 fallback contract).
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.URI = "tuic://" + tuicConvertTestUUID + ":pwd@host:443?allow_insecure=1"
	p := c.convertTuic(spec)
	if !p.SkipCertVerify {
		t.Error("expected skip-cert-verify=true from URI allow_insecure=1 fallback")
	}
}

func TestConvertTuic_CongestionController(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["congestion_controller"] = "bbr"
	p := c.convertTuic(spec)
	if p.CongestionController != "bbr" {
		t.Errorf("CongestionController = %q", p.CongestionController)
	}
}

func TestConvertTuic_UDPRelayMode(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["udp_relay_mode"] = "quic"
	p := c.convertTuic(spec)
	if p.UDPRelayMode != "quic" {
		t.Errorf("UDPRelayMode = %q", p.UDPRelayMode)
	}
}

// ZeroRTTHandshake → reduce-rtt key (NOT zero-rtt-handshake — that's the
// wrong name and would be silently ignored by mihomo client).
func TestConvertTuic_ReduceRTT(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["zero_rtt_handshake"] = true
	p := c.convertTuic(spec)
	if !p.ReduceRTT {
		t.Error("expected reduce-rtt=true from zero_rtt_handshake")
	}
	out, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "reduce-rtt: true") {
		t.Errorf("yaml must emit reduce-rtt: true; got:\n%s", got)
	}
	// Belt-and-braces: the wrong-name key must never leak into yaml.
	if strings.Contains(got, "zero-rtt-handshake") {
		t.Errorf("yaml must NOT emit zero-rtt-handshake (wrong key for mihomo); got:\n%s", got)
	}
}

func TestConvertTuic_DisableSNI(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["disable_sni"] = true
	p := c.convertTuic(spec)
	if !p.DisableSNI {
		t.Error("expected disable-sni=true")
	}
}

func TestConvertTuic_HeartbeatInterval(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["heartbeat_interval"] = "10s"
	p := c.convertTuic(spec)
	if p.HeartbeatInterval != 10000 {
		t.Errorf("HeartbeatInterval = %d ms, want 10000", p.HeartbeatInterval)
	}
}

func TestConvertTuic_HeartbeatInterval_Unparseable(t *testing.T) {
	// Unparseable duration falls through 0; mihomo client uses its default
	// (10000ms) when the field is absent.
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["heartbeat_interval"] = "not-a-duration"
	p := c.convertTuic(spec)
	if p.HeartbeatInterval != 0 {
		t.Errorf("HeartbeatInterval = %d, want 0 for unparseable input", p.HeartbeatInterval)
	}
}

// Operators copying mihomo's literal outbound schema use raw ms ints
// (e.g. "10000"). time.ParseDuration rejects those — but we accept them.
func TestConvertTuic_HeartbeatInterval_RawMs(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["heartbeat_interval"] = "10000"
	p := c.convertTuic(spec)
	if p.HeartbeatInterval != 10000 {
		t.Errorf("HeartbeatInterval = %d, want 10000 (raw-ms input)", p.HeartbeatInterval)
	}
}

func TestConvertTuic_HeartbeatInterval_NegativeRejected(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["heartbeat_interval"] = "-500"
	p := c.convertTuic(spec)
	if p.HeartbeatInterval != 0 {
		t.Errorf("HeartbeatInterval = %d, want 0 (negative rejected)", p.HeartbeatInterval)
	}
}

// Listener-side fields (max-idle-time, authentication-timeout, mux-option,
// client-auth-cert, ech-key) must NOT appear in the client outbound yaml —
// they're server-only knobs. Marshal-and-grep ensures we don't accidentally
// add them to ClashProxy and write them out.
func TestConvertTuic_DropsListenerOnlyFields(t *testing.T) {
	c := &ClashConverter{}
	spec := tuicBaseSpec()
	spec.Extensions["server_name"] = "tuic.example.com"
	spec.Extensions["congestion_controller"] = "bbr"
	spec.Extensions["zero_rtt_handshake"] = true
	p := c.convertTuic(spec)
	out, err := yaml.Marshal(p)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	got := string(out)
	for _, forbidden := range []string{
		"max-idle-time",
		"authentication-timeout",
		"client-auth-cert",
		"client-auth-type",
		"ech-key",
	} {
		if strings.Contains(got, forbidden) {
			t.Errorf("yaml must NOT contain server-only field %q; got:\n%s", forbidden, got)
		}
	}
}
