package subscription

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// --- parseShadowsocksURI tests ---

func TestParseShadowsocksURI_StreamAEAD(t *testing.T) {
	// ss://base64url(chacha20-ietf-poly1305:mypassword)@example.com:8388#Node1
	// base64url("chacha20-ietf-poly1305:mypassword") = "Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk"
	raw := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk@example.com:8388#Node1"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Fatal("parseShadowsocksURI returned false")
	}
	if spec.Protocol != contracts.ProtocolShadowsocks {
		t.Errorf("Protocol: got %q, want %q", spec.Protocol, contracts.ProtocolShadowsocks)
	}
	if spec.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", spec.Host, "example.com")
	}
	if spec.Port != 8388 {
		t.Errorf("Port: got %d, want %d", spec.Port, 8388)
	}
	if spec.Password != "mypassword" {
		t.Errorf("Password: got %q, want %q", spec.Password, "mypassword")
	}
	if spec.NodeName != "Node1" {
		t.Errorf("NodeName: got %q, want %q", spec.NodeName, "Node1")
	}
	if m := spec.Extensions["method"]; m != "chacha20-ietf-poly1305" {
		t.Errorf("method: got %q, want %q", m, "chacha20-ietf-poly1305")
	}
	if spec.URI != raw {
		t.Errorf("URI: got %q, want %q", spec.URI, raw)
	}
}

func TestParseShadowsocksURI_AEAD2022(t *testing.T) {
	raw := "ss://2022-blake3-aes-256-gcm:password123@example.com:443#MyNode"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Fatal("parseShadowsocksURI returned false")
	}
	if spec.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", spec.Host, "example.com")
	}
	if spec.Port != 443 {
		t.Errorf("Port: got %d, want %d", spec.Port, 443)
	}
	if spec.Password != "password123" {
		t.Errorf("Password: got %q, want %q", spec.Password, "password123")
	}
	if spec.NodeName != "MyNode" {
		t.Errorf("NodeName: got %q, want %q", spec.NodeName, "MyNode")
	}
	if m := spec.Extensions["method"]; m != "2022-blake3-aes-256-gcm" {
		t.Errorf("method: got %q, want %q", m, "2022-blake3-aes-256-gcm")
	}
}

func TestParseShadowsocksURI_AEAD2022_PercentEncoded(t *testing.T) {
	// AEAD-2022 with percent-encoded password containing special chars
	raw := "ss://2022-blake3-aes-256-gcm:p%2Bass%3Dword@example.com:8080"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Fatal("parseShadowsocksURI returned false")
	}
	if spec.Password != "p+ass=word" {
		t.Errorf("Password: got %q, want %q", spec.Password, "p+ass=word")
	}
	if spec.Port != 8080 {
		t.Errorf("Port: got %d, want %d", spec.Port, 8080)
	}
}

func TestParseShadowsocksURI_NoFragment(t *testing.T) {
	raw := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpteXBhc3N3b3Jk@example.com:8388"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Fatal("parseShadowsocksURI returned false")
	}
	if spec.NodeName != "" {
		t.Errorf("NodeName: got %q, want empty", spec.NodeName)
	}
	if spec.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", spec.Host, "example.com")
	}
}

func TestParseShadowsocksURI_AEAD2022_NoFragment(t *testing.T) {
	raw := "ss://2022-blake3-aes-128-gcm:secret@192.168.1.1:1080"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Fatal("parseShadowsocksURI returned false")
	}
	if spec.Host != "192.168.1.1" {
		t.Errorf("Host: got %q, want %q", spec.Host, "192.168.1.1")
	}
	if spec.Port != 1080 {
		t.Errorf("Port: got %d, want %d", spec.Port, 1080)
	}
	if spec.Password != "secret" {
		t.Errorf("Password: got %q, want %q", spec.Password, "secret")
	}
	if spec.NodeName != "" {
		t.Errorf("NodeName: got %q, want empty", spec.NodeName)
	}
}

func TestParseShadowsocksURI_InvalidBase64(t *testing.T) {
	// URL parses fine but base64 decode fails → method/password empty, ok=true
	raw := "ss://not-valid-base64!!!@example.com:8388"
	spec, ok := parseShadowsocksURI(raw)
	if !ok {
		t.Error("expected true (URL is structurally valid)")
	}
	if spec.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", spec.Host, "example.com")
	}
	if spec.Password != "" {
		t.Errorf("Password: expected empty for bad base64, got %q", spec.Password)
	}
}

// --- parseStandardURI tests (covers snell) ---

func TestParseStandardURI_Snell(t *testing.T) {
	raw := "snell://my-psk-key@example.com:48162?version=5#TestNode"
	spec, ok := parseStandardURI(raw, contracts.ProtocolSnell)
	if !ok {
		t.Fatal("parseStandardURI returned false")
	}
	if spec.Protocol != contracts.ProtocolSnell {
		t.Errorf("Protocol: got %q, want %q", spec.Protocol, contracts.ProtocolSnell)
	}
	if spec.Host != "example.com" {
		t.Errorf("Host: got %q, want %q", spec.Host, "example.com")
	}
	if spec.Port != 48162 {
		t.Errorf("Port: got %d, want %d", spec.Port, 48162)
	}
	if spec.Password != "my-psk-key" {
		t.Errorf("Password (PSK): got %q, want %q", spec.Password, "my-psk-key")
	}
	if spec.NodeName != "TestNode" {
		t.Errorf("NodeName: got %q, want %q", spec.NodeName, "TestNode")
	}
}

func TestParseStandardURI_SnellNoFragment(t *testing.T) {
	raw := "snell://psk@host:1234?version=5"
	spec, ok := parseStandardURI(raw, contracts.ProtocolSnell)
	if !ok {
		t.Fatal("parseStandardURI returned false")
	}
	if spec.NodeName != "" {
		t.Errorf("NodeName: got %q, want empty", spec.NodeName)
	}
	if spec.Host != "host" {
		t.Errorf("Host: got %q, want %q", spec.Host, "host")
	}
}

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
