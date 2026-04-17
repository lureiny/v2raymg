package converter

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
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
