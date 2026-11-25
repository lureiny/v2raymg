package codec

import (
	"reflect"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestDecodeHTTP_Plain(t *testing.T) {
	n, err := DecodeHTTP("http://alice:secret@proxy.example.com:8080#HTTPProxy")
	if err != nil {
		t.Fatalf("DecodeHTTP: %v", err)
	}
	if n.Protocol() != contracts.ProtocolHTTP {
		t.Errorf("Protocol() = %q, want http", n.Protocol())
	}
	if n.Host != "proxy.example.com" || n.Port != 8080 {
		t.Errorf("host:port = %s:%d", n.Host, n.Port)
	}
	if n.Username != "alice" || n.Password != "secret" {
		t.Errorf("auth = %q:%q", n.Username, n.Password)
	}
	if n.TLS {
		t.Error("expected TLS=false for http://")
	}
	if n.NodeName != "HTTPProxy" {
		t.Errorf("NodeName = %q", n.NodeName)
	}
}

func TestDecodeHTTP_HTTPS_WithSNIAndInsecure(t *testing.T) {
	n, err := DecodeHTTP("https://bob:pwd@[2001:db8::1]:8443?sni=edge.example.com&insecure=1#HTTPSv6")
	if err != nil {
		t.Fatalf("DecodeHTTP: %v", err)
	}
	if !n.TLS {
		t.Error("expected TLS=true for https://")
	}
	if n.Host != "2001:db8::1" || n.Port != 8443 {
		t.Errorf("host:port = %q:%d", n.Host, n.Port)
	}
	if n.SNI != "edge.example.com" {
		t.Errorf("SNI = %q, want edge.example.com", n.SNI)
	}
	if !n.SkipCertVerify {
		t.Error("expected SkipCertVerify=true for insecure=1")
	}
}

func TestDecodeHTTP_Unauthenticated(t *testing.T) {
	n, err := DecodeHTTP("http://192.168.1.2:3128#LAN")
	if err != nil {
		t.Fatalf("DecodeHTTP: %v", err)
	}
	if n.Username != "" || n.Password != "" {
		t.Errorf("expected empty credentials, got %q:%q", n.Username, n.Password)
	}
	if n.TLS {
		t.Error("expected TLS=false")
	}
}

func TestDecodeHTTP_Errors(t *testing.T) {
	cases := []string{
		"socks5://host:1080",
		"http://host",
		"https://host",
		"http://:8080",
		"https://host:0",
		"https://host:65536",
	}
	for _, uri := range cases {
		if _, err := DecodeHTTP(uri); err == nil {
			t.Errorf("expected error for %q", uri)
		}
	}
}

func TestHTTP_RoundTrip(t *testing.T) {
	cases := []*HTTPNode{
		{NodeName: "Plain", Host: "10.0.0.1", Port: 3128},
		{NodeName: "Auth", Host: "proxy.example.com", Port: 8080, Username: "u", Password: "p"},
		{NodeName: "HTTPS", Host: "2001:db8::1", Port: 443, Username: "u", Password: "p", TLS: true, SNI: "sni.example.com", SkipCertVerify: true},
	}
	for _, orig := range cases {
		encoded := orig.Encode()
		if orig.TLS && !strings.HasPrefix(encoded, "https://") {
			t.Errorf("expected https:// prefix, got %s", encoded)
		}
		decoded, err := DecodeHTTP(encoded)
		if err != nil {
			t.Fatalf("Decode(Encode(%+v)) = %v", orig, err)
		}
		if !reflect.DeepEqual(decoded, orig) {
			t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
		}
	}
}
