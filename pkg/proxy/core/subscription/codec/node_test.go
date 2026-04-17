package codec

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestDecode_DispatchVLess(t *testing.T) {
	n, err := Decode("vless://uuid@host:443?security=tls#N")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*VLessNode); !ok {
		t.Errorf("expected *VLessNode, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolVLess {
		t.Errorf("Protocol() = %q, want vless", n.Protocol())
	}
}

func TestDecode_DispatchVMess(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "uuid", "aid": "0", "net": "tcp",
	})
	uri := "vmess://" + base64.StdEncoding.EncodeToString(body)
	n, err := Decode(uri)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*VMessNode); !ok {
		t.Errorf("expected *VMessNode, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolVMess {
		t.Errorf("Protocol() = %q, want vmess", n.Protocol())
	}
}

func TestDecode_DispatchTrojan(t *testing.T) {
	n, err := Decode("trojan://pass@host:443#N")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*TrojanNode); !ok {
		t.Errorf("expected *TrojanNode, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolTrojan {
		t.Errorf("Protocol() = %q, want trojan", n.Protocol())
	}
}

func TestDecode_DispatchShadowsocks(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	n, err := Decode("ss://" + userinfo + "@host:443#N")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*ShadowsocksNode); !ok {
		t.Errorf("expected *ShadowsocksNode, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol() = %q, want shadowsocks", n.Protocol())
	}
}

func TestDecode_DispatchHysteria2(t *testing.T) {
	n, err := Decode("hysteria2://pwd@host:443#N")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*Hysteria2Node); !ok {
		t.Errorf("expected *Hysteria2Node, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolHysteria2 {
		t.Errorf("Protocol() = %q, want hysteria2", n.Protocol())
	}
}

func TestDecode_DispatchHy2Alias(t *testing.T) {
	n, err := Decode("hy2://pwd@host:443#N")
	if err != nil {
		t.Fatalf("Decode(hy2): %v", err)
	}
	if n.Protocol() != contracts.ProtocolHysteria2 {
		t.Errorf("Protocol() = %q, want hysteria2", n.Protocol())
	}
}

func TestDecode_DispatchSnell(t *testing.T) {
	n, err := Decode("snell://psk@host:443?version=5#N")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := n.(*SnellNode); !ok {
		t.Errorf("expected *SnellNode, got %T", n)
	}
	if n.Protocol() != contracts.ProtocolSnell {
		t.Errorf("Protocol() = %q, want snell", n.Protocol())
	}
}

func TestDecode_UnknownScheme(t *testing.T) {
	_, err := Decode("unknown://foo@host:443")
	if err == nil {
		t.Error("expected error for unknown scheme")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error message should mention 'unsupported', got %q", err.Error())
	}
}

func TestDecode_MissingScheme(t *testing.T) {
	_, err := Decode("not a uri at all")
	if err == nil {
		t.Error("expected error for missing scheme")
	}
}

func TestDecode_SchemeCaseInsensitive(t *testing.T) {
	// Scheme should be matched case-insensitively
	n, err := Decode("VLESS://uuid@host:443?security=tls#N")
	if err != nil {
		t.Fatalf("Decode uppercase scheme: %v", err)
	}
	if n.Protocol() != contracts.ProtocolVLess {
		t.Errorf("Protocol() = %q, want vless", n.Protocol())
	}
}

func TestDecode_PropagatesDecoderError(t *testing.T) {
	// Invalid base64 in vmess should bubble up
	_, err := Decode("vmess://!!!not-base64!!!")
	if err == nil {
		t.Error("expected decoder error to propagate")
	}
}

// Verify all concrete node types can round-trip through the generic Decode/Encode.
func TestDecode_EncodeRoundTrip_AllProtocols(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	vmessBody, _ := json.Marshal(map[string]any{
		"ps": "M", "add": "host.com", "port": "443",
		"id": "uuid", "aid": "0", "net": "ws", "path": "/ws", "host": "cdn.host.com", "tls": "tls",
	})
	vmessURI := "vmess://" + base64.StdEncoding.EncodeToString(vmessBody)
	cases := []string{
		"vless://uuid@host.com:443?security=reality&pbk=pk&sid=s1&fp=chrome&flow=xtls-rprx-vision&sni=host.com#V",
		vmessURI,
		"trojan://pass@host.com:443?sni=host.com#T",
		"ss://" + userinfo + "@host.com:443#S",
		"hysteria2://pwd@host.com:443?sni=host.com&insecure=1#H",
		"snell://psk@host.com:443?version=5#N",
	}
	for _, uri := range cases {
		n, err := Decode(uri)
		if err != nil {
			t.Errorf("Decode(%q): %v", uri, err)
			continue
		}
		encoded := n.Encode()
		reDecoded, err := Decode(encoded)
		if err != nil {
			t.Errorf("Decode(Encode) of %q failed: %v", uri, err)
			continue
		}
		if reDecoded.Protocol() != n.Protocol() {
			t.Errorf("protocol drift: %q -> %q", n.Protocol(), reDecoded.Protocol())
		}
	}
}
