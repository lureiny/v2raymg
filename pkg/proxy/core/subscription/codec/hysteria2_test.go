package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeHysteria2_Basic(t *testing.T) {
	raw := "hysteria2://mypwd@hy2.example.com:443?sni=hy2.example.com#Node"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if n.Host != "hy2.example.com" {
		t.Errorf("Host = %q, want hy2.example.com", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.Password != "mypwd" {
		t.Errorf("Password = %q, want mypwd", n.Password)
	}
	if n.SNI != "hy2.example.com" {
		t.Errorf("SNI = %q, want hy2.example.com", n.SNI)
	}
	if n.NodeName != "Node" {
		t.Errorf("NodeName = %q, want Node", n.NodeName)
	}
}

func TestDecodeHysteria2_Hy2Alias(t *testing.T) {
	raw := "hy2://pwd@host.com:443?sni=host.com#Alias"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2(hy2://): %v", err)
	}
	if n.Host != "host.com" {
		t.Errorf("Host = %q, want host.com", n.Host)
	}
	if n.NodeName != "Alias" {
		t.Errorf("NodeName = %q, want Alias", n.NodeName)
	}
}

func TestDecodeHysteria2_Insecure(t *testing.T) {
	raw := "hysteria2://pwd@host.com:443?sni=host.com&insecure=1#I"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if !n.SkipCertVerify {
		t.Error("SkipCertVerify should be true when insecure=1")
	}
}

func TestDecodeHysteria2_Obfs(t *testing.T) {
	raw := "hysteria2://pwd@host.com:443?sni=host.com&obfs=salamander&obfs-password=obfspwd#Obfs"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if n.Obfs != "salamander" {
		t.Errorf("Obfs = %q, want salamander", n.Obfs)
	}
	if n.ObfsPassword != "obfspwd" {
		t.Errorf("ObfsPassword = %q, want obfspwd", n.ObfsPassword)
	}
}

func TestDecodeHysteria2_ALPN_Single(t *testing.T) {
	raw := "hysteria2://pwd@host.com:443?sni=host.com&alpn=h3#ALPN"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3"}) {
		t.Errorf("ALPN = %v, want [h3]", n.ALPN)
	}
}

func TestDecodeHysteria2_ALPN_Multi(t *testing.T) {
	raw := "hysteria2://pwd@host.com:443?alpn=h3%2Ch2#N"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3", "h2"}) {
		t.Errorf("ALPN = %v, want [h3 h2]", n.ALPN)
	}
}

// Some clients serialize ALPN as repeated query keys instead of a single
// comma-separated value; Decode must accept both forms.
func TestDecodeHysteria2_ALPN_RepeatedQueryKey(t *testing.T) {
	raw := "hysteria2://pwd@host.com:443?alpn=h3&alpn=h2#N"
	n, err := DecodeHysteria2(raw)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if !reflect.DeepEqual(n.ALPN, []string{"h3", "h2"}) {
		t.Errorf("ALPN = %v, want [h3 h2]", n.ALPN)
	}
}

func TestDecodeHysteria2_MissingPort_Error(t *testing.T) {
	_, err := DecodeHysteria2("hysteria2://pwd@host.com")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeHysteria2_WrongScheme_Error(t *testing.T) {
	_, err := DecodeHysteria2("trojan://pwd@host.com:443")
	if err == nil {
		t.Error("expected error for non-hysteria2 scheme")
	}
}

// --- Encode + RoundTrip ---

func TestEncodeHysteria2_CanonicalScheme(t *testing.T) {
	// Hy2 alias input should round-trip through canonical hysteria2://
	orig, err := DecodeHysteria2("hy2://pwd@host.com:443?sni=host.com#N")
	if err != nil {
		t.Fatalf("Decode hy2: %v", err)
	}
	encoded := orig.Encode()
	if !strings.HasPrefix(encoded, "hysteria2://") {
		t.Errorf("canonical encode should use hysteria2:// prefix, got %s", encoded)
	}
}

func TestEncodeHysteria2_OmitsEmptyOptionals(t *testing.T) {
	n := &Hysteria2Node{Host: "h", Port: 443, Password: "p"}
	s := n.Encode()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	q := u.Query()
	if q.Get("sni") != "" {
		t.Errorf("sni should be omitted when empty, got %q", q.Get("sni"))
	}
	if q.Get("insecure") != "" {
		t.Errorf("insecure should be omitted when false, got %q", q.Get("insecure"))
	}
	if q.Get("obfs") != "" {
		t.Errorf("obfs should be omitted when empty, got %q", q.Get("obfs"))
	}
}

func TestHysteria2_RoundTrip_Basic(t *testing.T) {
	orig := &Hysteria2Node{
		NodeName: "Basic",
		Host:     "host.com",
		Port:     443,
		Password: "pwd",
		SNI:      "host.com",
	}
	decoded, err := DecodeHysteria2(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestHysteria2_RoundTrip_FullFields(t *testing.T) {
	orig := &Hysteria2Node{
		NodeName:       "Full",
		Host:           "host.com",
		Port:           443,
		Password:       "pwd",
		SNI:            "host.com",
		SkipCertVerify: true,
		ALPN:           []string{"h3", "h2"},
		Obfs:           "salamander",
		ObfsPassword:   "obfspwd",
	}
	decoded, err := DecodeHysteria2(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestHysteria2Node_ImplementsInterface(t *testing.T) {
	var _ Node = (*Hysteria2Node)(nil)
}

// --- Regression: fields that broke pre-review ---

func TestHysteria2_RoundTrip_IPv6Host(t *testing.T) {
	orig := &Hysteria2Node{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		Password: "pwd",
		SNI:      "example.com",
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeHysteria2(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

func TestDecodeHysteria2_EmptyPassword_Error(t *testing.T) {
	_, err := DecodeHysteria2("hysteria2://@host.com:443#N")
	if err == nil {
		t.Error("expected error for empty password")
	}
}
