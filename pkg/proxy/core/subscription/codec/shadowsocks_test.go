package codec

import (
	"encoding/base64"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeShadowsocks_SIP002_Basic(t *testing.T) {
	// ss://base64url(chacha20-ietf-poly1305:mypassword)@example.com:8388#Node1
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:mypassword"))
	raw := "ss://" + userinfo + "@example.com:8388#Node1"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", n.Host)
	}
	if n.Port != 8388 {
		t.Errorf("Port = %d, want 8388", n.Port)
	}
	if n.Method != "chacha20-ietf-poly1305" {
		t.Errorf("Method = %q, want chacha20-ietf-poly1305", n.Method)
	}
	if n.Password != "mypassword" {
		t.Errorf("Password = %q, want mypassword", n.Password)
	}
	if n.NodeName != "Node1" {
		t.Errorf("NodeName = %q, want Node1", n.NodeName)
	}
}

func TestDecodeShadowsocks_AEAD2022(t *testing.T) {
	raw := "ss://2022-blake3-aes-256-gcm:password123@example.com:443#MyNode"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.Method != "2022-blake3-aes-256-gcm" {
		t.Errorf("Method = %q, want 2022-blake3-aes-256-gcm", n.Method)
	}
	if n.Password != "password123" {
		t.Errorf("Password = %q, want password123", n.Password)
	}
	if n.NodeName != "MyNode" {
		t.Errorf("NodeName = %q, want MyNode", n.NodeName)
	}
}

func TestDecodeShadowsocks_AEAD2022_PercentEncodedPwd(t *testing.T) {
	raw := "ss://2022-blake3-aes-256-gcm:p%2Bass%3Dword@example.com:8080"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.Password != "p+ass=word" {
		t.Errorf("Password = %q, want p+ass=word (decoded)", n.Password)
	}
}

func TestDecodeShadowsocks_ObfsPlugin(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	pluginVal := url.QueryEscape("obfs-local;obfs=tls;obfs-host=example.com")
	raw := "ss://" + userinfo + "@host.com:8388?plugin=" + pluginVal + "#Obfs"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.Plugin != "obfs-local" {
		t.Errorf("Plugin = %q, want obfs-local", n.Plugin)
	}
	if n.PluginOpts["obfs"] != "tls" {
		t.Errorf("PluginOpts[obfs] = %q, want tls", n.PluginOpts["obfs"])
	}
	if n.PluginOpts["obfs-host"] != "example.com" {
		t.Errorf("PluginOpts[obfs-host] = %q, want example.com", n.PluginOpts["obfs-host"])
	}
}

func TestDecodeShadowsocks_V2RayPluginWithFlagOpt(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	// v2ray-plugin uses "tls" as a flag (no value) in SIP003 format
	pluginVal := url.QueryEscape("v2ray-plugin;mode=websocket;tls;host=example.com;path=/ws")
	raw := "ss://" + userinfo + "@host.com:443?plugin=" + pluginVal + "#V2"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.Plugin != "v2ray-plugin" {
		t.Errorf("Plugin = %q, want v2ray-plugin", n.Plugin)
	}
	if _, has := n.PluginOpts["tls"]; !has {
		t.Error("flag opt 'tls' should be present with empty value")
	}
	if n.PluginOpts["tls"] != "" {
		t.Errorf("flag opt tls value = %q, want empty", n.PluginOpts["tls"])
	}
	if n.PluginOpts["mode"] != "websocket" {
		t.Errorf("PluginOpts[mode] = %q, want websocket", n.PluginOpts["mode"])
	}
	if n.PluginOpts["path"] != "/ws" {
		t.Errorf("PluginOpts[path] = %q, want /ws", n.PluginOpts["path"])
	}
}

func TestDecodeShadowsocks_NoFragment(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pwd"))
	raw := "ss://" + userinfo + "@host.com:443"
	n, err := DecodeShadowsocks(raw)
	if err != nil {
		t.Fatalf("DecodeShadowsocks: %v", err)
	}
	if n.NodeName != "" {
		t.Errorf("NodeName = %q, want empty", n.NodeName)
	}
}

func TestDecodeShadowsocks_MissingPort_Error(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	_, err := DecodeShadowsocks("ss://" + userinfo + "@host.com")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeShadowsocks_InvalidBase64Userinfo(t *testing.T) {
	// Userinfo contains no colon, no valid base64 method:password
	_, err := DecodeShadowsocks("ss://garbage_not_base64@host.com:443")
	if err == nil {
		t.Error("expected error for unparseable userinfo")
	}
}

func TestDecodeShadowsocks_WrongScheme_Error(t *testing.T) {
	_, err := DecodeShadowsocks("trojan://pass@host.com:443")
	if err == nil {
		t.Error("expected error for non-ss scheme")
	}
}

// --- Encode ---

func TestEncodeShadowsocks_SIP002_Base64Userinfo(t *testing.T) {
	n := &ShadowsocksNode{
		NodeName: "N", Host: "host.com", Port: 443,
		Method: "chacha20-ietf-poly1305", Password: "pwd",
	}
	uri := n.Encode()
	if !strings.HasPrefix(uri, "ss://") {
		t.Fatalf("missing ss:// prefix")
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	// SIP002: userinfo is base64url (RFC 4648 §5), no padding, no ':' directly
	if strings.Contains(u.User.Username(), ":") {
		t.Errorf("SIP002 userinfo should not contain ':' directly, got %q", u.User.Username())
	}
	if strings.Contains(u.User.Username(), "=") {
		t.Errorf("SIP002 userinfo must use RawURLEncoding (no padding), got %q", u.User.Username())
	}
	decoded, err := base64.RawURLEncoding.DecodeString(u.User.Username())
	if err != nil {
		t.Errorf("SIP002 userinfo should decode via RawURLEncoding, got %q: %v", u.User.Username(), err)
	}
	if !strings.Contains(string(decoded), ":") {
		t.Errorf("decoded userinfo should be 'method:password', got %q", decoded)
	}
}

func TestEncodeShadowsocks_AEAD2022_PlainUserinfo(t *testing.T) {
	n := &ShadowsocksNode{
		Host: "host.com", Port: 443,
		Method: "2022-blake3-aes-256-gcm", Password: "pwd",
	}
	uri := n.Encode()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	// AEAD-2022: userinfo is method:password
	if u.User.Username() != "2022-blake3-aes-256-gcm" {
		t.Errorf("AEAD-2022 username = %q, want method directly", u.User.Username())
	}
	pwd, ok := u.User.Password()
	if !ok || pwd != "pwd" {
		t.Errorf("AEAD-2022 password = %q (ok=%v), want pwd", pwd, ok)
	}
}

// --- RoundTrip ---

func TestShadowsocks_RoundTrip_SIP002(t *testing.T) {
	orig := &ShadowsocksNode{
		NodeName: "Node",
		Host:     "host.com",
		Port:     8388,
		Method:   "chacha20-ietf-poly1305",
		Password: "mypassword",
	}
	decoded, err := DecodeShadowsocks(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestShadowsocks_RoundTrip_AEAD2022(t *testing.T) {
	orig := &ShadowsocksNode{
		NodeName: "A2022",
		Host:     "host.com",
		Port:     443,
		Method:   "2022-blake3-aes-256-gcm",
		Password: "password123",
	}
	decoded, err := DecodeShadowsocks(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestShadowsocks_RoundTrip_ObfsPlugin(t *testing.T) {
	orig := &ShadowsocksNode{
		NodeName: "OBFS",
		Host:     "host.com",
		Port:     8388,
		Method:   "aes-128-gcm",
		Password: "pwd",
		Plugin:   "obfs-local",
		PluginOpts: map[string]string{
			"obfs":      "tls",
			"obfs-host": "example.com",
		},
	}
	decoded, err := DecodeShadowsocks(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestShadowsocks_RoundTrip_V2RayPluginWithFlag(t *testing.T) {
	orig := &ShadowsocksNode{
		NodeName: "V2",
		Host:     "host.com",
		Port:     443,
		Method:   "aes-128-gcm",
		Password: "pwd",
		Plugin:   "v2ray-plugin",
		PluginOpts: map[string]string{
			"mode": "websocket",
			"tls":  "", // flag-style opt
			"host": "example.com",
			"path": "/ws",
		},
	}
	decoded, err := DecodeShadowsocks(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestShadowsocksNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*ShadowsocksNode)(nil)
}

// --- Regression: fields that broke pre-review ---

// SIP002 userinfo MUST use RawURLEncoding per spec. StdEncoding produces '+', '/',
// '=' which either break URL parsers or get double-percent-encoded.
func TestShadowsocks_RoundTrip_PasswordWithBase64SpecialChars(t *testing.T) {
	// "this+pwd/has=specials" → base64 contains +,/,= under StdEncoding but not RawURLEncoding.
	orig := &ShadowsocksNode{
		NodeName: "Special",
		Host:     "host.com",
		Port:     8388,
		Method:   "chacha20-ietf-poly1305",
		Password: "this+pwd/has=specials",
	}
	uri := orig.Encode()
	if strings.ContainsAny(uri, "+/=") && !strings.Contains(uri, "%") {
		// URL encode of = is %3D, of / is %2F, of + is %2B — so raw +/= in userinfo means we used StdEncoding.
		u, _ := url.Parse(uri)
		if u != nil {
			if strings.ContainsAny(u.User.Username(), "+/=") {
				t.Errorf("SIP002 userinfo leaked StdEncoding chars: %q", u.User.Username())
			}
		}
	}
	decoded, err := DecodeShadowsocks(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Password != orig.Password {
		t.Errorf("Password corrupted: got %q, want %q", decoded.Password, orig.Password)
	}
	if decoded.Method != orig.Method {
		t.Errorf("Method corrupted: got %q, want %q", decoded.Method, orig.Method)
	}
}

func TestShadowsocks_RoundTrip_IPv6Host(t *testing.T) {
	orig := &ShadowsocksNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     8388,
		Method:   "aes-128-gcm",
		Password: "pwd",
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeShadowsocks(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host round-trip lost brackets: got %q, want 2001:db8::1", decoded.Host)
	}
	if decoded.Port != 8388 {
		t.Errorf("Port = %d, want 8388", decoded.Port)
	}
}

func TestDecodeShadowsocks_EmptyPassword_Error(t *testing.T) {
	// base64url("aes-128-gcm:") = "YWVzLTEyOC1nY206"
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:"))
	_, err := DecodeShadowsocks("ss://" + userinfo + "@host.com:443")
	if err == nil {
		t.Error("expected error for empty password")
	}
}
