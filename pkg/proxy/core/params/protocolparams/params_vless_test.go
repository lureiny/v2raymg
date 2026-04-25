package protocolparams

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestParseVLESSMatrix exercises the full transport × security combinations
// that Phase 1's mihomo listener must support, per plan T-M-P1-10.
func TestParseVLESSMatrix(t *testing.T) {
	baseRaw := map[string]any{
		KeyProtocol: "vless",
		KeyPort:     uint32(443),
		KeyUUID:     "11111111-2222-3333-4444-555555555555",
	}

	type want struct {
		protocol      contracts.Protocol
		transportKind contracts.Transport
		securityKind  contracts.Security
		checkVLESS    func(t *testing.T, p *VLESSParams)
		checkTLS      func(t *testing.T, tls *TLSSpec)
		checkReality  func(t *testing.T, r *RealitySpec)
	}

	cases := []struct {
		name  string
		extra map[string]any
		want  want
	}{
		{
			name:  "tcp-none",
			extra: map[string]any{},
			want: want{
				protocol:      contracts.ProtocolVLess,
				transportKind: contracts.TransportTCP,
				securityKind:  "", // security is nil when KeySecurity unset
			},
		},
		{
			name: "tcp-tls",
			extra: map[string]any{
				KeyTransport: "tcp",
				KeySecurity:  "tls",
				KeyCertFile:  "/tmp/cert.pem",
				KeyKeyFile:   "/tmp/key.pem",
				KeySNI:       "vless-tcp.example",
			},
			want: want{
				transportKind: contracts.TransportTCP,
				securityKind:  contracts.SecurityTLS,
				checkTLS: func(t *testing.T, tls *TLSSpec) {
					if tls.SNI != "vless-tcp.example" {
						t.Errorf("SNI = %q", tls.SNI)
					}
					if tls.CertFile != "/tmp/cert.pem" || tls.KeyFile != "/tmp/key.pem" {
						t.Errorf("cert path mismatch: %+v", tls)
					}
				},
			},
		},
		{
			name: "tcp-reality-with-flow",
			extra: map[string]any{
				KeyTransport:          "tcp",
				KeySecurity:           "reality",
				KeyFlow:               "xtls-rprx-vision",
				KeyRealityTarget:      "www.microsoft.com:443",
				KeyRealityServerNames: []string{"www.microsoft.com"},
				KeyRealityShortIDs:    []string{testRealityShortID1},
				KeyRealityPrivateKey:  testRealityPriv,
				KeyRealityPublicKey:   testRealityPub,
			},
			want: want{
				transportKind: contracts.TransportTCP,
				securityKind:  contracts.SecurityReality,
				checkVLESS: func(t *testing.T, p *VLESSParams) {
					if p.Flow != "xtls-rprx-vision" {
						t.Errorf("flow = %q", p.Flow)
					}
				},
				checkReality: func(t *testing.T, r *RealitySpec) {
					if r.Target != "www.microsoft.com:443" {
						t.Errorf("target = %q", r.Target)
					}
					if r.PrivateKey != testRealityPriv {
						t.Errorf("caller-supplied priv was overwritten: got %q want %q", r.PrivateKey, testRealityPriv)
					}
					if len(r.ShortIDs) != 1 || r.ShortIDs[0] != testRealityShortID1 {
						t.Errorf("ShortIDs = %v", r.ShortIDs)
					}
				},
			},
		},
		{
			name: "ws-tls",
			extra: map[string]any{
				KeyTransport: "ws",
				KeyWSPath:    "/vless-ws",
				KeyWSHost:    "cdn.example.com",
				KeySecurity:  "tls",
				KeyCertFile:  "/tmp/cert.pem",
				KeyKeyFile:   "/tmp/key.pem",
			},
			want: want{
				transportKind: contracts.TransportWS,
				securityKind:  contracts.SecurityTLS,
			},
		},
		{
			name: "grpc-reality-autogen-keys-and-sid",
			extra: map[string]any{
				KeyTransport:          "grpc",
				KeyGRPCServiceName:    "TunnelService",
				KeySecurity:           "reality",
				KeyRealityTarget:      "example.com:443",
				KeyRealityServerNames: []string{"example.com"},
				// No short_ids → parseReality auto-generates a 16-hex one.
				// No key pair → parseReality auto-generates a new X25519 pair.
			},
			want: want{
				transportKind: contracts.TransportGRPC,
				securityKind:  contracts.SecurityReality,
				checkReality: func(t *testing.T, r *RealitySpec) {
					if len(r.ShortIDs) != 1 {
						t.Fatalf("ShortIDs auto-fill expected 1 entry, got %d", len(r.ShortIDs))
					}
					if err := ValidateRealityShortID(r.ShortIDs[0]); err != nil {
						t.Errorf("auto-generated shortId invalid: %v", err)
					}
					if err := ValidateRealityBase64Key(r.PrivateKey); err != nil {
						t.Errorf("auto-generated priv invalid: %v", err)
					}
					if err := ValidateRealityBase64Key(r.PublicKey); err != nil {
						t.Errorf("auto-generated pub invalid: %v", err)
					}
				},
			},
		},
		{
			name: "xhttp-tls",
			extra: map[string]any{
				KeyTransport: "xhttp",
				KeyXHTTPPath: "/xh",
				KeyXHTTPMode: "auto",
				KeySecurity:  "tls",
				KeyCertFile:  "/tmp/cert.pem",
				KeyKeyFile:   "/tmp/key.pem",
			},
			want: want{
				transportKind: contracts.TransportXHTTP,
				securityKind:  contracts.SecurityTLS,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := mergeMaps(baseRaw, tc.extra)
			pp, err := Parse(raw)
			if err != nil {
				t.Fatalf("Parse err = %v", err)
			}
			if pp.Protocol != contracts.ProtocolVLess {
				t.Errorf("protocol = %q", pp.Protocol)
			}
			if pp.VLESS == nil {
				t.Fatal("VLESS sub-spec nil")
			}
			if pp.VLESS.UUID != "11111111-2222-3333-4444-555555555555" {
				t.Errorf("UUID = %q", pp.VLESS.UUID)
			}
			if pp.VLESS.Decryption != "none" {
				t.Errorf("Decryption = %q, want 'none'", pp.VLESS.Decryption)
			}
			if pp.Transport == nil {
				t.Fatal("Transport nil")
			}
			if pp.Transport.Kind != tc.want.transportKind {
				t.Errorf("Transport.Kind = %q, want %q", pp.Transport.Kind, tc.want.transportKind)
			}
			if tc.want.securityKind == "" {
				if pp.Security != nil {
					t.Errorf("Security should be nil (no KeySecurity), got %+v", pp.Security)
				}
			} else {
				if pp.Security == nil {
					t.Fatalf("Security nil, want Kind=%q", tc.want.securityKind)
				}
				if pp.Security.Kind != tc.want.securityKind {
					t.Errorf("Security.Kind = %q", pp.Security.Kind)
				}
				if tc.want.checkTLS != nil {
					if pp.Security.TLS == nil {
						t.Fatal("TLS sub-spec nil")
					}
					tc.want.checkTLS(t, pp.Security.TLS)
				}
				if tc.want.checkReality != nil {
					if pp.Security.Reality == nil {
						t.Fatal("Reality sub-spec nil")
					}
					tc.want.checkReality(t, pp.Security.Reality)
				}
			}
			if tc.want.checkVLESS != nil {
				tc.want.checkVLESS(t, pp.VLESS)
			}
		})
	}
}

func TestParseVLESSRequiresUUID(t *testing.T) {
	_, err := Parse(map[string]any{
		KeyProtocol: "vless",
		KeyPort:     uint32(443),
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

// TestParseVLESSRejectsUnsupportedTransports locks the parseVLESS gate
// for mihomo-unsupported transports (P1-4 from review): rejecting early
// gives the caller a pointed error instead of letting profilegen fail
// later with a generic message. If mihomo's VlessOption gains these
// fields, move the check to an adapter-level validator.
func TestParseVLESSRejectsUnsupportedTransports(t *testing.T) {
	for _, kind := range []string{"httpupgrade", "h2"} {
		t.Run(kind, func(t *testing.T) {
			_, err := Parse(map[string]any{
				KeyProtocol:  "vless",
				KeyPort:      uint32(443),
				KeyUUID:      "abc",
				KeyTransport: kind,
			})
			if !errors.Is(err, ErrInvalidCombination) {
				t.Errorf("err = %v, want ErrInvalidCombination", err)
			}
		})
	}
}

func TestParseVLESSRejectsBadMaxTimeDiff(t *testing.T) {
	_, err := Parse(map[string]any{
		KeyProtocol:           "vless",
		KeyPort:               uint32(443),
		KeyUUID:               "abc",
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityMaxTimeDiff: "soon",
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired (max_time_diff must be integer)", err)
	}
}

func TestParseVLESSAcceptsGoodMaxTimeDiff(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol:           "vless",
		KeyPort:               uint32(443),
		KeyUUID:               "abc",
		KeySecurity:           "reality",
		KeyRealityTarget:      "example.com:443",
		KeyRealityServerNames: []string{"example.com"},
		KeyRealityMaxTimeDiff: "60",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.Security.Reality.MaxTimeDiff != "60" {
		t.Errorf("MaxTimeDiff lost: %q", pp.Security.Reality.MaxTimeDiff)
	}
}

func TestParseVLESSDefaultsDecryption(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vless",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VLESS.Decryption != "none" {
		t.Errorf("Decryption default = %q, want 'none'", pp.VLESS.Decryption)
	}
}

func TestParseVLESSListenAddrDefaults(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vless",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.ListenAddr != "127.0.0.1" {
		t.Errorf("ListenAddr = %q, want 127.0.0.1", pp.ListenAddr)
	}
}

func TestParseVLESSCustomListenAddr(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol:   "vless",
		KeyPort:       uint32(443),
		KeyUUID:       "abc",
		KeyListenAddr: "0.0.0.0",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.ListenAddr != "0.0.0.0" {
		t.Errorf("ListenAddr = %q, want 0.0.0.0", pp.ListenAddr)
	}
}

// mergeMaps returns a new map containing all entries of a and b; b wins on
// key collision.
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// TestParseVLESSPreservesInputShape is a belt-and-braces check that the
// VLESS branch, now that it's a real implementation, still doesn't mutate
// the caller's map. Complements the stub-era integration_test.
func TestParseVLESSPreservesInputShape(t *testing.T) {
	raw := map[string]any{
		KeyProtocol: "vless",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
		KeySecurity: "tls",
		KeyCertFile: "/tmp/cert.pem",
		KeyKeyFile:  "/tmp/key.pem",
	}
	before := make(map[string]any, len(raw))
	for k, v := range raw {
		before[k] = v
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse err = %v", err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Errorf("Parse mutated raw: before=%+v after=%+v", before, raw)
	}
}
