package chain

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func decodeXrayJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v\n%s", err, raw)
	}
	if m["domainStrategy"] != "AsIs" {
		t.Errorf("domainStrategy = %v, want AsIs", m["domainStrategy"])
	}
	return m
}

func TestRenderXrayOutbound_AllSupportedProtocols(t *testing.T) {
	t.Run("vless xhttp reality + encryption + spx", func(t *testing.T) {
		raw := "vless://uuid-1@home.ddns.example:443?security=reality&type=xhttp&path=%2Fxh&host=cdn.example.com&mode=auto&pbk=pk1&sid=ab12&spx=%2Fsp&fp=chrome&encryption=mlkem768x25519plus.native&sni=www.microsoft.com"
		spec, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b, err := RenderXrayOutbound("__chain_out__:t1", spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound: %v", err)
		}
		m := decodeXrayJSON(t, b)
		if m["tag"] != "__chain_out__:t1" || m["protocol"] != "vless" {
			t.Errorf("tag/protocol = %v/%v", m["tag"], m["protocol"])
		}
		settings := m["settings"].(map[string]any)
		vnext := settings["vnext"].([]any)[0].(map[string]any)
		if vnext["address"] != "home.ddns.example" {
			t.Errorf("address = %v, want preserved DDNS domain", vnext["address"])
		}
		user := vnext["users"].([]any)[0].(map[string]any)
		if user["encryption"] != "mlkem768x25519plus.native" || user["id"] != "uuid-1" {
			t.Errorf("user mismatch: %+v", user)
		}
		ss := m["streamSettings"].(map[string]any)
		if ss["network"] != "xhttp" || ss["security"] != "reality" {
			t.Errorf("stream mismatch: %+v", ss)
		}
		sh := ss["splithttpSettings"].(map[string]any)
		if sh["path"] != "/xh" || sh["host"] != "cdn.example.com" || sh["mode"] != "auto" {
			t.Errorf("splithttpSettings mismatch: %+v", sh)
		}
		rs := ss["realitySettings"].(map[string]any)
		if rs["publicKey"] != "pk1" || rs["shortId"] != "ab12" || rs["spiderX"] != "/sp" {
			t.Errorf("realitySettings mismatch: %+v", rs)
		}
	})

	t.Run("vless httpupgrade tls", func(t *testing.T) {
		raw := "vless://uuid-2@100.64.0.5:443?security=tls&type=httpupgrade&path=%2Fup&host=up.example.com&sni=up.example.com&flow=xtls-rprx-vision"
		spec, err := ParseURI(raw)
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b, err := RenderXrayOutbound("hu-out", spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound: %v", err)
		}
		m := decodeXrayJSON(t, b)
		ss := m["streamSettings"].(map[string]any)
		if ss["network"] != "httpupgrade" {
			t.Errorf("network = %v, want httpupgrade", ss["network"])
		}
		hu := ss["httpupgradeSettings"].(map[string]any)
		if hu["path"] != "/up" || hu["host"] != "up.example.com" {
			t.Errorf("httpupgradeSettings mismatch: %+v", hu)
		}
	})

	t.Run("vmess ws tls allowInsecure scy", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"ps": "VM", "add": "vm.ddns.example", "port": 8443,
			"id": "vm-id", "aid": 0, "scy": "chacha20-poly1305",
			"net": "ws", "path": "/vm", "host": "ws.example.com",
			"tls": "tls", "sni": "vm.ddns.example", "allowInsecure": true,
		})
		spec, err := ParseURI("vmess://" + base64.StdEncoding.EncodeToString(body))
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b, err := RenderXrayOutbound("vm-out", spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound: %v", err)
		}
		m := decodeXrayJSON(t, b)
		if m["protocol"] != "vmess" {
			t.Errorf("protocol = %v", m["protocol"])
		}
		user := m["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
		if user["security"] != "chacha20-poly1305" {
			t.Errorf("vmess cipher = %v, want chacha20-poly1305", user["security"])
		}
		tlsSettings := m["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)
		if tlsSettings["allowInsecure"] != true {
			t.Errorf("allowInsecure = %v, want true", tlsSettings["allowInsecure"])
		}
	})

	t.Run("trojan h2", func(t *testing.T) {
		spec, err := ParseURI("trojan://tpw@tr.example.com:443?type=h2&path=%2Fh2&host=h2.example.com&sni=tr.example.com")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b, err := RenderXrayOutbound("tr-out", spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound: %v", err)
		}
		m := decodeXrayJSON(t, b)
		if m["protocol"] != "trojan" {
			t.Errorf("protocol = %v", m["protocol"])
		}
		ss := m["streamSettings"].(map[string]any)
		if ss["network"] != "http" || ss["security"] != "tls" {
			t.Errorf("stream mismatch: %+v", ss)
		}
	})

	t.Run("shadowsocks pure 2022", func(t *testing.T) {
		spec, err := ParseURI("ss://2022-blake3-aes-256-gcm:secret@192.168.1.10:8388")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b, err := RenderXrayOutbound("ss-out", spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound: %v", err)
		}
		m := decodeXrayJSON(t, b)
		if m["protocol"] != "shadowsocks" {
			t.Errorf("protocol = %v", m["protocol"])
		}
	})

	t.Run("socks and https", func(t *testing.T) {
		s5Spec, err := ParseURI("socks5://alice:pwd@10.0.0.5:1080")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		b5, err := RenderXrayOutbound("s5-out", s5Spec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound socks: %v", err)
		}
		m5 := decodeXrayJSON(t, b5)
		if m5["protocol"] != "socks" {
			t.Errorf("protocol = %v, want socks", m5["protocol"])
		}

		httpsSpec, err := ParseURI("https://alice:pwd@proxy.example.com:443?sni=proxy.example.com&insecure=1")
		if err != nil {
			t.Fatalf("ParseURI: %v", err)
		}
		bh, err := RenderXrayOutbound("https-out", httpsSpec)
		if err != nil {
			t.Fatalf("RenderXrayOutbound https: %v", err)
		}
		mh := decodeXrayJSON(t, bh)
		if mh["protocol"] != "http" {
			t.Errorf("protocol = %v, want http", mh["protocol"])
		}
		ss := mh["streamSettings"].(map[string]any)
		if ss["security"] != "tls" {
			t.Errorf("https streamSettings security = %v, want tls", ss["security"])
		}
	})
}

func TestRenderXrayOutbound_UnsupportedSuggestsMihomo(t *testing.T) {
	ui := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	plugin := url.QueryEscape("shadow-tls;host=bing.com;password=stls;version=3")
	cases := []struct {
		name string
		uri  string
	}{
		{"hysteria2", "hy2://user:pass@home.example.com:443?sni=home.example.com"},
		{"tuic", "tuic://00112233-4455-6677-8899-aabbccddeeff:pwd@home.example.com:443"},
		{"anytls", "anytls://pwd@home.example.com:443?sni=home.example.com"},
		{"ss with plugin", "ss://" + ui + "@home.example.com:443?plugin=" + plugin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := ParseURI(tc.uri)
			if err != nil {
				t.Fatalf("ParseURI: %v", err)
			}
			_, err = RenderXrayOutbound("out", spec)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), "mihomo") {
				t.Errorf("error should suggest mihomo, got: %v", err)
			}
		})
	}
}
