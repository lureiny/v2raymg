package converter_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
	"gopkg.in/yaml.v3"
)

func vmessURIWithPS(t *testing.T, ps string, port int) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"v": "2", "ps": ps, "add": "vm.example.com", "port": port,
		"id": "11111111-1111-4111-8111-111111111111", "aid": 0, "net": "tcp",
		"type": "none", "tls": "tls",
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func decodeBase64Lines(t *testing.T, encoded string) []string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(string(raw), "\n")
}

func extractURIDisplayName(t *testing.T, rawURI string) string {
	t.Helper()
	if strings.HasPrefix(strings.ToLower(rawURI), "vmess://") {
		decoded, err := base64.StdEncoding.DecodeString(rawURI[len("vmess://"):])
		if err != nil {
			t.Fatalf("vmess base64 decode %q: %v", rawURI, err)
		}
		var obj map[string]any
		if err := json.Unmarshal(decoded, &obj); err != nil {
			t.Fatalf("vmess json unmarshal: %v", err)
		}
		ps, _ := obj["ps"].(string)
		return ps
	}
	u, err := url.Parse(rawURI)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", rawURI, err)
	}
	return u.Fragment
}

func TestUnifiedNaming_AllFormatsParity(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol: contracts.ProtocolVLess,
			Host:     "edge.example.com",
			Port:     443,
			Password: "uuid-1",
			NodeName: "Tokyo",
			URI:      "vless://uuid-1@edge.example.com:443?security=tls&type=tcp#Tokyo",
			Extensions: map[string]any{
				"security": "tls",
			},
		},
		{
			Protocol:   contracts.ProtocolAnyTLS,
			Host:       "edge.example.com",
			Port:       8443,
			Password:   "pw-anytls",
			NodeName:   "SH-Home",
			URI:        "anytls://pw-anytls@edge.example.com:8443?sni=edge.example.com#SH-Home",
			Extensions: map[string]any{},
		},
		{
			Protocol: contracts.ProtocolVMess,
			Host:     "vm.example.com",
			Port:     9443,
			Password: "11111111-1111-4111-8111-111111111111",
			NodeName: "LA Line & 01",
			URI:      vmessURIWithPS(t, "LA Line & 01", 9443),
			Extensions: map[string]any{
				"security": "tls",
			},
		},
	}

	wantAll := []string{
		"🌿 VLESS_Tokyo",
		"🌿 ANYTLS_SH-Home",
		"🌿 VMESS_LA Line & 01",
	}

	// 1. Common (base64 URIs)
	commonOut, err := (&converter.CommonConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("CommonConverter.Convert: %v", err)
	}
	commonLines := decodeBase64Lines(t, commonOut)
	if len(commonLines) != len(wantAll) {
		t.Fatalf("common lines = %d, want %d", len(commonLines), len(wantAll))
	}
	for i, want := range wantAll {
		if got := extractURIDisplayName(t, commonLines[i]); got != want {
			t.Errorf("common[%d] name = %q, want %q", i, got, want)
		}
	}
	// Spaces in fragment must use %20 (RFC 3986), not '+'.
	if strings.Contains(commonLines[0], "+") {
		t.Errorf("unexpected '+' in VLESS URI fragment: %q", commonLines[0])
	}
	if !strings.HasSuffix(commonLines[1], "#%F0%9F%8C%BF%20ANYTLS_SH-Home") {
		t.Errorf("unexpected AnyTLS fragment: %q", commonLines[1])
	}

	// 2. Qv2ray (plain URIs)
	qvOut, err := (&converter.Qv2rayConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("Qv2rayConverter.Convert: %v", err)
	}
	qvLines := strings.Split(qvOut, "\n")
	for i, want := range wantAll {
		if got := extractURIDisplayName(t, qvLines[i]); got != want {
			t.Errorf("qv2ray[%d] name = %q, want %q", i, got, want)
		}
	}

	// 3. Clash (YAML)
	clashOut, err := (&converter.ClashConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("ClashConverter.Convert: %v", err)
	}
	var parsedClash struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashOut), &parsedClash); err != nil {
		t.Fatalf("yaml.Unmarshal clash: %v", err)
	}
	for i, want := range wantAll {
		if parsedClash.Proxies[i].Name != want {
			t.Errorf("clash[%d].Name = %q, want %q", i, parsedClash.Proxies[i].Name, want)
		}
	}

	// 4. Surge (VLESS filtered out; AnyTLS + VMess emitted with identical names)
	surgeOut, err := (&converter.SurgeConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("SurgeConverter.Convert: %v", err)
	}
	surgeLines := strings.Split(surgeOut, "\n")
	if len(surgeLines) != 2 {
		t.Fatalf("surge lines = %d, want 2:\n%s", len(surgeLines), surgeOut)
	}
	if !strings.HasPrefix(surgeLines[0], "🌿 ANYTLS_SH-Home=anytls,") {
		t.Errorf("surge[0] = %q", surgeLines[0])
	}
	if !strings.HasPrefix(surgeLines[1], "🌿 VMESS_LA Line & 01=vmess,") {
		t.Errorf("surge[1] = %q", surgeLines[1])
	}
}

func TestDeduplication_01SuffixAndSecondaryCollisionAcrossFormats(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol: contracts.ProtocolTrojan,
			Host:     "a.example.com",
			Port:     443,
			Password: "p1",
			NodeName: "HK",
			URI:      "trojan://p1@a.example.com:443#HK",
		},
		{
			Protocol: contracts.ProtocolTrojan,
			Host:     "b.example.com",
			Port:     443,
			Password: "p2",
			NodeName: "HK",
			URI:      "trojan://p2@b.example.com:443#HK",
		},
		{
			// Existing unique name colliding with HK's _01 candidate must stay HK_01.
			Protocol: contracts.ProtocolTrojan,
			Host:     "c.example.com",
			Port:     443,
			Password: "p3",
			NodeName: "HK_01",
			URI:      "trojan://p3@c.example.com:443#HK_01",
		},
		{
			Protocol: contracts.ProtocolTrojan,
			Host:     "d.example.com",
			Port:     443,
			Password: "p4",
			NodeName: "HK",
			URI:      "trojan://p4@d.example.com:443#HK",
		},
		{
			// Unique entry must stay untouched without _01.
			Protocol: contracts.ProtocolTrojan,
			Host:     "e.example.com",
			Port:     443,
			Password: "p5",
			NodeName: "JP",
			URI:      "trojan://p5@e.example.com:443#JP",
		},
	}

	wantNames := []string{
		"🌿 TROJAN_HK_02",
		"🌿 TROJAN_HK_03",
		"🌿 TROJAN_HK_01",
		"🌿 TROJAN_HK_04",
		"🌿 TROJAN_JP",
	}

	// Common
	commonOut, err := (&converter.CommonConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("CommonConverter.Convert: %v", err)
	}
	commonLines := decodeBase64Lines(t, commonOut)
	for i, want := range wantNames {
		if got := extractURIDisplayName(t, commonLines[i]); got != want {
			t.Errorf("common[%d] = %q, want %q", i, got, want)
		}
	}

	// Qv2ray
	qvOut, err := (&converter.Qv2rayConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("Qv2rayConverter.Convert: %v", err)
	}
	qvLines := strings.Split(qvOut, "\n")
	for i, want := range wantNames {
		if got := extractURIDisplayName(t, qvLines[i]); got != want {
			t.Errorf("qv2ray[%d] = %q, want %q", i, got, want)
		}
	}

	// Clash
	clashOut, err := (&converter.ClashConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("ClashConverter.Convert: %v", err)
	}
	var parsed struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashOut), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	for i, want := range wantNames {
		if parsed.Proxies[i].Name != want {
			t.Errorf("clash[%d] = %q, want %q", i, parsed.Proxies[i].Name, want)
		}
	}

	// Surge
	surgeOut, err := (&converter.SurgeConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("SurgeConverter.Convert: %v", err)
	}
	surgeLines := strings.Split(surgeOut, "\n")
	for i, want := range wantNames {
		if !strings.HasPrefix(surgeLines[i], want+"=trojan,") {
			t.Errorf("surge[%d] = %q, want prefix %q", i, surgeLines[i], want+"=trojan,")
		}
	}
}

func TestSurgeConverter_UnsupportedEntriesDoNotAffectDedupNumbering(t *testing.T) {
	c := &converter.SurgeConverter{}
	specs := []contracts.SubscriptionSpec{
		// First is VLESS (unsupported by Surge), second is Trojan with same name "Same" -> only 1 Surge entry emitted!
		{Protocol: contracts.ProtocolVLess, Host: "1.1.1.1", Port: 443, Password: "u1", NodeName: "Same"},
		{Protocol: contracts.ProtocolTrojan, Host: "2.2.2.2", Port: 443, Password: "p1", NodeName: "Unique"},
		// Two VMess specs: one gRPC (skipped by Surge), one TCP -> emitted TCP must NOT get _01.
		{Protocol: contracts.ProtocolVMess, Host: "3.3.3.3", Port: 443, Password: "u2", NodeName: "VM", Extensions: map[string]any{"transport": "grpc"}},
		{Protocol: contracts.ProtocolVMess, Host: "4.4.4.4", Port: 443, Password: "u3", NodeName: "VM", Extensions: map[string]any{"transport": "tcp"}},
	}
	out, err := c.Convert(specs)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "🌿 TROJAN_Unique=trojan,") {
		t.Errorf("line[0] = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "🌿 VMESS_VM=vmess,") {
		t.Errorf("line[1] = %q (must not get _01 since gRPC sibling was skipped)", lines[1])
	}
}

func TestExternalSubscription_PreservesNameAndDeduplicatesAcrossFormats(t *testing.T) {
	specs := []contracts.SubscriptionSpec{
		{
			Protocol:   contracts.ProtocolVLess,
			Host:       "local.example.com",
			Port:       443,
			Password:   "uuid-local",
			NodeName:   "SH-Home",
			URI:        "vless://uuid-local@local.example.com:443?security=tls#SH-Home",
			Extensions: map[string]any{"security": "tls"},
		},
		{
			Protocol:   contracts.ProtocolTrojan,
			Host:       "ext1.example.com",
			Port:       443,
			Password:   "p-ext1",
			NodeName:   "🇭🇰 香港 01",
			IsExternal: true,
			URI:        "trojan://p-ext1@ext1.example.com:443#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%2001",
		},
		{
			Protocol:   contracts.ProtocolTrojan,
			Host:       "ext2.example.com",
			Port:       443,
			Password:   "p-ext2",
			NodeName:   "🇭🇰 香港 01",
			IsExternal: true,
			URI:        "trojan://p-ext2@ext2.example.com:443#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%2001",
		},
		{
			Protocol:   contracts.ProtocolVMess,
			Host:       "ext3.example.com",
			Port:       8443,
			Password:   "11111111-1111-4111-8111-111111111111",
			NodeName:   "🇺🇸 美国节点",
			IsExternal: true,
			URI:        vmessURIWithPS(t, "🇺🇸 美国节点", 8443),
			Extensions: map[string]any{"security": "tls"},
		},
	}

	wantAll := []string{
		"🌿 VLESS_SH-Home",
		"🇭🇰 香港 01_01",
		"🇭🇰 香港 01_02",
		"🇺🇸 美国节点",
	}

	// Common
	commonOut, err := (&converter.CommonConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("CommonConverter.Convert: %v", err)
	}
	commonLines := decodeBase64Lines(t, commonOut)
	for i, want := range wantAll {
		if got := extractURIDisplayName(t, commonLines[i]); got != want {
			t.Errorf("common[%d] = %q, want %q", i, got, want)
		}
	}

	// Clash
	clashOut, err := (&converter.ClashConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("ClashConverter.Convert: %v", err)
	}
	var parsed struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashOut), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	for i, want := range wantAll {
		if parsed.Proxies[i].Name != want {
			t.Errorf("clash[%d].Name = %q, want %q", i, parsed.Proxies[i].Name, want)
		}
	}

	// Surge (VLESS skipped; external Trojan + VMess keep original names + dedup)
	surgeOut, err := (&converter.SurgeConverter{}).Convert(specs)
	if err != nil {
		t.Fatalf("SurgeConverter.Convert: %v", err)
	}
	surgeLines := strings.Split(surgeOut, "\n")
	if len(surgeLines) != 3 {
		t.Fatalf("surge lines = %d, want 3:\n%s", len(surgeLines), surgeOut)
	}
	if !strings.HasPrefix(surgeLines[0], "🇭🇰 香港 01_01=trojan,") {
		t.Errorf("surge[0] = %q", surgeLines[0])
	}
	if !strings.HasPrefix(surgeLines[1], "🇭🇰 香港 01_02=trojan,") {
		t.Errorf("surge[1] = %q", surgeLines[1])
	}
	if !strings.HasPrefix(surgeLines[2], "🇺🇸 美国节点=vmess,") {
		t.Errorf("surge[2] = %q", surgeLines[2])
	}
}

func TestConvertURIsWithExternalAndOptions_AllFormats(t *testing.T) {
	localURIs := []string{
		"trojan://p1@n1.example.com:443?sni=n1.example.com#SH-Home",
		"trojan://p2@n2.example.com:443?sni=n2.example.com#SH-Home",
		"anytls://p3@n3.example.com:8443?sni=n3.example.com#Edge1",
	}
	extURIs := []string{
		"trojan://px1@ext1.example.com:443#Airport%20HK",
		vmessURIWithPS(t, "Airport HK", 443),
	}

	// Common (e.g. v2rayN)
	commonOut, err := subscription.ConvertURIsWithExternalAndOptions("v2rayN", localURIs, extURIs, nil)
	if err != nil {
		t.Fatalf("common ConvertURIsWithExternalAndOptions: %v", err)
	}
	commonLines := decodeBase64Lines(t, commonOut)
	if len(commonLines) != 5 {
		t.Fatalf("commonLines len = %d, want 5", len(commonLines))
	}
	wantAll := []string{
		"🌿 TROJAN_SH-Home_01",
		"🌿 TROJAN_SH-Home_02",
		"🌿 ANYTLS_Edge1",
		"Airport HK_01",
		"Airport HK_02",
	}
	for i, want := range wantAll {
		if got := extractURIDisplayName(t, commonLines[i]); got != want {
			t.Errorf("common[%d] name = %q, want %q", i, got, want)
		}
	}

	// Qv2ray
	qvOut, err := subscription.ConvertURIsWithExternalAndOptions("qv2ray", localURIs, extURIs, nil)
	if err != nil {
		t.Fatalf("qv2ray ConvertURIsWithExternalAndOptions: %v", err)
	}
	qvLines := strings.Split(qvOut, "\n")
	if len(qvLines) != 5 {
		t.Fatalf("qvLines len = %d, want 5", len(qvLines))
	}
	for i, want := range wantAll {
		if got := extractURIDisplayName(t, qvLines[i]); got != want {
			t.Errorf("qv2ray[%d] name = %q, want %q", i, got, want)
		}
	}

	// Surge
	surgeOut, err := subscription.ConvertURIsWithExternalAndOptions("Surge/5", localURIs, extURIs, nil)
	if err != nil {
		t.Fatalf("surge ConvertURIsWithExternalAndOptions: %v", err)
	}
	for _, wantPrefix := range []string{
		"🌿 TROJAN_SH-Home_01=trojan,",
		"🌿 TROJAN_SH-Home_02=trojan,",
		"🌿 ANYTLS_Edge1=anytls,",
		"Airport HK_01=trojan,",
		"Airport HK_02=vmess,",
	} {
		if !strings.Contains(surgeOut, wantPrefix) {
			t.Errorf("surge output missing %q:\n%s", wantPrefix, surgeOut)
		}
	}

	// Clash
	clashOut, err := subscription.ConvertURIsWithExternalAndOptions("clash.meta", localURIs, extURIs, nil)
	if err != nil {
		t.Fatalf("clash ConvertURIsWithExternalAndOptions: %v", err)
	}
	var parsed struct {
		Proxies []struct {
			Name string `yaml:"name"`
		} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashOut), &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal clash: %v", err)
	}
	if len(parsed.Proxies) != len(wantAll) {
		t.Fatalf("clash proxies = %d, want %d", len(parsed.Proxies), len(wantAll))
	}
	for i, want := range wantAll {
		if parsed.Proxies[i].Name != want {
			t.Errorf("clash[%d].Name = %q, want %q", i, parsed.Proxies[i].Name, want)
		}
	}
}
