package codec

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestDecodeSOCKS5_PlaintextAuth(t *testing.T) {
	n, err := DecodeSOCKS5("socks5://alice:secret@home.example.com:1080#HomeSocks")
	if err != nil {
		t.Fatalf("DecodeSOCKS5: %v", err)
	}
	if n.Protocol() != contracts.ProtocolSOCKS5 {
		t.Errorf("Protocol() = %q, want socks5", n.Protocol())
	}
	if n.Host != "home.example.com" || n.Port != 1080 {
		t.Errorf("host:port = %s:%d, want home.example.com:1080", n.Host, n.Port)
	}
	if n.Username != "alice" || n.Password != "secret" {
		t.Errorf("auth = %q:%q, want alice:secret", n.Username, n.Password)
	}
	if n.NodeName != "HomeSocks" {
		t.Errorf("NodeName = %q, want HomeSocks", n.NodeName)
	}
}

func TestDecodeSOCKS5_Socks5hAndUnauthenticated(t *testing.T) {
	n, err := DecodeSOCKS5("socks5h://192.168.1.1:1080#Router")
	if err != nil {
		t.Fatalf("DecodeSOCKS5: %v", err)
	}
	if n.Host != "192.168.1.1" || n.Port != 1080 {
		t.Errorf("host:port = %s:%d", n.Host, n.Port)
	}
	if n.Username != "" || n.Password != "" {
		t.Errorf("expected empty credentials, got %q:%q", n.Username, n.Password)
	}
	if n.NodeName != "Router" {
		t.Errorf("NodeName = %q, want Router", n.NodeName)
	}
}

func TestDecodeSOCKS5_V2RayNBase64_IPv6(t *testing.T) {
	creds := base64.StdEncoding.EncodeToString([]byte("alice:p?ss/with=chars"))
	raw := "socks://" + creds + "@[2001:db8::1]:10808#HomeV6"
	n, err := DecodeSOCKS5(raw)
	if err != nil {
		t.Fatalf("DecodeSOCKS5: %v", err)
	}
	if n.Host != "2001:db8::1" || n.Port != 10808 {
		t.Errorf("host:port = %q:%d, want 2001:db8::1:10808", n.Host, n.Port)
	}
	if n.Username != "alice" || n.Password != "p?ss/with=chars" {
		t.Errorf("auth = %q:%q, want alice:p?ss/with=chars", n.Username, n.Password)
	}
	if n.NodeName != "HomeV6" {
		t.Errorf("NodeName = %q, want HomeV6", n.NodeName)
	}
}

func TestDecodeSOCKS5_V2RayNBase64_WithSlashInBase64(t *testing.T) {
	// "user:??" encodes to "dXNlcjo/Pw==" (contains literal '/' in StdEncoding)
	creds := base64.StdEncoding.EncodeToString([]byte("user:??"))
	if !strings.Contains(creds, "/") {
		t.Fatalf("expected '/' in test base64 %q", creds)
	}
	raw := "socks://" + creds + "@[2001:db8::2]:1080#Slash"
	n, err := DecodeSOCKS5(raw)
	if err != nil {
		t.Fatalf("DecodeSOCKS5 with '/' in base64: %v", err)
	}
	if n.Username != "user" || n.Password != "??" {
		t.Errorf("auth = %q:%q, want user:??", n.Username, n.Password)
	}
	if n.Host != "2001:db8::2" || n.Port != 1080 {
		t.Errorf("host:port = %q:%d", n.Host, n.Port)
	}
}

func TestDecodeSOCKS5_FullBase64(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("bob:pwd@[2001:db8::5]:1081"))
	n, err := DecodeSOCKS5("socks://" + payload + "#Full")
	if err != nil {
		t.Fatalf("DecodeSOCKS5 full base64: %v", err)
	}
	if n.Host != "2001:db8::5" || n.Port != 1081 || n.Username != "bob" || n.Password != "pwd" {
		t.Errorf("got %+v", n)
	}
}

func TestDecodeSOCKS5_UsernameOnly_NotMisdecodedAsBase64(t *testing.T) {
	for _, user := range []string{"root", "user", "admin", "alice"} {
		n, err := DecodeSOCKS5("socks5://" + user + "@10.0.0.1:1080")
		if err != nil {
			t.Fatalf("DecodeSOCKS5(%s): %v", user, err)
		}
		if n.Username != user || n.Password != "" {
			t.Errorf("user %s misparsed as %q:%q", user, n.Username, n.Password)
		}
	}
}

func TestDecodeSOCKS5_Errors(t *testing.T) {
	cases := []string{
		"http://host:8080",
		"socks5://host",
		"socks5://:1080",
		"socks5://host:0",
		"socks5://host:70000",
		"socks5://[2001:db8::1]",
	}
	for _, uri := range cases {
		if _, err := DecodeSOCKS5(uri); err == nil {
			t.Errorf("expected error for %q", uri)
		}
	}
}

func TestSOCKS5_RoundTrip(t *testing.T) {
	cases := []*SOCKS5Node{
		{NodeName: "Unauth", Host: "10.0.0.1", Port: 1080},
		{NodeName: "Auth", Host: "home.ddns.example", Port: 1080, Username: "u1", Password: "p:1"},
		{NodeName: "IPv6", Host: "2001:db8::1", Port: 10808, Username: "alice", Password: "secret"},
		{NodeName: "UserOnly", Host: "100.64.0.1", Port: 1080, Username: "onlyuser"},
	}
	for _, orig := range cases {
		encoded := orig.Encode()
		decoded, err := DecodeSOCKS5(encoded)
		if err != nil {
			t.Fatalf("Decode(Encode(%+v)) = %v", orig, err)
		}
		if !reflect.DeepEqual(decoded, orig) {
			t.Errorf("round-trip mismatch\n got  %+v\n want %+v", decoded, orig)
		}
	}
}
