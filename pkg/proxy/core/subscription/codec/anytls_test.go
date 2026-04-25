package codec

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeAnyTLS_Basic(t *testing.T) {
	raw := "anytls://pwd@anytls.example.com:443?sni=anytls.example.com#Node"
	n, err := DecodeAnyTLS(raw)
	if err != nil {
		t.Fatalf("DecodeAnyTLS: %v", err)
	}
	if n.Host != "anytls.example.com" {
		t.Errorf("Host = %q, want anytls.example.com", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.Password != "pwd" {
		t.Errorf("Password = %q, want pwd", n.Password)
	}
	if n.SNI != "anytls.example.com" {
		t.Errorf("SNI = %q", n.SNI)
	}
	if n.NodeName != "Node" {
		t.Errorf("NodeName = %q", n.NodeName)
	}
}

func TestDecodeAnyTLS_LongUserinfoForm(t *testing.T) {
	// `username:password` form — mihomo's parser uses Password() when it
	// exists; v2raymg follows suit. The username slot is decorative.
	raw := "anytls://alice:secret@host.com:443#L"
	n, err := DecodeAnyTLS(raw)
	if err != nil {
		t.Fatalf("DecodeAnyTLS: %v", err)
	}
	if n.Password != "secret" {
		t.Errorf("Password = %q, want secret (long form)", n.Password)
	}
}

func TestDecodeAnyTLS_Insecure(t *testing.T) {
	raw := "anytls://pwd@host.com:443?insecure=1#I"
	n, err := DecodeAnyTLS(raw)
	if err != nil {
		t.Fatalf("DecodeAnyTLS: %v", err)
	}
	if !n.SkipCertVerify {
		t.Error("SkipCertVerify should be true when insecure=1")
	}
}

func TestDecodeAnyTLS_Hpkp(t *testing.T) {
	raw := "anytls://pwd@host.com:443?sni=host.com&hpkp=ABCDEF1234#H"
	n, err := DecodeAnyTLS(raw)
	if err != nil {
		t.Fatalf("DecodeAnyTLS: %v", err)
	}
	if n.Fingerprint != "ABCDEF1234" {
		t.Errorf("Fingerprint = %q, want ABCDEF1234", n.Fingerprint)
	}
}

func TestDecodeAnyTLS_MissingPort(t *testing.T) {
	_, err := DecodeAnyTLS("anytls://pwd@host.com#X")
	if err == nil {
		t.Error("expected error for missing port")
	}
}

func TestDecodeAnyTLS_EmptyPassword(t *testing.T) {
	_, err := DecodeAnyTLS("anytls://@host.com:443#E")
	if err == nil {
		t.Error("expected error for empty password")
	}
}

func TestDecodeAnyTLS_WrongScheme(t *testing.T) {
	_, err := DecodeAnyTLS("hysteria2://pwd@host.com:443")
	if err == nil {
		t.Error("expected error for wrong scheme")
	}
}

func TestDecodeAnyTLS_IgnoreUnknownQuery(t *testing.T) {
	// Spec keys are sni/insecure/hpkp; an extra unrelated query key from a
	// third-party tool must not cause Decode to fail.
	raw := "anytls://pwd@host.com:443?sni=host.com&foo=bar#X"
	if _, err := DecodeAnyTLS(raw); err != nil {
		t.Fatalf("DecodeAnyTLS rejected unknown query key: %v", err)
	}
}

func TestEncodeAnyTLS_Basic(t *testing.T) {
	n := &AnyTLSNode{
		NodeName: "Node",
		Host:     "anytls.example.com",
		Port:     443,
		Password: "pwd",
		SNI:      "anytls.example.com",
	}
	uri := n.Encode()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("encoded URI does not parse: %v", err)
	}
	if u.Scheme != "anytls" {
		t.Errorf("scheme = %q, want anytls", u.Scheme)
	}
	if u.User == nil || u.User.Username() != "pwd" {
		t.Errorf("userinfo = %v, want password=pwd", u.User)
	}
	if u.Host != "anytls.example.com:443" {
		t.Errorf("host = %q, want anytls.example.com:443", u.Host)
	}
	if u.Fragment != "Node" {
		t.Errorf("fragment = %q, want Node", u.Fragment)
	}
	if got := u.Query().Get("sni"); got != "anytls.example.com" {
		t.Errorf("query sni = %q, want anytls.example.com", got)
	}
}

func TestEncodeAnyTLS_AllSpecKeys(t *testing.T) {
	n := &AnyTLSNode{
		NodeName:       "Full",
		Host:           "host.com",
		Port:           443,
		Password:       "pwd",
		SNI:            "host.com",
		SkipCertVerify: true,
		Fingerprint:    "deadbeef",
	}
	uri := n.Encode()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("encoded URI does not parse: %v", err)
	}
	q := u.Query()
	if q.Get("insecure") != "1" {
		t.Errorf("insecure query missing/wrong: %q", uri)
	}
	if q.Get("sni") != "host.com" {
		t.Errorf("sni query missing/wrong: %q", uri)
	}
	if q.Get("hpkp") != "deadbeef" {
		t.Errorf("hpkp query missing/wrong: %q", uri)
	}
}

func TestEncodeAnyTLS_OmitsExtensionsOnlyFields(t *testing.T) {
	// PaddingScheme / Idle* / MinIdleSession / ALPN are intentionally NOT
	// in the URI spec; Encode must drop them silently.
	n := &AnyTLSNode{
		Host:                     "host.com",
		Port:                     443,
		Password:                 "pwd",
		ALPN:                     []string{"h2", "http/1.1"},
		PaddingScheme:            "stop=4\n0=30-30",
		IdleSessionCheckInterval: 30,
		IdleSessionTimeout:       60,
		MinIdleSession:           3,
	}
	uri := n.Encode()
	for _, banned := range []string{"alpn=", "padding", "idle_session", "min_idle"} {
		if strings.Contains(uri, banned) {
			t.Errorf("Encode() must not include %q-shaped query key, got %q", banned, uri)
		}
	}
}

func TestRoundTripAnyTLS(t *testing.T) {
	// All URI-spec fields populated; expected to round-trip byte-equal.
	original := &AnyTLSNode{
		NodeName:       "RT",
		Host:           "rt.example.com",
		Port:           8443,
		Password:       "secret",
		SNI:            "rt.example.com",
		SkipCertVerify: true,
		Fingerprint:    "fp",
	}
	uri := original.Encode()
	got, err := DecodeAnyTLS(uri)
	if err != nil {
		t.Fatalf("DecodeAnyTLS round-trip: %v", err)
	}
	// reflect.DeepEqual catches any field added in the future that
	// silently fails to round-trip — preferred over per-field == checks.
	if !reflect.DeepEqual(got, original) {
		t.Errorf("round-trip mismatch:\n have %+v\n want %+v", got, original)
	}
}

// TestRoundTripAnyTLS_ExtensionFieldsStayZero locks the contract that
// PaddingScheme / IdleSession* / MinIdleSession / ALPN are in-memory
// carriers only and MUST NOT leak into the URI. A future Encode that
// added e.g. `&padding-scheme=...` would surface here as Decode reading
// the field back, breaking the equality.
func TestRoundTripAnyTLS_ExtensionFieldsStayZero(t *testing.T) {
	original := &AnyTLSNode{
		NodeName: "Ext",
		Host:     "ext.example.com",
		Port:     443,
		Password: "secret",
		SNI:      "ext.example.com",
		// Extension-only fields populated:
		ALPN:                     []string{"h2", "http/1.1"},
		PaddingScheme:            "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000",
		IdleSessionCheckInterval: 30,
		IdleSessionTimeout:       60,
		MinIdleSession:           3,
	}
	uri := original.Encode()
	got, err := DecodeAnyTLS(uri)
	if err != nil {
		t.Fatalf("DecodeAnyTLS round-trip: %v", err)
	}
	if got.ALPN != nil {
		t.Errorf("ALPN should be nil after URI round-trip, got %v", got.ALPN)
	}
	if got.PaddingScheme != "" {
		t.Errorf("PaddingScheme should be empty after URI round-trip, got %q", got.PaddingScheme)
	}
	if got.IdleSessionCheckInterval != 0 {
		t.Errorf("IdleSessionCheckInterval should be 0 after URI round-trip, got %d", got.IdleSessionCheckInterval)
	}
	if got.IdleSessionTimeout != 0 {
		t.Errorf("IdleSessionTimeout should be 0 after URI round-trip, got %d", got.IdleSessionTimeout)
	}
	if got.MinIdleSession != 0 {
		t.Errorf("MinIdleSession should be 0 after URI round-trip, got %d", got.MinIdleSession)
	}
	// Sanity: URI must not even mention these extension-only key names,
	// even encoded — defensive belt to the field-check above.
	for _, banned := range []string{"alpn=", "padding", "idle", "min-idle", "min_idle"} {
		if strings.Contains(uri, banned) {
			t.Errorf("URI must not contain %q-shaped extension key, got %q", banned, uri)
		}
	}
}

func TestDecodeAnyTLS_IPv6(t *testing.T) {
	raw := "anytls://pwd@[2001:db8::1]:443?sni=v6.example.com#V6"
	n, err := DecodeAnyTLS(raw)
	if err != nil {
		t.Fatalf("DecodeAnyTLS IPv6: %v", err)
	}
	if n.Host != "2001:db8::1" {
		t.Errorf("Host = %q, want 2001:db8::1 (brackets stripped by url.Hostname)", n.Host)
	}
	if n.Port != 443 {
		t.Errorf("Port = %d, want 443", n.Port)
	}
	if n.NodeName != "V6" {
		t.Errorf("NodeName = %q, want V6", n.NodeName)
	}
	// Round-trip — Encode must re-bracket the IPv6 host.
	uri := n.Encode()
	if !strings.Contains(uri, "[2001:db8::1]:443") {
		t.Errorf("re-encoded IPv6 URI missing brackets: %q", uri)
	}
	got, err := DecodeAnyTLS(uri)
	if err != nil {
		t.Fatalf("DecodeAnyTLS IPv6 round-trip: %v", err)
	}
	if !reflect.DeepEqual(got, n) {
		t.Errorf("IPv6 round-trip mismatch:\n have %+v\n want %+v", got, n)
	}
}

func TestEncodeAnyTLS_IPv6(t *testing.T) {
	n := &AnyTLSNode{
		NodeName: "v6",
		Host:     "2001:db8::1",
		Port:     443,
		Password: "pwd",
	}
	uri := n.Encode()
	if !strings.Contains(uri, "[2001:db8::1]:443") {
		t.Errorf("IPv6 host not bracketed: %q", uri)
	}
}
