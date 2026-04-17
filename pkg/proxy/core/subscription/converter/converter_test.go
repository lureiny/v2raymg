package converter_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// --- CommonConverter tests ---

func TestCommonConverter_Empty(t *testing.T) {
	c := &converter.CommonConverter{}
	result, err := c.Convert(nil)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(result)
	if string(decoded) != "" {
		t.Errorf("expected empty base64, got %q", result)
	}
}

func TestCommonConverter_MultipleURIs(t *testing.T) {
	c := &converter.CommonConverter{}
	specs := []contracts.SubscriptionSpec{
		{URI: "vmess://abc"},
		{URI: "vless://def"},
		{URI: ""}, // empty, should be skipped
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	expected := "vmess://abc\nvless://def"
	if string(decoded) != expected {
		t.Errorf("decoded: got %q, want %q", string(decoded), expected)
	}
}

func TestCommonConverter_Format(t *testing.T) {
	c := &converter.CommonConverter{}
	if c.Format() != subscription.FormatCommon {
		t.Errorf("Format: got %q, want %q", c.Format(), subscription.FormatCommon)
	}
}

// --- Qv2rayConverter tests ---

func TestQv2rayConverter_MultipleURIs(t *testing.T) {
	c := &converter.Qv2rayConverter{}
	specs := []contracts.SubscriptionSpec{
		{URI: "vmess://abc"},
		{URI: "trojan://xyz"},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	expected := "vmess://abc\ntrojan://xyz"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestQv2rayConverter_Format(t *testing.T) {
	c := &converter.Qv2rayConverter{}
	if c.Format() != subscription.FormatQv2ray {
		t.Errorf("Format: got %q, want %q", c.Format(), subscription.FormatQv2ray)
	}
}

// --- SurgeConverter tests ---

func TestSurgeConverter_VMess(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "10.0.0.1",
			Port:       443,
			Password:   "test-uuid-placeholder",
			InboundTag: "in1",
			Extensions: map[string]any{
				"alter_id": 0,
				"security": "tls",
			},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(result, "vmess") {
		t.Error("expected vmess in output")
	}
	if !strings.Contains(result, "10.0.0.1") {
		t.Error("expected host in output")
	}
	if !strings.Contains(result, "tls=true") {
		t.Error("expected tls=true")
	}
	if !strings.Contains(result, "vmess-aead=true") {
		t.Error("expected vmess-aead=true for alter_id=0")
	}
}

func TestSurgeConverter_Trojan(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolTrojan,
			Host:       "10.0.0.2",
			Port:       443,
			Password:   "pass-placeholder",
			InboundTag: "trojan1",
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(result, "trojan") {
		t.Error("expected trojan in output")
	}
	if !strings.Contains(result, "password=pass-placeholder") {
		t.Error("expected password in output")
	}
}

func TestSurgeConverter_Shadowsocks(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolShadowsocks,
			Host:       "10.0.0.3",
			Port:       8388,
			Password:   "sspass-placeholder",
			InboundTag: "ss1",
			Extensions: map[string]any{"method": "chacha20-ietf-poly1305"},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(result, "ss") {
		t.Error("expected ss in output")
	}
	if !strings.Contains(result, "chacha20-ietf-poly1305") {
		t.Error("expected method in output")
	}
}

func TestSurgeConverter_VLessSkipped(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "10.0.0.4",
			Port:       443,
			Password:   "uuid-placeholder",
			InboundTag: "vless1",
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for VLESS, got %q", result)
	}
}

func TestSurgeConverter_Format(t *testing.T) {
	c := &converter.SurgeConverter{}
	if c.Format() != subscription.FormatSurge {
		t.Errorf("Format: got %q, want %q", c.Format(), subscription.FormatSurge)
	}
}

func TestSurgeConverter_VMess_GRPCSkipped(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "10.0.0.1",
			Port:       443,
			Password:   "uuid",
			InboundTag: "vmess1",
			Extensions: map[string]any{"transport": "grpc"},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for VMess+grpc (unsupported in Surge), got %q", result)
	}
}

func TestSurgeConverter_Trojan_GRPCSkipped(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolTrojan,
			Host:       "10.0.0.2",
			Port:       443,
			Password:   "pass",
			InboundTag: "trojan1",
			Extensions: map[string]any{"transport": "grpc"},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty for Trojan+grpc (unsupported in Surge), got %q", result)
	}
}

func TestSurgeConverter_Hysteria2_SkipCertVerify(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolHysteria2,
			Host:       "10.0.0.3",
			Port:       443,
			Password:   "token",
			InboundTag: "hy2",
			URI:        "hysteria2://token@10.0.0.3:443/?insecure=1#node",
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(result, "skip-cert-verify=true") {
		t.Errorf("expected skip-cert-verify=true for insecure=1, got %q", result)
	}
}

// The ext-driven path must work without URI fallback. Guards against a return
// to the old strings.Contains(spec.URI, "insecure=1") shortcut that silently
// dropped all other Hy2 fields when the spec was built without raw URI.
func TestSurgeConverter_Hysteria2_FromExtensions(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolHysteria2,
			Host:       "10.0.0.3",
			Port:       443,
			Password:   "token",
			InboundTag: "hy2",
			Extensions: map[string]any{
				"server_name":      "example.com",
				"skip_cert_verify": true,
				"obfs":             "salamander",
				"obfs_password":    "shh",
			},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, want := range []string{
		"sni=example.com",
		"skip-cert-verify=true",
		"obfs=salamander",
		"obfs-password=shh",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in output, got %q", want, result)
		}
	}
}

// --- ClashConverter tests ---

func TestClashConverter_VLessIncluded(t *testing.T) {
	c := &converter.ClashConverter{}
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "10.0.0.1",
			Port:       443,
			Password:   "uuid-placeholder",
			InboundTag: "vless1",
			Extensions: map[string]any{
				"security":  "tls",
				"transport": "ws",
			},
		},
	}
	result, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	// Mihomo supports VLESS — node should appear in output
	if !strings.Contains(result, "vless") {
		t.Error("VLESS should be included in Mihomo output")
	}
}

func TestClashConverter_Format(t *testing.T) {
	c := &converter.ClashConverter{}
	if c.Format() != subscription.FormatClash {
		t.Errorf("Format: got %q, want %q", c.Format(), subscription.FormatClash)
	}
}

// --- Registry tests ---

func TestRegistry_Get_Registered(t *testing.T) {
	// CommonConverter is registered via init()
	c, ok := converter.Get(subscription.FormatCommon)
	if !ok || c == nil {
		t.Error("expected CommonConverter registered")
	}
}

func TestRegistry_Get_Unknown(t *testing.T) {
	_, ok := converter.Get("nonexistent-format")
	if ok {
		t.Error("expected false for unknown format")
	}
}

func TestRegistry_GetOrDefault_Known(t *testing.T) {
	c := converter.GetOrDefault(subscription.FormatSurge)
	if c == nil {
		t.Error("expected SurgeConverter")
	}
	if c.Format() != subscription.FormatSurge {
		t.Errorf("Format: got %q, want %q", c.Format(), subscription.FormatSurge)
	}
}

func TestRegistry_GetOrDefault_Unknown(t *testing.T) {
	c := converter.GetOrDefault("unknown-format")
	if c == nil {
		t.Fatal("expected fallback to common converter")
	}
	if c.Format() != subscription.FormatCommon {
		t.Errorf("expected fallback to FormatCommon, got %q", c.Format())
	}
}

// --- ConvertURIs end-to-end tests (SS + Snell) ---

func TestConvertURIs_SS_StreamAEAD_SurgeFormat(t *testing.T) {
	// SS URI: chacha20-ietf-poly1305:mypassword@example.com:8388#Node1
	ssURI := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk@example.com:8388#Node1"
	result, err := subscription.ConvertURIs("surge/4.0", []string{ssURI})
	if err != nil {
		t.Fatalf("ConvertURIs: %v", err)
	}
	if !strings.Contains(result, "example.com") {
		t.Errorf("expected host in Surge output, got %q", result)
	}
	if !strings.Contains(result, "8388") {
		t.Errorf("expected port in Surge output, got %q", result)
	}
	if !strings.Contains(result, "encrypt-method=chacha20-ietf-poly1305") {
		t.Errorf("expected method in Surge output, got %q", result)
	}
	if !strings.Contains(result, "password=mypassword") {
		t.Errorf("expected password in Surge output, got %q", result)
	}
	if !strings.Contains(result, "Node1") {
		t.Errorf("expected node name in Surge output, got %q", result)
	}
}

func TestConvertURIs_SS_AEAD2022_SurgeFormat(t *testing.T) {
	ssURI := "ss://2022-blake3-aes-256-gcm:secret@server.io:443#MyNode"
	result, err := subscription.ConvertURIs("surge", []string{ssURI})
	if err != nil {
		t.Fatalf("ConvertURIs: %v", err)
	}
	if !strings.Contains(result, "server.io") {
		t.Errorf("expected host, got %q", result)
	}
	if !strings.Contains(result, "443") {
		t.Errorf("expected port, got %q", result)
	}
	if !strings.Contains(result, "encrypt-method=2022-blake3-aes-256-gcm") {
		t.Errorf("expected AEAD-2022 method, got %q", result)
	}
	if !strings.Contains(result, "password=secret") {
		t.Errorf("expected password, got %q", result)
	}
	if !strings.Contains(result, "MyNode") {
		t.Errorf("expected node name, got %q", result)
	}
}

func TestConvertURIs_Snell_SurgeFormat(t *testing.T) {
	snellURI := "snell://my-psk@example.com:48162?version=5#SnellNode"
	result, err := subscription.ConvertURIs("surge/5.0", []string{snellURI})
	if err != nil {
		t.Fatalf("ConvertURIs: %v", err)
	}
	if !strings.Contains(result, "example.com") {
		t.Errorf("expected host, got %q", result)
	}
	if !strings.Contains(result, "48162") {
		t.Errorf("expected port, got %q", result)
	}
	if !strings.Contains(result, "psk=my-psk") {
		t.Errorf("expected psk, got %q", result)
	}
	if !strings.Contains(result, "SnellNode") {
		t.Errorf("expected node name, got %q", result)
	}
	if !strings.Contains(result, "version=5") {
		t.Errorf("expected version=5, got %q", result)
	}
}

func TestConvertURIs_SSnell_Mixed_CommonFormat(t *testing.T) {
	ssURI := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk@ss-host:8388#SSNode"
	snellURI := "snell://psk@snell-host:48162?version=5#SnellNode"

	result, err := subscription.ConvertURIs("", []string{snellURI, ssURI})
	if err != nil {
		t.Fatalf("ConvertURIs: %v", err)
	}
	// Common format is base64 encoded; decode and check
	decoded, err := base64.StdEncoding.DecodeString(result)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !strings.Contains(string(decoded), snellURI) {
		t.Errorf("expected snell URI preserved, got %q", string(decoded))
	}
	if !strings.Contains(string(decoded), ssURI) {
		t.Errorf("expected SS URI preserved, got %q", string(decoded))
	}
}

func TestConvertURIs_SSnell_Mixed_SurgeFormat(t *testing.T) {
	ssURI := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk@ss-host:8388#SSNode"
	snellURI := "snell://psk@snell-host:48162?version=5#SnellNode"

	result, err := subscription.ConvertURIs("surge", []string{snellURI, ssURI})
	if err != nil {
		t.Fatalf("ConvertURIs: %v", err)
	}
	// Both nodes should appear with names
	if !strings.Contains(result, "SNELL_SnellNode") {
		t.Errorf("expected SNELL node name, got %q", result)
	}
	if !strings.Contains(result, "SS_SSNode") {
		t.Errorf("expected SS node name, got %q", result)
	}
	// SS fields
	if !strings.Contains(result, "ss-host") {
		t.Errorf("expected SS host, got %q", result)
	}
	if !strings.Contains(result, "8388") {
		t.Errorf("expected SS port, got %q", result)
	}
	// Snell fields
	if !strings.Contains(result, "snell-host") {
		t.Errorf("expected snell host, got %q", result)
	}
	if !strings.Contains(result, "48162") {
		t.Errorf("expected snell port, got %q", result)
	}
}
