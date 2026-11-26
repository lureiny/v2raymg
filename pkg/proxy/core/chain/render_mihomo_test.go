package chain

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRenderMihomoProxy_AllSupportedProtocols(t *testing.T) {
	t.Run("vless reality + tcp", func(t *testing.T) {
		spec, err := ParseURI("vless://uuid-v@home.ddns.example:443?security=reality&pbk=pk1&sid=ab12&flow=xtls-rprx-vision&sni=www.microsoft.com&encryption=none#VL")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		m, err := RenderMihomoProxy("__chain_out__:t1", spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		if m["name"] != "__chain_out__:t1" || m["type"] != "vless" || m["server"] != "home.ddns.example" || m["port"] != 443 || m["uuid"] != "uuid-v" {
			t.Errorf("unexpected vless dict: %+v", m)
		}
		if m["tls"] != true || m["client-fingerprint"] != "chrome" || m["flow"] != "xtls-rprx-vision" {
			t.Errorf("unexpected vless tls/fp/flow: %+v", m)
		}
		ro, _ := m["reality-opts"].(map[string]any)
		if ro["public-key"] != "pk1" || ro["short-id"] != "ab12" {
			t.Errorf("unexpected reality-opts: %+v", ro)
		}
	})

	t.Run("vmess ws tls + scy + allowInsecure", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"ps": "VM", "add": "vm.example.com", "port": 443,
			"id": "vm-id", "aid": 0, "scy": "aes-128-gcm",
			"net": "ws", "path": "/vm", "host": "ws.example.com",
			"tls": "tls", "sni": "vm.example.com", "allowInsecure": "1",
		})
		spec, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString(body))
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		m, err := RenderMihomoProxy("vm-out", spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		if m["type"] != "vmess" || m["cipher"] != "aes-128-gcm" || m["skip-cert-verify"] != true || m["network"] != "ws" {
			t.Errorf("unexpected vmess dict: %+v", m)
		}
	})

	t.Run("trojan grpc", func(t *testing.T) {
		spec, err := ParseURI("trojan://tpw@tr.example.com:443?type=grpc&serviceName=svc&sni=tr.example.com&insecure=1")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		m, err := RenderMihomoProxy("tr-out", spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		if m["type"] != "trojan" || m["sni"] != "tr.example.com" || m["skip-cert-verify"] != true || m["network"] != "grpc" {
			t.Errorf("unexpected trojan dict: %+v", m)
		}
	})

	t.Run("ss obfs-local and simple-obfs normalized to obfs", func(t *testing.T) {
		ui := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
		for _, plugin := range []string{"obfs-local", "simple-obfs"} {
			pVal := url.QueryEscape(plugin + ";obfs=http;obfs-host=bing.com")
			spec, err := ParseURI("ss://" + ui + "@1.2.3.4:8388?plugin=" + pVal)
			if err != nil {
				t.Fatalf("ParseURI: %v", err)
			}
			m, err := RenderMihomoProxy("ss-obfs", spec)
			if err != nil {
				t.Fatalf("RenderMihomoProxy: %v", err)
			}
			if m["plugin"] != "obfs" {
				t.Errorf("%s: plugin = %v, want obfs", plugin, m["plugin"])
			}
			opts, _ := m["plugin-opts"].(map[string]any)
			if opts["mode"] != "http" || opts["host"] != "bing.com" {
				t.Errorf("%s: plugin-opts = %+v", plugin, opts)
			}
		}
	})

	t.Run("ss v2ray-plugin and shadow-tls", func(t *testing.T) {
		ui := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pwd"))
		v2Val := url.QueryEscape("v2ray-plugin;mode=websocket;tls;host=cdn.example.com;path=/ws")
		specV2, err := ParseURI("ss://" + ui + "@1.2.3.4:443?plugin=" + v2Val)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		mV2, err := RenderMihomoProxy("ss-v2", specV2)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		v2Opts, _ := mV2["plugin-opts"].(map[string]any)
		if mV2["plugin"] != "v2ray-plugin" || v2Opts["tls"] != true || v2Opts["path"] != "/ws" {
			t.Errorf("unexpected v2ray-plugin: %+v", mV2)
		}

		stVal := url.QueryEscape("shadow-tls;host=www.microsoft.com;passwd=stls-pw;version=3")
		specST, err := ParseURI("ss://" + ui + "@1.2.3.4:443?plugin=" + stVal)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		mST, err := RenderMihomoProxy("ss-st", specST)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		stOpts, _ := mST["plugin-opts"].(map[string]any)
		if mST["plugin"] != "shadow-tls" || stOpts["password"] != "stls-pw" || stOpts["version"] != 3 || stOpts["host"] != "www.microsoft.com" {
			t.Errorf("unexpected shadow-tls: %+v", mST)
		}
	})

	t.Run("hysteria2 user:pass mport pinSHA256 strips sha256 prefix", func(t *testing.T) {
		hexPin := strings.Repeat("ab", 32)
		for _, rawPin := range []string{hexPin, "sha256:" + hexPin, "SHA256:" + hexPin} {
			spec, err := ParseURI("hy2://alice:secret@home.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-40000&pinSHA256=" + url.QueryEscape(rawPin) + "#H")
			if err != nil {
				t.Fatalf("ParseURI(%q): %v", rawPin, err)
			}
			m, err := RenderMihomoProxy("hy2-out", spec)
			if err != nil {
				t.Fatalf("RenderMihomoProxy(%q): %v", rawPin, err)
			}
			if m["type"] != "hysteria2" || m["password"] != "alice:secret" || m["ports"] != "20000-40000" || m["fingerprint"] != hexPin {
				t.Errorf("rawPin=%q: unexpected hy2 dict: %+v", rawPin, m)
			}
		}
	})

	t.Run("tuic", func(t *testing.T) {
		spec, err := ParseURI("tuic://00112233-4455-6677-8899-aabbccddeeff:pwd@100.64.0.1:443?congestion_control=bbr&udp_relay_mode=quic&disable_sni=1&allow_insecure=1")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		m, err := RenderMihomoProxy("tuic-out", spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		if m["type"] != "tuic" || m["congestion-controller"] != "bbr" || m["udp-relay-mode"] != "quic" || m["disable-sni"] != true || m["skip-cert-verify"] != true {
			t.Errorf("unexpected tuic dict: %+v", m)
		}
	})

	t.Run("anytls", func(t *testing.T) {
		spec, err := ParseURI("anytls://pwd@any.example.com:443?sni=any.example.com&hpkp=PIN&insecure=1")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		m, err := RenderMihomoProxy("any-out", spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy: %v", err)
		}
		if m["type"] != "anytls" || m["password"] != "pwd" || m["fingerprint"] != "PIN" || m["skip-cert-verify"] != true {
			t.Errorf("unexpected anytls dict: %+v", m)
		}
	})

	t.Run("socks5 and http(s)", func(t *testing.T) {
		s5Spec, err := ParseURI("socks5://u:p@192.168.1.1:1080")
		if err != nil {
			t.Fatalf("ParseURI socks5: %v", err)
		}
		m5, err := RenderMihomoProxy("s5", s5Spec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy socks5: %v", err)
		}
		if m5["type"] != "socks5" || m5["username"] != "u" || m5["password"] != "p" || m5["udp"] != true {
			t.Errorf("unexpected socks5 dict: %+v", m5)
		}

		httpsSpec, err := ParseURI("https://u:p@proxy.example.com:8443?sni=proxy.example.com&insecure=1")
		if err != nil {
			t.Fatalf("ParseURI https: %v", err)
		}
		mh, err := RenderMihomoProxy("https-out", httpsSpec)
		if err != nil {
			t.Fatalf("RenderMihomoProxy https: %v", err)
		}
		if mh["type"] != "http" || mh["tls"] != true || mh["sni"] != "proxy.example.com" || mh["skip-cert-verify"] != true {
			t.Errorf("unexpected https dict: %+v", mh)
		}
		if _, hasUDP := mh["udp"]; hasUDP {
			t.Errorf("http proxy must not set udp=true")
		}
	})
}

func TestRenderMihomoProxy_UnsupportedSuggestsXray(t *testing.T) {
	cases := []struct {
		name string
		uri  string
	}{
		{"vless httpupgrade", "vless://uuid@host.com:443?security=tls&type=httpupgrade&path=/up"},
		{"vless xhttp", "vless://uuid@host.com:443?security=tls&type=xhttp&path=/xh"},
		{"vless splithttp", "vless://uuid@host.com:443?security=tls&type=splithttp&path=/sh"},
		{"vless mlkem768 encryption", "vless://uuid@host.com:443?security=tls&encryption=mlkem768x25519plus.native"},
		{"vless xtls", "vless://uuid@host.com:443?security=xtls&flow=xtls-rprx-direct"},
		{"trojan httpupgrade", "trojan://pwd@host.com:443?type=httpupgrade&path=/up"},
		{"vmess httpupgrade", "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"add":"host.com","port":443,"id":"uuid","net":"httpupgrade"}`))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ParseURI(tc.uri)
			if err != nil {
				t.Fatalf("ParseURI: %v", err)
			}
			_, err = RenderMihomoProxy("out", spec)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "xray") {
				t.Errorf("error should suggest xray, got: %v", err)
			}
		})
	}

	// Uppercase Security="XTLS" must also be rejected case-insensitively.
	t.Run("vless uppercase XTLS", func(t *testing.T) {
		_, err := RenderMihomoProxy("out", &OutboundSpec{
			Protocol: "vless",
			Host:     "host.com",
			Port:     443,
			UUID:     "uuid",
			Security: "XTLS",
		})
		if err == nil {
			t.Fatal("expected error for uppercase XTLS, got nil")
		}
	})
}
