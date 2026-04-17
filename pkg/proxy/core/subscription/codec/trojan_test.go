package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeTrojan_Basic(t *testing.T) {
	raw := "trojan://mypass@host.com:443?sni=host.com&fp=chrome#NodeA"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Host != "host.com" {
		t.Errorf("Host = %q, want host.com", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.Password != "mypass" {
		t.Errorf("Password = %q, want mypass", n.Password)
	}
	if n.SNI != "host.com" {
		t.Errorf("SNI = %q, want host.com", n.SNI)
	}
	if n.Fingerprint != "chrome" {
		t.Errorf("Fingerprint = %q, want chrome", n.Fingerprint)
	}
	if n.NodeName != "NodeA" {
		t.Errorf("NodeName = %q, want NodeA", n.NodeName)
	}
}

func TestDecodeTrojan_Flow(t *testing.T) {
	raw := "trojan://pass@host.com:443?sni=host.com&flow=xtls-rprx-vision#F"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q, want xtls-rprx-vision", n.Flow)
	}
}

func TestDecodeTrojan_Reality(t *testing.T) {
	raw := "trojan://pass@host.com:443?security=reality&pbk=pubkey&sid=sid1&fp=chrome&sni=host.com&flow=xtls-rprx-vision#R"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Security != "reality" {
		t.Errorf("Security = %q, want reality", n.Security)
	}
	if n.RealityPublicKey != "pubkey" {
		t.Errorf("RealityPublicKey = %q, want pubkey", n.RealityPublicKey)
	}
	if n.RealityShortID != "sid1" {
		t.Errorf("RealityShortID = %q, want sid1", n.RealityShortID)
	}
}

func TestDecodeTrojan_WS(t *testing.T) {
	raw := "trojan://pass@host.com:443?type=ws&path=%2Fws&host=cdn.host.com&sni=host.com#W"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
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

func TestDecodeTrojan_GRPC(t *testing.T) {
	raw := "trojan://pass@host.com:443?type=grpc&serviceName=svc&sni=host.com#G"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Transport != "grpc" {
		t.Errorf("Transport = %q, want grpc", n.Transport)
	}
	if n.GRPCServiceName != "svc" {
		t.Errorf("GRPCServiceName = %q, want svc", n.GRPCServiceName)
	}
}

func TestDecodeTrojan_Insecure(t *testing.T) {
	raw := "trojan://pass@host.com:443?insecure=1&sni=host.com#I"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if !n.SkipCertVerify {
		t.Error("SkipCertVerify should be true when insecure=1")
	}
}

func TestDecodeTrojan_MissingPort_Error(t *testing.T) {
	_, err := DecodeTrojan("trojan://pass@host.com#NP")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeTrojan_WrongScheme_Error(t *testing.T) {
	_, err := DecodeTrojan("vless://uuid@host.com:443")
	if err == nil {
		t.Error("expected error for non-trojan scheme")
	}
}

// --- Encode + RoundTrip ---

func TestEncodeTrojan_BasicOmitsSecurity(t *testing.T) {
	n := &TrojanNode{
		NodeName: "N",
		Host:     "host.com",
		Port:     443,
		Password: "pass",
		SNI:      "host.com",
	}
	s := n.Encode()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	// Plain TLS trojan should NOT emit security param (TLS is implied)
	if u.Query().Get("security") != "" {
		t.Errorf("security should be omitted for plain TLS, got %q", u.Query().Get("security"))
	}
}

func TestEncodeTrojan_RealityEmitsSecurity(t *testing.T) {
	n := &TrojanNode{
		Host: "host", Port: 443, Password: "pass",
		Security: "reality", RealityPublicKey: "pubkey", RealityShortID: "sid",
	}
	u, err := url.Parse(n.Encode())
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	q := u.Query()
	if q.Get("security") != "reality" {
		t.Errorf("security = %q, want reality", q.Get("security"))
	}
	if q.Get("pbk") != "pubkey" {
		t.Errorf("pbk = %q, want pubkey", q.Get("pbk"))
	}
	if q.Get("sid") != "sid" {
		t.Errorf("sid = %q, want sid", q.Get("sid"))
	}
}

func TestEncodeTrojan_OmitsRealityFieldsWhenNotReality(t *testing.T) {
	n := &TrojanNode{
		Host: "h", Port: 443, Password: "p",
		RealityPublicKey: "should-not-appear",
	}
	s := n.Encode()
	if strings.Contains(s, "pbk=") {
		t.Errorf("pbk leaked when not reality: %s", s)
	}
}

func TestTrojan_RoundTrip_Basic(t *testing.T) {
	orig := &TrojanNode{
		NodeName:    "Basic",
		Host:        "host.com",
		Port:        443,
		Password:    "pass",
		SNI:         "host.com",
		Fingerprint: "chrome",
	}
	decoded, err := DecodeTrojan(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTrojan_RoundTrip_Reality(t *testing.T) {
	orig := &TrojanNode{
		NodeName:         "R",
		Host:             "host.com",
		Port:             443,
		Password:         "pass",
		Security:         "reality",
		SNI:              "host.com",
		Flow:             "xtls-rprx-vision",
		Fingerprint:      "chrome",
		RealityPublicKey: "pubkey",
		RealityShortID:   "sid",
	}
	decoded, err := DecodeTrojan(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTrojan_RoundTrip_WS(t *testing.T) {
	orig := &TrojanNode{
		NodeName:  "W",
		Host:      "host.com",
		Port:      443,
		Password:  "pass",
		SNI:       "host.com",
		Transport: "ws",
		WSPath:    "/api",
		WSHost:    "cdn.host.com",
	}
	decoded, err := DecodeTrojan(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTrojan_RoundTrip_GRPC(t *testing.T) {
	orig := &TrojanNode{
		NodeName:        "G",
		Host:            "host.com",
		Port:            443,
		Password:        "pass",
		SNI:             "host.com",
		Transport:       "grpc",
		GRPCServiceName: "svc",
	}
	decoded, err := DecodeTrojan(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTrojan_RoundTrip_XTLS(t *testing.T) {
	orig := &TrojanNode{
		NodeName: "X",
		Host:     "host.com",
		Port:     443,
		Password: "pass",
		Security: "xtls",
		SNI:      "host.com",
		Flow:     "xtls-rprx-direct",
	}
	decoded, err := DecodeTrojan(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTrojanNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*TrojanNode)(nil)
}

// --- Regression: fields that broke pre-review ---

func TestTrojan_RoundTrip_IPv6Host(t *testing.T) {
	orig := &TrojanNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		Password: "pass",
		SNI:      "example.com",
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeTrojan(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

func TestDecodeTrojan_EmptyPassword_Error(t *testing.T) {
	_, err := DecodeTrojan("trojan://@host.com:443#N")
	if err == nil {
		t.Error("expected error for empty password")
	}
}

// For Trojan, TLS is implied. URIs carrying security=tls are redundant but
// occur in the wild. Decode must normalize to "" so that Security only takes
// the canonical values {"", "reality", "xtls"} and round-trip is stable.
func TestDecodeTrojan_SecurityTLSNormalized(t *testing.T) {
	n, err := DecodeTrojan("trojan://pass@host.com:443?security=tls&sni=host.com#N")
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Security != "" {
		t.Errorf("Security = %q, want empty (tls is normalized)", n.Security)
	}
}

// Trojan requires TLS at the wire level. "security=none" is a misconfig, not
// a way to opt out — Decode collapses it to "" (same canonical as TLS implied).
func TestDecodeTrojan_SecurityNoneNormalized(t *testing.T) {
	n, err := DecodeTrojan("trojan://pass@host.com:443?security=none&sni=host.com#N")
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Security != "" {
		t.Errorf("Security = %q, want empty (none normalized)", n.Security)
	}
}

// Symmetric to TCPNormalizedToEmpty: even if a caller sets Transport="tcp"
// directly (non-canonical), Encode must omit type=tcp so round-trip yields "".
func TestEncodeTrojan_TCPOmitsType(t *testing.T) {
	n := &TrojanNode{
		Host: "host", Port: 443, Password: "p",
		Transport: "tcp",
	}
	s := n.Encode()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if got := u.Query().Get("type"); got != "" {
		t.Errorf("type = %q, want omitted for canonical tcp", got)
	}
}

// URIs with type=tcp are redundant. Decode normalizes to "".
func TestDecodeTrojan_TCPNormalizedToEmpty(t *testing.T) {
	n, err := DecodeTrojan("trojan://pass@host.com:443?type=tcp&sni=host.com#N")
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.Transport != "" {
		t.Errorf("Transport = %q, want empty (tcp normalized)", n.Transport)
	}
}

// A URI with security!=reality but carrying pbk/sid must not leak reality fields.
func TestDecodeTrojan_NonRealityIgnoresPBK(t *testing.T) {
	raw := "trojan://pass@host.com:443?pbk=stale&sid=stale&sni=host.com#N"
	n, err := DecodeTrojan(raw)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if n.RealityPublicKey != "" {
		t.Errorf("RealityPublicKey = %q, want empty when security is not reality", n.RealityPublicKey)
	}
	if n.RealityShortID != "" {
		t.Errorf("RealityShortID = %q, want empty when security is not reality", n.RealityShortID)
	}
}
