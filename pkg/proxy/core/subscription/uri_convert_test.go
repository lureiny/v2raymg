package subscription

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// The low-level URI parsers moved to pkg/proxy/core/subscription/codec and have
// their own exhaustive tests. The tests here verify urisToSpecs's integration:
// typed Node → SubscriptionSpec.Extensions mapping that the converter layer reads.

func firstSpec(t *testing.T, uri string) contracts.SubscriptionSpec {
	t.Helper()
	specs := urisToSpecs([]string{uri})
	if len(specs) != 1 {
		t.Fatalf("urisToSpecs returned %d specs, want 1", len(specs))
	}
	return specs[0]
}

// --- urisToSpecs: happy-path spec fields ---

func TestURIsToSpecs_VLess_CoreFields(t *testing.T) {
	spec := firstSpec(t, "vless://uuid-abc@host.com:443?security=tls&sni=host.com&fp=chrome#N")
	if spec.Protocol != contracts.ProtocolVLess {
		t.Errorf("Protocol = %q, want vless", spec.Protocol)
	}
	if spec.Host != "host.com" {
		t.Errorf("Host = %q, want host.com", spec.Host)
	}
	if spec.Port != 443 {
		t.Errorf("Port = %d, want 443", spec.Port)
	}
	if spec.Password != "uuid-abc" {
		t.Errorf("Password (uuid) = %q, want uuid-abc", spec.Password)
	}
	if spec.NodeName != "N" {
		t.Errorf("NodeName = %q, want N", spec.NodeName)
	}
}

func TestURIsToSpecs_VLessReality_ExtensionsMapping(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=reality&flow=xtls-rprx-vision&pbk=pk&sid=sid1&fp=chrome&sni=host.com#R"
	spec := firstSpec(t, raw)
	check := func(key, want string) {
		t.Helper()
		got, _ := spec.Extensions[key].(string)
		if got != want {
			t.Errorf("extensions[%q] = %q, want %q", key, got, want)
		}
	}
	check("security", "reality")
	check("flow", "xtls-rprx-vision")
	check("reality_public_key", "pk")
	check("reality_short_ids", "sid1")
	check("utls_fingerprint", "chrome")
	check("server_name", "host.com")
}

func TestURIsToSpecs_VLess_TCPNotInExtensions(t *testing.T) {
	// tcp is the canonical default; codec normalizes it to "" so converters
	// can't see "transport":"tcp" in Extensions (canonical form).
	spec := firstSpec(t, "vless://uuid@host.com:443?security=tls&type=tcp#N")
	if _, has := spec.Extensions["transport"]; has {
		t.Errorf("transport key should be absent for canonical tcp, got %v", spec.Extensions["transport"])
	}
}

func TestURIsToSpecs_VLessWS_PathHostRouting(t *testing.T) {
	spec := firstSpec(t, "vless://uuid@host.com:443?security=tls&type=ws&path=%2Fws&host=cdn.host.com#N")
	if got, _ := spec.Extensions["transport"].(string); got != "ws" {
		t.Errorf("transport = %q, want ws", got)
	}
	if got, _ := spec.Extensions["ws_path"].(string); got != "/ws" {
		t.Errorf("ws_path = %q, want /ws", got)
	}
	if got, _ := spec.Extensions["ws_host"].(string); got != "cdn.host.com" {
		t.Errorf("ws_host = %q, want cdn.host.com", got)
	}
	if _, has := spec.Extensions["xhttp_path"]; has {
		t.Error("xhttp_path must not be set for ws transport")
	}
}

func TestURIsToSpecs_VLessXHTTP_PathHostRouting(t *testing.T) {
	spec := firstSpec(t, "vless://uuid@host.com:443?security=tls&type=xhttp&path=%2Fxh&host=xh.host.com&mode=stream-one#N")
	if got, _ := spec.Extensions["xhttp_path"].(string); got != "/xh" {
		t.Errorf("xhttp_path = %q, want /xh", got)
	}
	if got, _ := spec.Extensions["xhttp_host"].(string); got != "xh.host.com" {
		t.Errorf("xhttp_host = %q, want xh.host.com", got)
	}
	if got, _ := spec.Extensions["xhttp_mode"].(string); got != "stream-one" {
		t.Errorf("xhttp_mode = %q, want stream-one", got)
	}
}

func TestURIsToSpecs_VLessGRPC_ServiceName(t *testing.T) {
	spec := firstSpec(t, "vless://uuid@host.com:443?security=tls&type=grpc&serviceName=svc#N")
	if got, _ := spec.Extensions["grpc_service_name"].(string); got != "svc" {
		t.Errorf("grpc_service_name = %q, want svc", got)
	}
}

func TestURIsToSpecs_Hysteria2_Insecure(t *testing.T) {
	spec := firstSpec(t, "hysteria2://pwd@host.com:443?insecure=1&sni=host.com#N")
	if got, _ := spec.Extensions["skip_cert_verify"].(bool); !got {
		t.Error("skip_cert_verify should be true when insecure=1")
	}
}

func TestURIsToSpecs_Hysteria2_ALPN(t *testing.T) {
	spec := firstSpec(t, "hysteria2://pwd@host.com:443?alpn=h3%2Ch2#N")
	alpn, _ := spec.Extensions["alpn"].([]string)
	if len(alpn) != 2 || alpn[0] != "h3" || alpn[1] != "h2" {
		t.Errorf("alpn = %v, want [h3 h2]", alpn)
	}
}

func TestURIsToSpecs_Trojan_Reality(t *testing.T) {
	spec := firstSpec(t, "trojan://pass@host.com:443?security=reality&pbk=pk&sid=sid1&sni=host.com#N")
	if got, _ := spec.Extensions["security"].(string); got != "reality" {
		t.Errorf("security = %q, want reality", got)
	}
	if got, _ := spec.Extensions["reality_public_key"].(string); got != "pk" {
		t.Errorf("reality_public_key = %q, want pk", got)
	}
}

func TestURIsToSpecs_Trojan_SecurityTLSNormalizedAway(t *testing.T) {
	// TLS is implied for Trojan; codec normalizes security=tls to "",
	// so the Extensions map must not carry the now-empty value.
	spec := firstSpec(t, "trojan://pass@host.com:443?security=tls&sni=host.com#N")
	if _, has := spec.Extensions["security"]; has {
		t.Errorf("security key should be absent after tls→\"\" normalization, got %v", spec.Extensions["security"])
	}
}

func TestURIsToSpecs_ShadowsocksSIP002(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:mypwd"))
	spec := firstSpec(t, "ss://"+userinfo+"@host.com:8388#SS")
	if spec.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol = %q, want shadowsocks", spec.Protocol)
	}
	if spec.Password != "mypwd" {
		t.Errorf("Password = %q, want mypwd", spec.Password)
	}
	if got, _ := spec.Extensions["method"].(string); got != "chacha20-ietf-poly1305" {
		t.Errorf("method = %q, want chacha20-ietf-poly1305", got)
	}
}

func TestURIsToSpecs_ShadowsocksAEAD2022(t *testing.T) {
	spec := firstSpec(t, "ss://2022-blake3-aes-256-gcm:pwd@host.com:443#A2022")
	if got, _ := spec.Extensions["method"].(string); got != "2022-blake3-aes-256-gcm" {
		t.Errorf("method = %q", got)
	}
}

func TestURIsToSpecs_ShadowsocksPlugin(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	pluginVal := url.QueryEscape("obfs-local;obfs=tls;obfs-host=example.com")
	spec := firstSpec(t, "ss://"+userinfo+"@host.com:443?plugin="+pluginVal+"#N")
	if got, _ := spec.Extensions["plugin"].(string); got != "obfs-local" {
		t.Errorf("plugin = %q, want obfs-local", got)
	}
	if got, _ := spec.Extensions["plugin_mode"].(string); got != "tls" {
		t.Errorf("plugin_mode = %q, want tls", got)
	}
	if got, _ := spec.Extensions["plugin_host"].(string); got != "example.com" {
		t.Errorf("plugin_host = %q, want example.com", got)
	}
}

func TestURIsToSpecs_Snell_Core(t *testing.T) {
	spec := firstSpec(t, "snell://my-psk@host.com:443?version=5#N")
	if spec.Protocol != contracts.ProtocolSnell {
		t.Errorf("Protocol = %q, want snell", spec.Protocol)
	}
	if spec.Password != "my-psk" {
		t.Errorf("Password (PSK) = %q, want my-psk", spec.Password)
	}
	if got, _ := spec.Extensions["version"].(int); got != 5 {
		t.Errorf("version = %d, want 5", got)
	}
}

func TestURIsToSpecs_VMess_H2_AllFields(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ps": "H2", "add": "host.com", "port": 443,
		"id": "uuid", "aid": 0, "net": "h2",
		"path": "/api", "host": "h2.host.com", "tls": "tls", "fp": "chrome",
	})
	spec := firstSpec(t, "vmess://"+base64.StdEncoding.EncodeToString(body))
	if got, _ := spec.Extensions["transport"].(string); got != "h2" {
		t.Errorf("transport = %q, want h2", got)
	}
	if got, _ := spec.Extensions["http_path"].(string); got != "/api" {
		t.Errorf("http_path = %q, want /api", got)
	}
	if got, _ := spec.Extensions["http_host"].(string); got != "h2.host.com" {
		t.Errorf("http_host = %q, want h2.host.com", got)
	}
	if got, _ := spec.Extensions["utls_fingerprint"].(string); got != "chrome" {
		t.Errorf("utls_fingerprint = %q, want chrome", got)
	}
}

func TestURIsToSpecs_VMess_GRPC(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ps": "G", "add": "g.host.com", "port": 443,
		"id": "uuid", "aid": 0, "net": "grpc", "path": "my-svc", "tls": "tls",
	})
	spec := firstSpec(t, "vmess://"+base64.StdEncoding.EncodeToString(body))
	if got, _ := spec.Extensions["grpc_service_name"].(string); got != "my-svc" {
		t.Errorf("grpc_service_name = %q, want my-svc", got)
	}
}

func TestURIsToSpecs_Trojan_WS(t *testing.T) {
	spec := firstSpec(t, "trojan://pass@host.com:443?type=ws&path=%2Fws&host=cdn.host.com&sni=host.com#T")
	if got, _ := spec.Extensions["transport"].(string); got != "ws" {
		t.Errorf("transport = %q, want ws", got)
	}
	if got, _ := spec.Extensions["ws_path"].(string); got != "/ws" {
		t.Errorf("ws_path = %q, want /ws", got)
	}
	if got, _ := spec.Extensions["ws_host"].(string); got != "cdn.host.com" {
		t.Errorf("ws_host = %q, want cdn.host.com", got)
	}
}

func TestURIsToSpecs_Trojan_GRPC(t *testing.T) {
	spec := firstSpec(t, "trojan://pass@host.com:443?type=grpc&serviceName=svc&sni=host.com#T")
	if got, _ := spec.Extensions["grpc_service_name"].(string); got != "svc" {
		t.Errorf("grpc_service_name = %q, want svc", got)
	}
}

func TestURIsToSpecs_Hysteria2_Obfs(t *testing.T) {
	spec := firstSpec(t, "hysteria2://pwd@host.com:443?obfs=salamander&obfs-password=shh#N")
	if got, _ := spec.Extensions["obfs"].(string); got != "salamander" {
		t.Errorf("obfs = %q, want salamander", got)
	}
	if got, _ := spec.Extensions["obfs_password"].(string); got != "shh" {
		t.Errorf("obfs_password = %q, want shh", got)
	}
}

// When both mode/host (v2ray-plugin style) and obfs/obfs-host (obfs-local style)
// are present in PluginOpts, mode/host win — this is arbitrary but deterministic.
// Real plugins only use one style at a time, so the conflict shouldn't occur in
// practice; this test locks the ordering so a future reorder is flagged.
func TestURIsToSpecs_ShadowsocksPlugin_ConflictPreference(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pwd"))
	pluginVal := url.QueryEscape("custom;mode=ws;host=ws.host;obfs=tls;obfs-host=obfs.host")
	spec := firstSpec(t, "ss://"+userinfo+"@host.com:443?plugin="+pluginVal+"#N")
	if got, _ := spec.Extensions["plugin_mode"].(string); got != "ws" {
		t.Errorf("plugin_mode = %q, want ws (mode/host preferred over obfs/obfs-host)", got)
	}
	if got, _ := spec.Extensions["plugin_host"].(string); got != "ws.host" {
		t.Errorf("plugin_host = %q, want ws.host", got)
	}
}

func TestURIsToSpecs_VMess_JSONBody(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ps": "N", "add": "host.com", "port": "443",
		"id": "uuid-abc", "aid": "0", "net": "ws",
		"path": "/ws", "host": "cdn.host.com", "tls": "tls", "sni": "host.com", "fp": "chrome",
	})
	uri := "vmess://" + base64.StdEncoding.EncodeToString(body)
	spec := firstSpec(t, uri)
	if spec.Protocol != contracts.ProtocolVMess {
		t.Errorf("Protocol = %q, want vmess", spec.Protocol)
	}
	if spec.Host != "host.com" {
		t.Errorf("Host = %q, want host.com", spec.Host)
	}
	if spec.Password != "uuid-abc" {
		t.Errorf("Password (UUID) = %q", spec.Password)
	}
	if got, _ := spec.Extensions["transport"].(string); got != "ws" {
		t.Errorf("transport = %q, want ws", got)
	}
	if got, _ := spec.Extensions["ws_path"].(string); got != "/ws" {
		t.Errorf("ws_path = %q, want /ws", got)
	}
	if got, _ := spec.Extensions["security"].(string); got != "tls" {
		t.Errorf("security = %q, want tls", got)
	}
}

func TestURIsToSpecs_InvalidURIPassesThrough(t *testing.T) {
	spec := firstSpec(t, "not-a-uri")
	if spec.URI != "not-a-uri" {
		t.Errorf("URI = %q, want 'not-a-uri'", spec.URI)
	}
	if spec.Protocol != "" {
		t.Errorf("Protocol should be empty for unrecognized URI, got %q", spec.Protocol)
	}
}

func TestURIsToSpecs_SkipsEmpty(t *testing.T) {
	specs := urisToSpecs([]string{"", "ss://unparseable"})
	if len(specs) != 1 {
		t.Errorf("empty string should be skipped; got %d specs", len(specs))
	}
}

// --- BuildConvertOptions (unrelated to codec) ---

func TestBuildConvertOptions_MergesInlineAndRemoteSources(t *testing.T) {
	pgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Auto:url-test:all\n"))
	}))
	defer pgServer.Close()

	rpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote-rules:http:domain:https://example.com/remote.yaml\n"))
	}))
	defer rpServer.Close()

	ruleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("RULE-SET,remote-rules,REJECT\n"))
	}))
	defer ruleServer.Close()

	opts := BuildConvertOptions(
		[]string{"Proxy:select:all"},
		[]string{"inline-rules:http:domain:https://example.com/inline.yaml"},
		[]string{"MATCH,,Proxy"},
		pgServer.URL,
		rpServer.URL,
		ruleServer.URL,
	)

	if opts == nil {
		t.Fatal("BuildConvertOptions returned nil")
	}
	if len(opts.ProxyGroups) != 2 {
		t.Fatalf("expected 2 proxy groups, got %d", len(opts.ProxyGroups))
	}
	if len(opts.RuleProviders) != 2 {
		t.Fatalf("expected 2 rule providers, got %d", len(opts.RuleProviders))
	}
	if len(opts.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(opts.Rules))
	}
	if opts.RuleProviders[0].URL != "https://example.com/inline.yaml" {
		t.Fatalf("inline rule provider URL not preserved: %q", opts.RuleProviders[0].URL)
	}
	if opts.RuleProviders[1].URL != "https://example.com/remote.yaml" {
		t.Fatalf("remote rule provider URL not preserved: %q", opts.RuleProviders[1].URL)
	}
}

func TestBuildConvertOptions_SkipsInvalidInlineRuleProviderButKeepsValidRemote(t *testing.T) {
	rpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("remote-rules:http:domain:https://example.com/remote.yaml\n"))
	}))
	defer rpServer.Close()

	opts := BuildConvertOptions(
		nil,
		[]string{"broken-rule-provider"},
		nil,
		"",
		rpServer.URL,
		"",
	)

	if opts == nil {
		t.Fatal("BuildConvertOptions returned nil")
	}
	if len(opts.RuleProviders) != 1 {
		t.Fatalf("expected 1 valid remote rule provider, got %d", len(opts.RuleProviders))
	}
	if opts.RuleProviders[0].Name != "remote-rules" {
		t.Fatalf("unexpected remote rule provider name: %q", opts.RuleProviders[0].Name)
	}
	if opts.RuleProviders[0].URL != "https://example.com/remote.yaml" {
		t.Fatalf("unexpected remote rule provider URL: %q", opts.RuleProviders[0].URL)
	}
}

func TestURIsToSpecs_VMess_SkipCertVerifyAndScy(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"ps": "HomeVMess", "add": "home.example.com", "port": 443,
		"id": "uuid-v", "aid": 0, "scy": "aes-128-gcm",
		"net": "ws", "tls": "tls", "allowInsecure": "1",
	})
	spec := firstSpec(t, "vmess://"+base64.StdEncoding.EncodeToString(body))
	if got, _ := spec.Extensions["skip_cert_verify"].(bool); !got {
		t.Errorf("skip_cert_verify = %v, want true", spec.Extensions["skip_cert_verify"])
	}
	if got, _ := spec.Extensions["cipher"].(string); got != "aes-128-gcm" {
		t.Errorf("cipher = %q, want aes-128-gcm", got)
	}
}

func TestURIsToSpecs_Shadowsocks_ShadowTLS_PasswordAndPasswd(t *testing.T) {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:ss-pass"))
	for _, key := range []string{"password", "passwd"} {
		pluginVal := url.QueryEscape("shadow-tls;host=www.microsoft.com;" + key + "=stls-secret;version=3")
		spec := firstSpec(t, "ss://"+userinfo+"@home.example.com:443?plugin="+pluginVal+"#STLS")
		if got, _ := spec.Extensions["plugin_password"].(string); got != "stls-secret" {
			t.Errorf("%s: plugin_password = %q, want stls-secret", key, got)
		}
		if got, _ := spec.Extensions["plugin_version"].(string); got != "3" {
			t.Errorf("%s: plugin_version = %q, want 3", key, got)
		}
		if got, _ := spec.Extensions["plugin_host"].(string); got != "www.microsoft.com" {
			t.Errorf("%s: plugin_host = %q, want www.microsoft.com", key, got)
		}
	}
}

func TestRedactURI_StripsAllCredentialsIncludingSocksBase64(t *testing.T) {
	// socks://base64 with '/' in userinfo
	credsSlash := base64.StdEncoding.EncodeToString([]byte("user:??"))
	gotSocks := redactURI("socks://" + credsSlash + "@[2001:db8::1]:1080#Home")
	if strings.Contains(gotSocks, credsSlash) || strings.Contains(gotSocks, "user") || strings.Contains(gotSocks, "??") {
		t.Errorf("redactURI leaked socks credentials: %q", gotSocks)
	}
	if !strings.Contains(gotSocks, "[2001:db8::1]:1080") {
		t.Errorf("redactURI dropped host:port: %q", gotSocks)
	}

	// full-string base64 ss://
	ssFull := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:secret-pw@1.2.3.4:8388"))
	gotSS := redactURI("ss://" + ssFull + "#Router")
	if strings.Contains(gotSS, ssFull) || strings.Contains(gotSS, "secret-pw") {
		t.Errorf("redactURI leaked ss full base64 credentials: %q", gotSS)
	}

	// invalid full-string base64 ss:// must not leak raw blob
	gotSSBad := redactURI("ss://c2VjcmV0X2Jsb2I=")
	if strings.Contains(gotSSBad, "c2VjcmV0") {
		t.Errorf("redactURI leaked unparseable ss blob: %q", gotSSBad)
	}

	// vmess://
	if got := redactURI("VMESS://eyJhZGQiOiIxLjIuMy40In0="); got != "vmess://[redacted]" {
		t.Errorf("redactURI(VMESS) = %q, want vmess://[redacted]", got)
	}

	// credential-bearing schemes missing '@' must never echo secret
	for _, tc := range []struct {
		uri  string
		want string
	}{
		{"trojan://TOPSECRET:443", "trojan://[redacted]"},
		{"vless://TOPSECRET:443", "vless://[redacted]"},
		{"hy2://TOPSECRET:443", "hy2://[redacted]"},
		{"anytls://TOPSECRET:443", "anytls://[redacted]"},
		{"socks5://user:TOPSECRET", "socks5://[redacted]"},
	} {
		got := redactURI(tc.uri)
		if strings.Contains(got, "TOPSECRET") {
			t.Errorf("redactURI(%q) leaked secret: %q", tc.uri, got)
		}
		if got != tc.want {
			t.Errorf("redactURI(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

func TestUrisToSpecs_VMessHTTPUpgradeAndALPN(t *testing.T) {
	body := `{"ps":"VMHU","add":"vm.example.com","port":443,"id":"uuid-1","aid":0,"net":"httpupgrade","path":"/up","host":"cdn.example.com","tls":"tls","alpn":"h2,http/1.1"}`
	uri := "vmess://" + base64.StdEncoding.EncodeToString([]byte(body))
	specs := urisToSpecs([]string{uri})
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	ext := specs[0].Extensions
	if ext["httpupgrade_path"] != "/up" || ext["httpupgrade_host"] != "cdn.example.com" {
		t.Errorf("httpupgrade extensions mismatch: %+v", ext)
	}
	alpn, _ := ext["alpn"].([]string)
	if len(alpn) != 2 || alpn[0] != "h2" || alpn[1] != "http/1.1" {
		t.Errorf("alpn extension = %v, want [h2 http/1.1]", ext["alpn"])
	}
}
