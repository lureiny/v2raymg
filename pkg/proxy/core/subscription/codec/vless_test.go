package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeVLess_TCP_TLS(t *testing.T) {
	raw := "vless://uuid-abc@hk.example.com:443?security=tls&sni=hk.example.com&fp=chrome&type=tcp#HK"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	want := &VLessNode{
		NodeName:    "HK",
		Host:        "hk.example.com",
		Port:        443,
		UUID:        "uuid-abc",
		Security:    "tls",
		SNI:         "hk.example.com",
		Fingerprint: "chrome",
		// type=tcp in URI is normalized to "" (canonical default).
		Transport: "",
	}
	if !reflect.DeepEqual(n, want) {
		t.Errorf("got %+v, want %+v", n, want)
	}
}

func TestDecodeVLess_Reality_Vision(t *testing.T) {
	raw := "vless://uuid-sg@sg.example.com:443?security=reality&flow=xtls-rprx-vision&pbk=pubkey456&sid=shortid1&fp=chrome&type=tcp&sni=sg.example.com#SG"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Security != "reality" {
		t.Errorf("Security = %q, want reality", n.Security)
	}
	if n.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q, want xtls-rprx-vision", n.Flow)
	}
	if n.RealityPublicKey != "pubkey456" {
		t.Errorf("RealityPublicKey = %q, want pubkey456", n.RealityPublicKey)
	}
	if n.RealityShortID != "shortid1" {
		t.Errorf("RealityShortID = %q, want shortid1", n.RealityShortID)
	}
	if n.Fingerprint != "chrome" {
		t.Errorf("Fingerprint = %q, want chrome", n.Fingerprint)
	}
}

func TestDecodeVLess_WS(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&type=ws&path=%2Fws&host=cdn.host.com&sni=host.com#WS"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
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

func TestDecodeVLess_GRPC(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&type=grpc&serviceName=my-service&sni=host.com#GRPC"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Transport != "grpc" {
		t.Errorf("Transport = %q, want grpc", n.Transport)
	}
	if n.GRPCServiceName != "my-service" {
		t.Errorf("GRPCServiceName = %q, want my-service", n.GRPCServiceName)
	}
	if n.WSPath != "" {
		t.Errorf("WSPath should be empty for grpc, got %q", n.WSPath)
	}
}

func TestDecodeVLess_XHTTP(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&type=xhttp&path=%2Fxh&host=xh.host.com&mode=stream-one&sni=host.com#XH"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Transport != "xhttp" {
		t.Errorf("Transport = %q, want xhttp", n.Transport)
	}
	if n.XHTTPPath != "/xh" {
		t.Errorf("XHTTPPath = %q, want /xh", n.XHTTPPath)
	}
	if n.XHTTPHost != "xh.host.com" {
		t.Errorf("XHTTPHost = %q, want xh.host.com", n.XHTTPHost)
	}
	if n.XHTTPMode != "stream-one" {
		t.Errorf("XHTTPMode = %q, want stream-one", n.XHTTPMode)
	}
}

func TestDecodeVLess_Splithttp(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&type=splithttp&path=%2Fsh&host=sh.host.com#SH"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Transport != "splithttp" {
		t.Errorf("Transport = %q, want splithttp", n.Transport)
	}
	if n.XHTTPPath != "/sh" {
		t.Errorf("XHTTPPath = %q, want /sh", n.XHTTPPath)
	}
}

func TestDecodeVLess_PlainTCP_NoSecurity(t *testing.T) {
	raw := "vless://uuid@host.com:1234#Plain"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Security != "" {
		t.Errorf("Security = %q, want empty", n.Security)
	}
	if n.Port != 1234 {
		t.Errorf("Port = %d, want 1234", n.Port)
	}
}

func TestDecodeVLess_Insecure(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&insecure=1&sni=host.com#I"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if !n.SkipCertVerify {
		t.Error("SkipCertVerify should be true when insecure=1")
	}
}

func TestDecodeVLess_MissingPort_Error(t *testing.T) {
	_, err := DecodeVLess("vless://uuid@host.com#NoPort")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeVLess_WrongScheme_Error(t *testing.T) {
	_, err := DecodeVLess("vmess://uuid@host.com:443")
	if err == nil {
		t.Error("expected error for non-vless scheme")
	}
}

// --- Encode + RoundTrip ---

func TestEncodeVLess_Reality(t *testing.T) {
	n := &VLessNode{
		NodeName:         "SG",
		Host:             "sg.example.com",
		Port:             443,
		UUID:             "uuid-sg",
		Security:         "reality",
		SNI:              "sg.example.com",
		Flow:             "xtls-rprx-vision",
		Fingerprint:      "chrome",
		RealityPublicKey: "pubkey",
		RealityShortID:   "sid1",
		// leave Transport empty — tcp is the canonical default
	}
	s := n.Encode()
	if !strings.HasPrefix(s, "vless://uuid-sg@sg.example.com:443?") {
		t.Errorf("URI prefix wrong: %s", s)
	}

	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse encoded: %v", err)
	}
	q := u.Query()
	if q.Get("security") != "reality" {
		t.Error("security param lost")
	}
	if q.Get("flow") != "xtls-rprx-vision" {
		t.Error("flow param lost")
	}
	if q.Get("pbk") != "pubkey" {
		t.Error("pbk param lost")
	}
	if q.Get("sid") != "sid1" {
		t.Error("sid param lost")
	}
	if q.Get("type") != "" {
		t.Errorf("type should be omitted for default tcp, got %q", q.Get("type"))
	}
	if u.Fragment != "SG" {
		t.Errorf("Fragment = %q, want SG", u.Fragment)
	}
}

func TestEncodeVLess_OmitsRealityFieldsWhenNotReality(t *testing.T) {
	n := &VLessNode{
		Host: "h", Port: 443, UUID: "u",
		Security:         "tls",
		RealityPublicKey: "should-not-appear",
		RealityShortID:   "should-not-appear",
	}
	s := n.Encode()
	if strings.Contains(s, "pbk=") || strings.Contains(s, "sid=") {
		t.Errorf("reality fields leaked in non-reality encoding: %s", s)
	}
}

func TestVLess_RoundTrip_Reality(t *testing.T) {
	orig := &VLessNode{
		NodeName:         "Node",
		Host:             "sg.example.com",
		Port:             443,
		UUID:             "uuid-sg",
		Security:         "reality",
		SNI:              "sg.example.com",
		Flow:             "xtls-rprx-vision",
		Fingerprint:      "chrome",
		RealityPublicKey: "pubkey",
		RealityShortID:   "sid1",
	}
	decoded, err := DecodeVLess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVLess_RoundTrip_WS(t *testing.T) {
	orig := &VLessNode{
		NodeName:  "WS",
		Host:      "ws.host.com",
		Port:      8443,
		UUID:      "uuid",
		Security:  "tls",
		SNI:       "ws.host.com",
		Transport: "ws",
		WSPath:    "/api/ws",
		WSHost:    "cdn.host.com",
	}
	decoded, err := DecodeVLess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVLess_RoundTrip_GRPC(t *testing.T) {
	orig := &VLessNode{
		NodeName:        "G",
		Host:            "g.host.com",
		Port:            443,
		UUID:            "uuid",
		Security:        "tls",
		SNI:             "g.host.com",
		Transport:       "grpc",
		GRPCServiceName: "my-svc",
	}
	decoded, err := DecodeVLess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVLess_RoundTrip_XHTTP(t *testing.T) {
	orig := &VLessNode{
		NodeName:  "X",
		Host:      "x.host.com",
		Port:      443,
		UUID:      "uuid",
		Security:  "tls",
		SNI:       "x.host.com",
		Transport: "xhttp",
		XHTTPPath: "/xh",
		XHTTPHost: "xh.host.com",
		XHTTPMode: "stream-one",
	}
	decoded, err := DecodeVLess(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestVLessNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*VLessNode)(nil)
}

// --- Regression: fields that broke pre-review ---

func TestVLess_RoundTrip_IPv6Host(t *testing.T) {
	orig := &VLessNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		UUID:     "uuid",
		Security: "tls",
		SNI:      "example.com",
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeVLess(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

func TestDecodeVLess_EmptyUUID_Error(t *testing.T) {
	_, err := DecodeVLess("vless://@host.com:443?security=tls#N")
	if err == nil {
		t.Error("expected error for empty UUID")
	}
}

// security=none means "no TLS" — same canonical as empty (cf. Trojan).
func TestDecodeVLess_SecurityNoneNormalized(t *testing.T) {
	n, err := DecodeVLess("vless://uuid@host.com:443?security=none#N")
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Security != "" {
		t.Errorf("Security = %q, want empty (none normalized)", n.Security)
	}
}

// Symmetric to TCPNormalizedToEmpty: even if a caller sets Transport="tcp"
// directly (non-canonical), Encode must omit type=tcp so round-trip yields "".
func TestEncodeVLess_TCPOmitsType(t *testing.T) {
	n := &VLessNode{
		Host: "host", Port: 443, UUID: "u",
		Security: "tls", Transport: "tcp",
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

// URIs with type=tcp are redundant (tcp is the default). Decode normalizes to "".
func TestDecodeVLess_TCPNormalizedToEmpty(t *testing.T) {
	n, err := DecodeVLess("vless://uuid@host.com:443?security=tls&type=tcp#N")
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.Transport != "" {
		t.Errorf("Transport = %q, want empty (tcp normalized)", n.Transport)
	}
}

// A URI with security=tls but carrying pbk/sid params (malformed or stale config)
// must not leak reality fields into the decoded node, otherwise a subsequent Encode
// would silently drop them and round-trip would be lossy.
func TestDecodeVLess_NonRealityIgnoresPBK(t *testing.T) {
	raw := "vless://uuid@host.com:443?security=tls&pbk=should-be-ignored&sid=also-ignored#N"
	n, err := DecodeVLess(raw)
	if err != nil {
		t.Fatalf("DecodeVLess: %v", err)
	}
	if n.RealityPublicKey != "" {
		t.Errorf("RealityPublicKey = %q, want empty when security=tls", n.RealityPublicKey)
	}
	if n.RealityShortID != "" {
		t.Errorf("RealityShortID = %q, want empty when security=tls", n.RealityShortID)
	}
}
