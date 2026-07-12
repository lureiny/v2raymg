package subscription

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyBlockedAddr_Blocks(t *testing.T) {
	blocked := []string{
		"127.0.0.1:80",             // loopback
		"[::1]:80",                 // IPv6 loopback
		"10.0.0.1:80",             // RFC1918
		"192.168.1.1:80",          // RFC1918
		"172.16.0.1:80",           // RFC1918
		"169.254.169.254:80",      // link-local (cloud metadata)
		"0.0.0.0:80",              // unspecified
		"[::ffff:127.0.0.1]:80",   // v4-mapped loopback
		"100.64.0.1:80",           // CGNAT
		"[fc00::1]:80",            // IPv6 ULA
		"[fe80::1]:80",            // IPv6 link-local
		"[64:ff9b::7f00:1]:80",    // NAT64-embedded 127.0.0.1
		"[2002:7f00:1::1]:80",     // 6to4-embedded 127.0.0.1
		"198.18.0.1:80",           // benchmarking
		"240.0.0.1:80",            // reserved
	}
	for _, a := range blocked {
		if err := classifyBlockedAddr(a); err == nil {
			t.Errorf("classifyBlockedAddr(%q) = nil, want blocked", a)
		}
	}
}

func TestClassifyBlockedAddr_AllowsPublic(t *testing.T) {
	allowed := []string{
		"8.8.8.8:443",
		"1.1.1.1:80",
		"[2606:4700:4700::1111]:443",
	}
	for _, a := range allowed {
		if err := classifyBlockedAddr(a); err != nil {
			t.Errorf("classifyBlockedAddr(%q) = %v, want allowed", a, err)
		}
	}
}

func TestValidateOutboundURL(t *testing.T) {
	bad := []string{"file:///etc/passwd", "gopher://x/", "ftp://h/", "http://", "://nohost"}
	for _, u := range bad {
		if err := ValidateOutboundURL(u); err == nil {
			t.Errorf("ValidateOutboundURL(%q) = nil, want error", u)
		}
	}
	good := []string{"http://example.com", "https://example.com/a?b=c"}
	for _, u := range good {
		if err := ValidateOutboundURL(u); err != nil {
			t.Errorf("ValidateOutboundURL(%q) = %v, want nil", u, err)
		}
	}
}

// TestHTTPGet_GuardBlocksLoopback proves the guard is actually wired into the
// client used by httpGet: with the guard enabled, a fetch of a loopback
// httptest server is refused at dial time. (Per-hop redirect coverage follows
// from net.Dialer.Control firing on every dial, verified by
// TestClassifyBlockedAddr_Blocks covering the resolved IPs.)
func TestHTTPGet_GuardBlocksLoopback(t *testing.T) {
	restore := SetSSRFGuardEnabledForTest(true)
	defer restore()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	_, err := httpGet(srv.URL) // http://127.0.0.1:port
	if err == nil {
		t.Fatal("expected SSRF guard to block loopback fetch, got nil")
	}
	if !strings.Contains(err.Error(), "ssrf guard") {
		t.Fatalf("expected ssrf guard error, got %v", err)
	}
}
