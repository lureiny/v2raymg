package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeSnell_Basic(t *testing.T) {
	raw := "snell://my-psk@example.com:48162?version=5#TestNode"
	n, err := DecodeSnell(raw)
	if err != nil {
		t.Fatalf("DecodeSnell: %v", err)
	}
	if n.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", n.Host)
	}
	if n.Port != 48162 {
		t.Errorf("Port = %d, want 48162", n.Port)
	}
	if n.PSK != "my-psk" {
		t.Errorf("PSK = %q, want my-psk", n.PSK)
	}
	if n.Version != 5 {
		t.Errorf("Version = %d, want 5", n.Version)
	}
	if n.NodeName != "TestNode" {
		t.Errorf("NodeName = %q, want TestNode", n.NodeName)
	}
}

func TestDecodeSnell_NoFragment(t *testing.T) {
	n, err := DecodeSnell("snell://psk@host:1234?version=4")
	if err != nil {
		t.Fatalf("DecodeSnell: %v", err)
	}
	if n.NodeName != "" {
		t.Errorf("NodeName = %q, want empty", n.NodeName)
	}
}

func TestDecodeSnell_NoVersion(t *testing.T) {
	n, err := DecodeSnell("snell://psk@host:1234#N")
	if err != nil {
		t.Fatalf("DecodeSnell: %v", err)
	}
	if n.Version != 0 {
		t.Errorf("Version = %d, want 0 (unset)", n.Version)
	}
}

func TestDecodeSnell_Obfs(t *testing.T) {
	raw := "snell://psk@host:443?version=3&obfs=tls&obfs-host=example.com#Obfs"
	n, err := DecodeSnell(raw)
	if err != nil {
		t.Fatalf("DecodeSnell: %v", err)
	}
	if n.Obfs != "tls" {
		t.Errorf("Obfs = %q, want tls", n.Obfs)
	}
	if n.ObfsHost != "example.com" {
		t.Errorf("ObfsHost = %q, want example.com", n.ObfsHost)
	}
}

func TestDecodeSnell_MissingPort_Error(t *testing.T) {
	_, err := DecodeSnell("snell://psk@host")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeSnell_WrongScheme_Error(t *testing.T) {
	_, err := DecodeSnell("trojan://pwd@host:443")
	if err == nil {
		t.Error("expected error for non-snell scheme")
	}
}

// --- Encode + RoundTrip ---

func TestEncodeSnell_OmitsVersionWhenZero(t *testing.T) {
	n := &SnellNode{Host: "h", Port: 443, PSK: "p"}
	u, err := url.Parse(n.Encode())
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if u.Query().Get("version") != "" {
		t.Errorf("version should be omitted for zero, got %q", u.Query().Get("version"))
	}
}

func TestEncodeSnell_IncludesVersion(t *testing.T) {
	n := &SnellNode{Host: "h", Port: 443, PSK: "p", Version: 5}
	u, err := url.Parse(n.Encode())
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	if u.Query().Get("version") != "5" {
		t.Errorf("version = %q, want 5", u.Query().Get("version"))
	}
}

func TestSnell_RoundTrip_Basic(t *testing.T) {
	orig := &SnellNode{
		NodeName: "N",
		Host:     "host.com",
		Port:     443,
		PSK:      "my-psk",
		Version:  5,
	}
	decoded, err := DecodeSnell(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestSnell_RoundTrip_WithObfs(t *testing.T) {
	orig := &SnellNode{
		NodeName: "Obfs",
		Host:     "host.com",
		Port:     443,
		PSK:      "psk",
		Version:  3,
		Obfs:     "tls",
		ObfsHost: "example.com",
	}
	decoded, err := DecodeSnell(orig.Encode())
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if !reflect.DeepEqual(decoded, orig) {
		t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
	}
}

func TestSnellNode_ImplementsInterface(t *testing.T) {
	var _ Node = (*SnellNode)(nil)
}

// --- Regression: fields that broke pre-review ---

func TestSnell_RoundTrip_IPv6Host(t *testing.T) {
	orig := &SnellNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		PSK:      "psk",
		Version:  5,
	}
	uri := orig.Encode()
	if !strings.Contains(uri, "[2001:db8::1]") {
		t.Errorf("IPv6 host not bracketed in URI: %s", uri)
	}
	decoded, err := DecodeSnell(uri)
	if err != nil {
		t.Fatalf("Decode(Encode): %v", err)
	}
	if decoded.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1", decoded.Host)
	}
}

func TestDecodeSnell_EmptyPSK_Error(t *testing.T) {
	_, err := DecodeSnell("snell://@host.com:443?version=5#N")
	if err == nil {
		t.Error("expected error for empty PSK")
	}
}
