package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const tuicCodecUUID = "00112233-4455-6677-8899-aabbccddeeff"

func TestDecodeTuic_Basic(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID + ":pwd@tuic.example.com:443?sni=tuic.example.com#Node"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if n.Host != "tuic.example.com" {
		t.Errorf("Host = %q, want tuic.example.com", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.UUID != tuicCodecUUID {
		t.Errorf("UUID = %q", n.UUID)
	}
	if n.Password != "pwd" {
		t.Errorf("Password = %q, want pwd", n.Password)
	}
	if n.SNI != "tuic.example.com" {
		t.Errorf("SNI = %q", n.SNI)
	}
	if n.NodeName != "Node" {
		t.Errorf("NodeName = %q", n.NodeName)
	}
}

// v4 token form (userinfo without colon) is mihomo-compatible per dae spec
// but v2raymg only ships v5 — Decode must reject loud rather than silently
// downgrade or drop the credential.
func TestDecodeTuic_RejectV4Token(t *testing.T) {
	raw := "tuic://sometoken@host.com:443#V4"
	_, err := DecodeTuic(raw)
	if err == nil {
		t.Error("expected error for v4 token format (userinfo without colon)")
	}
}

func TestDecodeTuic_QueryParams(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID +
		":pwd@host.com:443?sni=host.com&congestion_control=bbr&udp_relay_mode=quic&disable_sni=1#Q"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if n.CongestionControl != "bbr" {
		t.Errorf("CongestionControl = %q", n.CongestionControl)
	}
	if n.UDPRelayMode != "quic" {
		t.Errorf("UDPRelayMode = %q", n.UDPRelayMode)
	}
	if !n.DisableSNI {
		t.Error("DisableSNI should be true when disable_sni=1")
	}
}

func TestDecodeTuic_AllowInsecure(t *testing.T) {
	// dae's allow_insecure flag — mihomo strips it, but v2raymg honours
	// on Decode for cross-tool URI interop.
	raw := "tuic://" + tuicCodecUUID + ":pwd@host.com:443?sni=host.com&allow_insecure=1#I"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if !n.SkipCertVerify {
		t.Error("SkipCertVerify should be true when allow_insecure=1")
	}
}

func TestDecodeTuic_ALPN_Single(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID + ":pwd@host.com:443?alpn=h3#A"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3"}) {
		t.Errorf("ALPN = %v", n.ALPN)
	}
}

func TestDecodeTuic_ALPN_Multi_Comma(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID + ":pwd@host.com:443?alpn=h3%2Ch2#A"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3", "h2"}) {
		t.Errorf("ALPN = %v", n.ALPN)
	}
}

func TestDecodeTuic_ALPN_Repeated(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID + ":pwd@host.com:443?alpn=h3&alpn=h2#A"
	n, err := DecodeTuic(raw)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3", "h2"}) {
		t.Errorf("ALPN = %v", n.ALPN)
	}
}

func TestDecodeTuic_MissingPort_Error(t *testing.T) {
	_, err := DecodeTuic("tuic://" + tuicCodecUUID + ":pwd@host.com")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeTuic_WrongScheme_Error(t *testing.T) {
	_, err := DecodeTuic("hysteria2://pwd@host.com:443")
	if err == nil {
		t.Error("expected error for non-tuic scheme")
	}
}

func TestDecodeTuic_MissingUserinfo_Error(t *testing.T) {
	_, err := DecodeTuic("tuic://host.com:443#N")
	if err == nil {
		t.Error("expected error for missing userinfo")
	}
}

func TestEncodeTuic_OmitsEmptyOptionals(t *testing.T) {
	n := &TuicNode{Host: "h", Port: 443, UUID: tuicCodecUUID, Password: "p"}
	s := n.Encode()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	q := u.Query()
	for _, k := range []string{"sni", "alpn", "congestion_control", "udp_relay_mode", "disable_sni", "allow_insecure"} {
		if v := q.Get(k); v != "" {
			t.Errorf("query %q should be omitted when default, got %q", k, v)
		}
	}
}

// SkipCertVerify=true must round-trip: Encode emits allow_insecure=1,
// Decode restores SkipCertVerify=true.
func TestEncodeTuic_EmitsAllowInsecureWhenSkipCertVerifyTrue(t *testing.T) {
	n := &TuicNode{Host: "h", Port: 443, UUID: tuicCodecUUID, Password: "p", SkipCertVerify: true}
	s := n.Encode()
	if !strings.Contains(s, "allow_insecure=1") {
		t.Errorf("Encode must emit allow_insecure=1 when SkipCertVerify=true; got %s", s)
	}
	decoded, err := DecodeTuic(s)
	if err != nil {
		t.Fatalf("DecodeTuic failed: %v", err)
	}
	if !decoded.SkipCertVerify {
		t.Errorf("SkipCertVerify did not round-trip through allow_insecure=1")
	}
}

func TestTuic_RoundTrip_Basic(t *testing.T) {
	orig := &TuicNode{
		NodeName: "Basic",
		Host:     "host.com",
		Port:     443,
		UUID:     tuicCodecUUID,
		Password: "pwd",
		SNI:      "host.com",
	}
	decoded, err := DecodeTuic(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTuic_RoundTrip_FullURIFields(t *testing.T) {
	orig := &TuicNode{
		NodeName:          "Full",
		Host:              "host.com",
		Port:              443,
		UUID:              tuicCodecUUID,
		Password:          "pwd",
		SNI:               "host.com",
		ALPN:              []string{"h3", "h2"},
		CongestionControl: "bbr",
		UDPRelayMode:      "quic",
		DisableSNI:        true,
		// SkipCertVerify, ZeroRTTHandshake, HeartbeatInterval intentionally
		// omitted — they are not URI-bound fields.
	}
	decoded, err := DecodeTuic(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestTuicNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*TuicNode)(nil)
}

func TestTuic_RoundTrip_IPv6Host(t *testing.T) {
	orig := &TuicNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		UUID:     tuicCodecUUID,
		Password: "pwd",
		SNI:      "example.com",
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeTuic(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

// Regression: top-level Decode dispatch routes tuic:// to DecodeTuic.
func TestDispatch_TuicScheme(t *testing.T) {
	raw := "tuic://" + tuicCodecUUID + ":pwd@host.com:443#Disp"
	node, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := node.(*TuicNode); !ok {
		t.Errorf("Decode returned %T, want *TuicNode", node)
	}
}
