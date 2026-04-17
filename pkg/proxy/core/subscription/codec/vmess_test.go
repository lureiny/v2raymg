package codec

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func makeVMessURI(t *testing.T, body map[string]any) string {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(b)
}

func TestDecodeVMess_BasicTCPTLS(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"v": "2", "ps": "NodeA", "add": "us.example.com", "port": "443",
		"id": "uuid-abc", "aid": "0", "net": "tcp", "type": "none", "tls": "tls",
		"sni": "us.example.com", "fp": "chrome",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Host != "us.example.com" {
		t.Errorf("Host = %q, want us.example.com", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.UUID != "uuid-abc" {
		t.Errorf("UUID = %q, want uuid-abc", n.UUID)
	}
	if n.AlterId != 0 {
		t.Errorf("AlterId = %d, want 0", n.AlterId)
	}
	if n.Security != "tls" {
		t.Errorf("Security = %q, want tls", n.Security)
	}
	if n.SNI != "us.example.com" {
		t.Errorf("SNI = %q, want us.example.com", n.SNI)
	}
	if n.Fingerprint != "chrome" {
		t.Errorf("Fingerprint = %q, want chrome", n.Fingerprint)
	}
	// wire "tcp" normalizes to "" — same canonical as missing net field.
	if n.Transport != "" {
		t.Errorf("Transport = %q, want empty (tcp normalized)", n.Transport)
	}
	if n.NodeName != "NodeA" {
		t.Errorf("NodeName = %q, want NodeA", n.NodeName)
	}
}

func TestDecodeVMess_PortAsNumber(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": 8443,
		"id": "uuid", "aid": 0, "net": "tcp",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Port != 8443 {
		t.Errorf("Port = %d, want 8443 (number in JSON)", n.Port)
	}
}

func TestDecodeVMess_AidAsNumber(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": 443,
		"id": "uuid", "aid": 64, "net": "tcp",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.AlterId != 64 {
		t.Errorf("AlterId = %d, want 64 (number in JSON)", n.AlterId)
	}
}

func TestDecodeVMess_WS(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "WS", "add": "ws.host.com", "port": "8080",
		"id": "uuid-ws", "aid": "0", "net": "ws",
		"path": "/ws", "host": "cdn.host.com", "tls": "tls",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "ws" {
		t.Errorf("Transport = %q, want ws", n.Transport)
	}
	if n.WSPath != "/ws" {
		t.Errorf("WSPath = %q, want /ws", n.WSPath)
	}
	if n.WSHost != "cdn.host.com" {
		t.Errorf("WSHost = %q, want cdn.host.com", n.WSHost)
	}
}

func TestDecodeVMess_H2(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "H2", "add": "h2.host.com", "port": "443",
		"id": "uuid-h2", "aid": "0", "net": "h2",
		"path": "/api", "host": "h2.host.com", "tls": "tls",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "h2" {
		t.Errorf("Transport = %q, want h2", n.Transport)
	}
	if n.H2Path != "/api" {
		t.Errorf("H2Path = %q, want /api", n.H2Path)
	}
	if n.H2Host != "h2.host.com" {
		t.Errorf("H2Host = %q, want h2.host.com", n.H2Host)
	}
}

func TestDecodeVMess_GRPC(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "G", "add": "g.host.com", "port": "443",
		"id": "uuid-g", "aid": "0", "net": "grpc",
		"path": "my-service", "tls": "tls",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "grpc" {
		t.Errorf("Transport = %q, want grpc", n.Transport)
	}
	if n.GRPCServiceName != "my-service" {
		t.Errorf("GRPCServiceName = %q, want my-service", n.GRPCServiceName)
	}
}

func TestDecodeVMess_MissingNetPreservesEmpty(t *testing.T) {
	// When net is omitted, we preserve "" rather than defaulting to "tcp" so that
	// round-trip is symmetric. Callers who need the VMess default semantics can
	// branch on Transport == "".
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "uuid", "aid": "0",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "" {
		t.Errorf("Transport = %q, want empty (preserved from missing net)", n.Transport)
	}
}

func TestDecodeVMess_InvalidBase64(t *testing.T) {
	_, err := DecodeVMess("vmess://!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecodeVMess_InvalidJSON(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("this is not json"))
	_, err := DecodeVMess("vmess://" + body)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDecodeVMess_MissingAdd(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "port": "443", "id": "uuid", "aid": "0",
		// add omitted
	})
	_, err := DecodeVMess(uri)
	if err == nil {
		t.Error("expected error for missing add")
	}
}

func TestDecodeVMess_MissingPort(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "id": "uuid", "aid": "0",
	})
	_, err := DecodeVMess(uri)
	if err == nil {
		t.Error("expected error for missing port")
	}
}

// --- Encode + RoundTrip ---

func TestEncodeVMess_BasicStructure(t *testing.T) {
	n := &VMessNode{
		NodeName: "Test",
		Host:     "host.com",
		Port:     443,
		UUID:     "uuid",
		AlterId:  0,
		Security: "tls",
		SNI:      "host.com",
	}
	uri := n.Encode()
	if !strings.HasPrefix(uri, "vmess://") {
		t.Fatalf("missing vmess:// prefix: %s", uri)
	}
	body := strings.TrimPrefix(uri, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(decoded, &obj); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if obj["add"] != "host.com" {
		t.Errorf("add = %v, want host.com", obj["add"])
	}
	if obj["port"] != "443" {
		t.Errorf("port = %v, want \"443\" (string)", obj["port"])
	}
	if obj["tls"] != "tls" {
		t.Errorf("tls = %v, want tls", obj["tls"])
	}
}

func TestVMess_RoundTrip_TCPTLS(t *testing.T) {
	orig := &VMessNode{
		NodeName:    "TCP",
		Host:        "host.com",
		Port:        443,
		UUID:        "uuid-abc",
		AlterId:     0,
		Security:    "tls",
		SNI:         "host.com",
		Fingerprint: "chrome",
		// Transport omitted: tcp is the canonical empty, wire emits "tcp", Decode normalizes back.
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVMess_RoundTrip_WS(t *testing.T) {
	orig := &VMessNode{
		NodeName: "WS", Host: "ws.host.com", Port: 8080,
		UUID: "uuid", AlterId: 0,
		Security: "tls", SNI: "ws.host.com",
		Transport: "ws", WSPath: "/ws", WSHost: "cdn.host.com",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVMess_RoundTrip_GRPC(t *testing.T) {
	orig := &VMessNode{
		NodeName: "G", Host: "g.host.com", Port: 443,
		UUID: "uuid", AlterId: 0,
		Security: "tls", SNI: "g.host.com",
		Transport: "grpc", GRPCServiceName: "svc",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVMess_RoundTrip_H2(t *testing.T) {
	orig := &VMessNode{
		NodeName: "H2", Host: "h2.host.com", Port: 443,
		UUID: "uuid", AlterId: 0,
		Security: "tls", SNI: "h2.host.com",
		Transport: "h2", H2Path: "/api", H2Host: "h2.host.com",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVMess_RoundTrip_AlterIdNonZero(t *testing.T) {
	orig := &VMessNode{
		NodeName: "Legacy", Host: "h", Port: 443,
		UUID: "uuid", AlterId: 32,
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.AlterId != 32 {
		t.Errorf("AlterId round-trip lost: got %d, want 32", decoded.AlterId)
	}
}

func TestVMessNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*VMessNode)(nil)
}

// --- Regression: fields that broke pre-review ---

// Round-trip must preserve an empty Transport; previously Encode defaulted to
// "tcp" and Decode defaulted missing net to "tcp", making Transport="" asymmetric.
func TestVMess_RoundTrip_EmptyTransport(t *testing.T) {
	orig := &VMessNode{
		NodeName:  "Empty",
		Host:      "host.com",
		Port:      443,
		UUID:      "uuid",
		Transport: "",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Transport != "" {
		t.Errorf("Transport = %q, want empty (round-trip should not promote to tcp)", decoded.Transport)
	}
}

func TestVMess_RoundTrip_IPv6Host(t *testing.T) {
	// VMess stores Host in JSON body, not URI authority, so brackets are not used —
	// but the round-trip of an IPv6 literal must still preserve it intact.
	orig := &VMessNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		UUID:     "uuid",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

// Guards against someone later adding [brackets] to VMess JSON fields (wrong:
// VMess uses JSON body, not URL authority). If a future change routes VMess
// IPv6 through hostPort(), this test will catch the double-bracket regression.
func TestVMess_RoundTrip_IPv6HostAndIPv6WSHost(t *testing.T) {
	orig := &VMessNode{
		NodeName:  "v6-ws",
		Host:      "2001:db8::1",
		Port:      443,
		UUID:      "uuid",
		Transport: "ws",
		WSPath:    "/ws",
		WSHost:    "2001:db8::2",
	}
	uri := orig.Encode()
	if strings.Contains(uri, "[2001:db8::") {
		t.Errorf("VMess URI unexpectedly brackets IPv6 literal: %s", uri)
	}
	decoded, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
	if decoded.WSHost != "2001:db8::2" {
		t.Errorf("WSHost = %q, want 2001:db8::2", decoded.WSHost)
	}
}

// VMess "type" field (header obfuscation) must round-trip; previously it was
// hardcoded to "none" on Encode and unread on Decode, silently dropping the value.
func TestVMess_RoundTrip_HeaderType(t *testing.T) {
	orig := &VMessNode{
		NodeName:   "HTTP",
		Host:       "host.com",
		Port:       443,
		UUID:       "uuid",
		HeaderType: "http",
	}
	decoded, err := DecodeVMess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.HeaderType != "http" {
		t.Errorf("HeaderType = %q, want http", decoded.HeaderType)
	}
}

// "type":"none" in the wire body is the default and decodes to "" canonically.
func TestDecodeVMess_NoneHeaderTypeNormalized(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "uuid", "aid": "0", "net": "tcp", "type": "none",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.HeaderType != "" {
		t.Errorf("HeaderType = %q, want empty (none normalized)", n.HeaderType)
	}
}

// VMess wire "tcp" is the default transport; Decode collapses it to "" to
// give Transport a single canonical form (like VLess/Trojan).
func TestDecodeVMess_TCPNormalizedToEmpty(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "uuid", "aid": "0", "net": "tcp",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "" {
		t.Errorf("Transport = %q, want empty (tcp normalized)", n.Transport)
	}
}

// wire "http" is a legacy alias for "h2"; Decode canonicalizes so that
// caller-facing Transport has a single value per semantic.
func TestDecodeVMess_HTTPNormalizedToH2(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "uuid", "aid": "0", "net": "http", "path": "/a", "host": "h",
	})
	n, err := DecodeVMess(uri)
	if err != nil {
		t.Fatalf("DecodeVMess: %v", err)
	}
	if n.Transport != "h2" {
		t.Errorf("Transport = %q, want h2 (http normalized)", n.Transport)
	}
	if n.H2Path != "/a" || n.H2Host != "h" {
		t.Errorf("h2 path/host not preserved under http→h2 normalization")
	}
}

// Encode must emit wire-level "tcp" when Transport is empty so clients that
// reject "net":"" (e.g. sing-box) still accept the URI.
func TestEncodeVMess_EmptyTransportEmitsWireTCP(t *testing.T) {
	n := &VMessNode{Host: "h", Port: 443, UUID: "u"}
	uri := n.Encode()
	body := strings.TrimPrefix(uri, "vmess://")
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("json: %v", err)
	}
	if obj["net"] != "tcp" {
		t.Errorf("wire net = %v, want \"tcp\" (client compat)", obj["net"])
	}
}

func TestDecodeVMess_EmptyID_Error(t *testing.T) {
	uri := makeVMessURI(t, map[string]any{
		"ps": "N", "add": "host", "port": "443",
		"id": "", "aid": "0",
	})
	_, err := DecodeVMess(uri)
	if err == nil {
		t.Error("expected error for empty UUID")
	}
}
