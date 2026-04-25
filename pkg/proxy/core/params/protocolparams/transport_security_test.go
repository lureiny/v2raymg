package protocolparams

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestParseTransportMissing(t *testing.T) {
	spec, err := parseTransport(map[string]any{})
	if err != nil {
		t.Fatalf("parseTransport({}) err = %v", err)
	}
	if spec != nil {
		t.Errorf("parseTransport({}) = %+v, want nil", spec)
	}
}

func TestParseTransportKinds(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want TransportSpec
	}{
		{
			name: "tcp",
			raw:  map[string]any{KeyTransport: "tcp"},
			want: TransportSpec{Kind: contracts.TransportTCP},
		},
		{
			name: "ws",
			raw: map[string]any{
				KeyTransport: "ws",
				KeyWSPath:    "/path",
				KeyWSHost:    "example.com",
			},
			want: TransportSpec{Kind: contracts.TransportWS, WSPath: "/path", WSHost: "example.com"},
		},
		{
			name: "grpc",
			raw: map[string]any{
				KeyTransport:       "grpc",
				KeyGRPCServiceName: "TunnelService",
			},
			want: TransportSpec{Kind: contracts.TransportGRPC, GRPCServiceName: "TunnelService"},
		},
		{
			name: "httpupgrade",
			raw: map[string]any{
				KeyTransport:       "httpupgrade",
				KeyHTTPUpgradePath: "/upgrade",
				KeyHTTPUpgradeHost: "cdn.example.com",
			},
			want: TransportSpec{Kind: contracts.TransportHTTPUpgrade, HTTPUpgradePath: "/upgrade", HTTPUpgradeHost: "cdn.example.com"},
		},
		{
			name: "xhttp",
			raw: map[string]any{
				KeyTransport: "xhttp",
				KeyXHTTPPath: "/xh",
				KeyXHTTPHost: "cdn.example.com",
				KeyXHTTPMode: "packet-up",
			},
			want: TransportSpec{Kind: contracts.TransportXHTTP, XHTTPPath: "/xh", XHTTPHost: "cdn.example.com", XHTTPMode: "packet-up"},
		},
		{
			name: "splithttp",
			raw: map[string]any{
				KeyTransport: "splithttp",
				KeyXHTTPPath: "/split",
			},
			want: TransportSpec{Kind: contracts.TransportSplitHTTP, XHTTPPath: "/split"},
		},
		{
			name: "h2_via_http_alias",
			raw: map[string]any{
				KeyTransport: "http",
				KeyHTTPPath:  "/h2",
				KeyHTTPHost:  "host1.example.com,host2.example.com",
			},
			want: TransportSpec{Kind: "h2", HTTPPath: "/h2", HTTPHost: []string{"host1.example.com", "host2.example.com"}},
		},
		{
			name: "h2_explicit",
			raw: map[string]any{
				KeyTransport: "h2",
				KeyHTTPPath:  "/h2",
			},
			want: TransportSpec{Kind: "h2", HTTPPath: "/h2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTransport(tc.raw)
			if err != nil {
				t.Fatalf("parseTransport err = %v", err)
			}
			if got == nil {
				t.Fatal("parseTransport returned nil")
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("parseTransport = %+v, want %+v", *got, tc.want)
			}
		})
	}
}

func TestParseTransportUnknownKind(t *testing.T) {
	_, err := parseTransport(map[string]any{KeyTransport: "quic"})
	if !errors.Is(err, ErrProtocolNotSupported) {
		t.Errorf("err = %v, want ErrProtocolNotSupported", err)
	}
}

func TestParseTransportCaseInsensitive(t *testing.T) {
	got, err := parseTransport(map[string]any{KeyTransport: "WS", KeyWSPath: "/x"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.Kind != contracts.TransportWS {
		t.Errorf("got kind = %q, want ws", got.Kind)
	}
}

func TestParseSecurityMissing(t *testing.T) {
	spec, err := parseSecurity(map[string]any{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if spec != nil {
		t.Errorf("expected nil, got %+v", spec)
	}
}

func TestParseSecurityNone(t *testing.T) {
	spec, err := parseSecurity(map[string]any{KeySecurity: "none"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if spec == nil || spec.Kind != contracts.SecurityNone {
		t.Errorf("got %+v, want Kind=none", spec)
	}
	if spec.TLS != nil || spec.Reality != nil {
		t.Errorf("sub-specs not nil: tls=%v reality=%v", spec.TLS, spec.Reality)
	}
}

func TestParseSecurityTLSFull(t *testing.T) {
	raw := map[string]any{
		KeySecurity:            "tls",
		KeySNI:                 "example.com",
		KeyALPN:                "h2,http/1.1",
		KeyCertFile:            "/etc/ssl/cert.pem",
		KeyKeyFile:             "/etc/ssl/key.pem",
		KeyCertSource:          "domain",
		KeyTLSMinVersion:       "1.2",
		KeyTLSRejectUnknownSNI: true,
		KeyUTLSFingerprint:     "chrome",
	}
	spec, err := parseSecurity(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if spec.Kind != contracts.SecurityTLS {
		t.Fatalf("kind = %q, want tls", spec.Kind)
	}
	want := TLSSpec{
		SNI:              "example.com",
		ALPN:             []string{"h2", "http/1.1"},
		CertFile:         "/etc/ssl/cert.pem",
		KeyFile:          "/etc/ssl/key.pem",
		CertSource:       "domain",
		MinVersion:       "1.2",
		RejectUnknownSNI: true,
		UTLSFingerprint:  "chrome",
	}
	if !reflect.DeepEqual(*spec.TLS, want) {
		t.Errorf("TLS = %+v\n want %+v", *spec.TLS, want)
	}
}

func TestParseSecurityTLSMismatchedCertPair(t *testing.T) {
	_, err := parseSecurity(map[string]any{
		KeySecurity: "tls",
		KeyCertFile: "/only-cert.pem",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
	_, err = parseSecurity(map[string]any{
		KeySecurity: "tls",
		KeyKeyFile:  "/only-key.pem",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

func TestParseSecurityRealityFull(t *testing.T) {
	// Three valid 16-hex shortIds + a real-shaped base64 keypair —
	// parseReality's xray-parity auto-fill preserves caller-supplied
	// values unchanged.
	raw := map[string]any{
		KeySecurity:            "reality",
		KeyRealityTarget:       "www.microsoft.com:443",
		KeyRealityServerNames:  []string{"www.microsoft.com", "microsoft.com"},
		KeyRealityShortIDs:     testRealityShortID1 + "," + testRealityShortID2 + ",ffffeeeeddddcccc",
		KeyRealityPrivateKey:   testRealityPriv,
		KeyRealityPublicKey:    testRealityPub,
		KeyRealityMinClientVer: "1.8.0",
	}
	spec, err := parseSecurity(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if spec.Reality == nil {
		t.Fatal("Reality nil")
	}
	want := RealitySpec{
		Target:       "www.microsoft.com:443",
		ServerNames:  []string{"www.microsoft.com", "microsoft.com"},
		ShortIDs:     []string{testRealityShortID1, testRealityShortID2, "ffffeeeeddddcccc"},
		PrivateKey:   testRealityPriv,
		PublicKey:    testRealityPub,
		MinClientVer: "1.8.0",
	}
	if !reflect.DeepEqual(*spec.Reality, want) {
		t.Errorf("Reality = %+v\n want %+v", *spec.Reality, want)
	}
}

func TestParseSecurityRealityAutoFillShortID(t *testing.T) {
	// No short_ids, no keys → parseReality generates both.
	raw := map[string]any{
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
	}
	spec, err := parseSecurity(raw)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	rc := spec.Reality
	if len(rc.ShortIDs) != 1 {
		t.Fatalf("ShortIDs len = %d, want 1 auto-generated entry", len(rc.ShortIDs))
	}
	if err := ValidateRealityShortID(rc.ShortIDs[0]); err != nil {
		t.Errorf("auto shortId: %v (%q)", err, rc.ShortIDs[0])
	}
	if rc.PrivateKey == "" || rc.PublicKey == "" {
		t.Fatal("key pair not auto-generated")
	}
	if err := ValidateRealityBase64Key(rc.PrivateKey); err != nil {
		t.Errorf("auto priv: %v", err)
	}
	if err := ValidateRealityBase64Key(rc.PublicKey); err != nil {
		t.Errorf("auto pub: %v", err)
	}
}

func TestParseSecurityRealityRejectsUnpairedKey(t *testing.T) {
	// Only private.
	_, err := parseSecurity(map[string]any{
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityPrivateKey:  testRealityPriv,
	})
	if err == nil {
		t.Errorf("only-private reality accepted")
	}

	// Only public.
	_, err = parseSecurity(map[string]any{
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityPublicKey:   testRealityPub,
	})
	if err == nil {
		t.Errorf("only-public reality accepted")
	}
}

func TestParseSecurityRealityRejectsInvalidKey(t *testing.T) {
	_, err := parseSecurity(map[string]any{
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityPrivateKey:  "not-a-base64-key",
		KeyRealityPublicKey:   "not-a-base64-key",
	})
	if err == nil {
		t.Error("invalid-shape keys accepted")
	}
}

func TestParseSecurityRealityRejectsInvalidShortID(t *testing.T) {
	_, err := parseSecurity(map[string]any{
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityShortIDs:    []string{"tooShort"}, // not 16 hex
	})
	if err == nil {
		t.Error("8-char shortId accepted")
	}
}

func TestParseSecurityRealityMissingTarget(t *testing.T) {
	_, err := parseSecurity(map[string]any{
		KeySecurity:           "reality",
		KeyRealityServerNames: []string{"example.com"},
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

func TestParseSecurityRealityMissingServerNames(t *testing.T) {
	_, err := parseSecurity(map[string]any{
		KeySecurity:      "reality",
		KeyRealityTarget: "www.microsoft.com:443",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

func TestParseSecurityUnknown(t *testing.T) {
	_, err := parseSecurity(map[string]any{KeySecurity: "xtls-sneaky"})
	if !errors.Is(err, ErrProtocolNotSupported) {
		t.Errorf("err = %v, want ErrProtocolNotSupported", err)
	}
}

func TestOptionalStringSliceShapes(t *testing.T) {
	if got := optionalStringSlice(map[string]any{"k": []string{"a", "b"}}, "k"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("[]string passthrough = %v", got)
	}
	if got := optionalStringSlice(map[string]any{"k": []any{"a", "b", 7, ""}}, "k"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("[]any filter = %v", got)
	}
	if got := optionalStringSlice(map[string]any{"k": " a , b , ,c "}, "k"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("csv split = %v", got)
	}
	if got := optionalStringSlice(map[string]any{"k": ""}, "k"); got != nil {
		t.Errorf("empty string, want nil, got %v", got)
	}
	if got := optionalStringSlice(map[string]any{}, "k"); got != nil {
		t.Errorf("missing key, want nil, got %v", got)
	}
}

func TestOptionalBoolShapes(t *testing.T) {
	cases := []struct {
		raw     map[string]any
		wantVal bool
		wantOk  bool
	}{
		{map[string]any{"b": true}, true, true},
		{map[string]any{"b": false}, false, true},
		{map[string]any{"b": "true"}, true, true},
		{map[string]any{"b": "FALSE"}, false, true},
		{map[string]any{"b": "yes"}, true, true},
		{map[string]any{"b": "no"}, false, true},
		{map[string]any{"b": "1"}, true, true},
		{map[string]any{"b": "0"}, false, true},
		{map[string]any{"b": "wat"}, false, false},
		{map[string]any{}, false, false},
	}
	for _, c := range cases {
		gotV, gotOk := optionalBool(c.raw, "b")
		if gotV != c.wantVal || gotOk != c.wantOk {
			t.Errorf("optionalBool(%v) = (%v,%v), want (%v,%v)", c.raw, gotV, gotOk, c.wantVal, c.wantOk)
		}
	}
}

func TestOptionalIntShapes(t *testing.T) {
	cases := []struct {
		raw     map[string]any
		wantVal int
		wantOk  bool
	}{
		{map[string]any{"i": 42}, 42, true},
		{map[string]any{"i": int32(42)}, 42, true},
		{map[string]any{"i": int64(42)}, 42, true},
		{map[string]any{"i": float64(42)}, 42, true},
		{map[string]any{"i": float64(42.5)}, 0, false},
		{map[string]any{"i": "42"}, 42, true},
		{map[string]any{"i": "bad"}, 0, false},
		{map[string]any{}, 0, false},
	}
	for _, c := range cases {
		gotV, gotOk := optionalInt(c.raw, "i")
		if gotV != c.wantVal || gotOk != c.wantOk {
			t.Errorf("optionalInt(%v) = (%v,%v), want (%v,%v)", c.raw, gotV, gotOk, c.wantVal, c.wantOk)
		}
	}
}
