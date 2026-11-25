package chain

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestParseURI_AllProtocols(t *testing.T) {
	t.Run("vless reality xhttp", func(t *testing.T) {
		raw := "vless://uuid-1@home.ddns.example:443?security=reality&type=xhttp&path=%2Fxh&host=cdn.example.com&mode=auto&pbk=pk1&sid=ab12&spx=%2Fsp&fp=chrome&flow=xtls-rprx-vision&encryption=none&sni=www.microsoft.com&alpn=h2,http/1.1#SH-VLESS"
		spec, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolVLess || spec.Scheme != "vless" {
			t.Errorf("protocol/scheme = %q/%q", spec.Protocol, spec.Scheme)
		}
		if spec.Host != "home.ddns.example" || spec.Port != 443 || spec.Remark != "SH-VLESS" {
			t.Errorf("target/remark mismatch: %+v", spec)
		}
		if spec.UUID != "uuid-1" || spec.Flow != "xtls-rprx-vision" || spec.Encryption != "none" {
			t.Errorf("auth/flow mismatch: %+v", spec)
		}
		if spec.Transport != "xhttp" || spec.XHTTPPath != "/xh" || spec.XHTTPHost != "cdn.example.com" || spec.XHTTPMode != "auto" {
			t.Errorf("xhttp mismatch: %+v", spec)
		}
		if spec.Security != "reality" || spec.RealityPublicKey != "pk1" || spec.RealityShortID != "ab12" || spec.RealitySpiderX != "/sp" {
			t.Errorf("reality mismatch: %+v", spec)
		}
		if !reflect.DeepEqual(spec.ALPN, []string{"h2", "http/1.1"}) {
			t.Errorf("ALPN = %v", spec.ALPN)
		}
	})

	t.Run("vmess allowInsecure int and scy", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"ps": "HomeVMess", "add": "home.ddns.example", "port": 8443,
			"id": "vm-uuid", "aid": 0, "scy": "aes-128-gcm",
			"net": "ws", "path": "/ws", "host": "ws.example.com",
			"tls": "tls", "sni": "sni.example.com", "allowInsecure": 1,
		})
		spec, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString(body))
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolVMess || spec.Method != "aes-128-gcm" || !spec.SkipCertVerify || !spec.TLS {
			t.Errorf("vmess mismatch: %+v", spec)
		}
	})

	t.Run("trojan httpupgrade", func(t *testing.T) {
		spec, err := ParseURI("trojan://tr-pass@10.0.0.2:443?type=httpupgrade&path=%2Fup&host=up.example.com&sni=up.example.com#TR")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolTrojan || spec.Transport != "httpupgrade" || spec.HTTPUpgradePath != "/up" || spec.HTTPUpgradeHost != "up.example.com" {
			t.Errorf("trojan mismatch: %+v", spec)
		}
		if spec.Security != "tls" || !spec.TLS {
			t.Errorf("expected security=tls, got %q (TLS=%v)", spec.Security, spec.TLS)
		}
	})

	t.Run("shadowsocks openwrt full base64 + shadow-tls", func(t *testing.T) {
		payload := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:ss-pass@192.168.10.1:8388"))
		plugin := url.QueryEscape("shadow-tls;host=www.bing.com;passwd=stls-pw;version=3")
		spec, err := ParseURI("ss://" + payload + "?plugin=" + plugin + "#OpenWrt")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolShadowsocks || spec.Method != "aes-256-gcm" || spec.Password != "ss-pass" {
			t.Errorf("ss mismatch: %+v", spec)
		}
		if spec.Plugin != "shadow-tls" || spec.PluginOpts["passwd"] != "stls-pw" || spec.PluginOpts["version"] != "3" {
			t.Errorf("plugin mismatch: %+v", spec)
		}
	})

	t.Run("hysteria2 user:pass mport pinSHA256", func(t *testing.T) {
		raw := "hy2://alice:secret@sh-home.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-40000&pinSHA256=DEAD:BEEF&obfs=salamander&obfs-password=opw#SH-Home"
		spec, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Scheme != "hy2" || spec.Protocol != contracts.ProtocolHysteria2 {
			t.Errorf("scheme/proto = %q/%q", spec.Scheme, spec.Protocol)
		}
		if spec.Password != "alice:secret" || spec.Ports != "20000-40000" || spec.PinSHA256 != "DEAD:BEEF" {
			t.Errorf("hy2 fields mismatch: %+v", spec)
		}
	})

	t.Run("tuic v5", func(t *testing.T) {
		raw := "tuic://00112233-4455-6677-8899-aabbccddeeff:tpw@100.64.0.10:443?sni=t.example.com&congestion_control=bbr&udp_relay_mode=quic&disable_sni=1&allow_insecure=1#TUIC"
		spec, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolTUIC || spec.UUID != "00112233-4455-6677-8899-aabbccddeeff" || spec.Password != "tpw" {
			t.Errorf("tuic auth mismatch: %+v", spec)
		}
		if spec.CongestionControl != "bbr" || spec.UDPRelayMode != "quic" || !spec.DisableSNI || !spec.SkipCertVerify {
			t.Errorf("tuic opts mismatch: %+v", spec)
		}
	})

	t.Run("anytls", func(t *testing.T) {
		spec, err := ParseURI("anytls://apw@[2001:db8::1]:443?sni=a.example.com&insecure=1&hpkp=FINGERPRINT#AnyTLS")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Protocol != contracts.ProtocolAnyTLS || spec.Host != "2001:db8::1" || spec.Password != "apw" || spec.Fingerprint != "FINGERPRINT" || !spec.SkipCertVerify {
			t.Errorf("anytls mismatch: %+v", spec)
		}
	})

	t.Run("socks5 v2rayn base64 ipv6", func(t *testing.T) {
		creds := base64.StdEncoding.EncodeToString([]byte("u1:p1"))
		spec, err := ParseURI("socks://" + creds + "@[2001:db8::9]:1080#S5V6")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Scheme != "socks" || spec.Protocol != contracts.ProtocolSOCKS5 || spec.Host != "2001:db8::9" || spec.Port != 1080 || spec.Username != "u1" || spec.Password != "p1" {
			t.Errorf("socks5 mismatch: %+v", spec)
		}
	})

	t.Run("https proxy", func(t *testing.T) {
		spec, err := ParseURI("https://u:p@proxy.home.example:8443?sni=proxy.home.example&insecure=1#HTTPS")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		if spec.Scheme != "https" || spec.Protocol != contracts.ProtocolHTTP || !spec.TLS || spec.Security != "tls" || spec.SNI != "proxy.home.example" || !spec.SkipCertVerify {
			t.Errorf("https mismatch: %+v", spec)
		}
	})
}

func TestParseURI_Errors(t *testing.T) {
	for _, raw := range []string{
		"",
		"   ",
		"not-a-uri",
		"unknown://user:pass@host:443",
		"vless://uuid@host:0",
	} {
		if _, err := ParseURI(raw); err == nil {
			t.Errorf("ParseURI(%q) expected error", raw)
		}
	}
}
