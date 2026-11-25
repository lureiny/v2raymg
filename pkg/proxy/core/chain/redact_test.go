package chain

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURI_And_SummarizeURI(t *testing.T) {
	vmessJSON, _ := json.Marshal(map[string]any{
		"ps": "HomeVM", "add": "home.ddns.example", "port": 8443,
		"id": "super-secret-vmess-uuid", "aid": 0, "net": "ws",
	})
	vmessB64 := base64.StdEncoding.EncodeToString(vmessJSON)
	socksB64 := base64.StdEncoding.EncodeToString([]byte("alice:super-socks-secret"))
	socksSlashB64 := base64.StdEncoding.EncodeToString([]byte("user:??"))
	ssSIP002 := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:super-ss-secret"))
	ssFull := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:full-ss-secret@192.168.1.5:8388"))

	cases := []struct {
		name       string
		uri        string
		secrets    []string
		wantProto  string
		wantTarget string
	}{
		{
			name:       "vless reality",
			uri:        "vless://secret-uuid@node.example.com:443?security=reality&pbk=secret-pbk&sid=secret-sid#VL",
			secrets:    []string{"secret-uuid", "secret-pbk", "secret-sid"},
			wantProto:  "vless",
			wantTarget: "node.example.com:443",
		},
		{
			name:       "vmess",
			uri:        "vmess://" + vmessB64,
			secrets:    []string{vmessB64, "super-secret-vmess-uuid"},
			wantProto:  "vmess",
			wantTarget: "home.ddns.example:8443",
		},
		{
			name:       "trojan",
			uri:        "trojan://tr-secret@tr.example.com:443?sni=tr.example.com#TR",
			secrets:    []string{"tr-secret"},
			wantProto:  "trojan",
			wantTarget: "tr.example.com:443",
		},
		{
			name:       "ss sip002 + shadow-tls",
			uri:        "ss://" + ssSIP002 + "@ss.example.com:443?plugin=" + url.QueryEscape("shadow-tls;host=bing.com;password=stls-secret;version=3") + "#SS",
			secrets:    []string{ssSIP002, "super-ss-secret", "stls-secret"},
			wantProto:  "shadowsocks",
			wantTarget: "ss.example.com:443",
		},
		{
			name:       "ss full base64",
			uri:        "ss://" + ssFull + "#OpenWrt",
			secrets:    []string{ssFull, "full-ss-secret"},
			wantProto:  "shadowsocks",
			wantTarget: "192.168.1.5:8388",
		},
		{
			name:       "hy2 user:pass",
			uri:        "hy2://alice:hy2-secret@sh.ddns.example:28443?obfs=salamander&obfs-password=obfs-secret&mport=20000-40000#SH",
			secrets:    []string{"alice", "hy2-secret", "obfs-secret"},
			wantProto:  "hysteria2",
			wantTarget: "sh.ddns.example:28443",
		},
		{
			name:       "tuic",
			uri:        "tuic://00112233-4455-6677-8899-aabbccddeeff:tuic-secret@[2001:db8::1]:443#TUIC",
			secrets:    []string{"00112233-4455-6677-8899-aabbccddeeff", "tuic-secret"},
			wantProto:  "tuic",
			wantTarget: "[2001:db8::1]:443",
		},
		{
			name:       "anytls",
			uri:        "anytls://any-secret@any.example.com:443?sni=any.example.com#Any",
			secrets:    []string{"any-secret"},
			wantProto:  "anytls",
			wantTarget: "any.example.com:443",
		},
		{
			name:       "socks base64 ipv6",
			uri:        "socks://" + socksB64 + "@[2001:db8::2]:1080#S6",
			secrets:    []string{socksB64, "alice", "super-socks-secret"},
			wantProto:  "socks5",
			wantTarget: "[2001:db8::2]:1080",
		},
		{
			name:       "socks base64 with slash",
			uri:        "socks://" + socksSlashB64 + "@[2001:db8::3]:1080#Slash",
			secrets:    []string{socksSlashB64, "user", "??"},
			wantProto:  "socks5",
			wantTarget: "[2001:db8::3]:1080",
		},
		{
			name:       "https basic auth",
			uri:        "https://httpuser:httpsecret@proxy.example.com:8443?sni=proxy.example.com#H",
			secrets:    []string{"httpuser", "httpsecret"},
			wantProto:  "http",
			wantTarget: "proxy.example.com:8443",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			redacted := RedactURI(tc.uri)
			for _, sec := range tc.secrets {
				if strings.Contains(redacted, sec) {
					t.Errorf("RedactURI leaked secret %q in %q", sec, redacted)
				}
			}
			if !strings.Contains(redacted, tc.wantTarget) {
				t.Errorf("RedactURI(%q) = %q, expected target %q", tc.uri, redacted, tc.wantTarget)
			}

			proto, target := SummarizeURI(tc.uri)
			if proto != tc.wantProto || target != tc.wantTarget {
				t.Errorf("SummarizeURI = (%q, %q), want (%q, %q)", proto, target, tc.wantProto, tc.wantTarget)
			}
			for _, sec := range tc.secrets {
				if strings.Contains(proto, sec) || strings.Contains(target, sec) {
					t.Errorf("SummarizeURI leaked secret %q: (%q, %q)", sec, proto, target)
				}
			}
		})
	}
}

func TestRedactURI_MalformedNeverLeaks(t *testing.T) {
	malformed := []struct {
		uri    string
		secret string
	}{
		{"vmess://c2VjcmV0X2pzb24=", "c2VjcmV0X2pzb24"},
		{"ss://c2VjcmV0X3NzX2Jsb2I=", "c2VjcmV0X3NzX2Jsb2I"},
		{"socks://c2VjcmV0X3NvY2tz", "c2VjcmV0X3NvY2tz"},
		{"vless://secret-uuid@host:badport?pbk=secret-pbk", "secret-uuid"},
		{"raw-secret-without-scheme", "raw-secret-without-scheme"},
	}
	for _, tc := range malformed {
		got := RedactURI(tc.uri)
		if strings.Contains(got, tc.secret) {
			t.Errorf("RedactURI(%q) leaked %q: %q", tc.uri, tc.secret, got)
		}
		_, target := SummarizeURI(tc.uri)
		if strings.Contains(target, tc.secret) {
			t.Errorf("SummarizeURI(%q) leaked %q in target %q", tc.uri, tc.secret, target)
		}
	}
}
