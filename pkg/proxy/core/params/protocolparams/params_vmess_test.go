package protocolparams

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestParseVMessMatrix drives the transport × security combinations mihomo
// VmessOption actually supports (tcp / ws / grpc × none / tls / reality).
// httpupgrade / xhttp / splithttp / h2 rejection lives in a separate test.
func TestParseVMessMatrix(t *testing.T) {
	baseRaw := map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
		KeyUUID:     "11111111-2222-3333-4444-555555555555",
	}

	type want struct {
		transportKind contracts.Transport
		securityKind  contracts.Security
		checkVMess    func(t *testing.T, p *VMessParams)
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
				transportKind: contracts.TransportTCP,
				securityKind:  "", // security nil when KeySecurity unset
			},
		},
		{
			name: "tcp-tls",
			extra: map[string]any{
				KeyTransport: "tcp",
				KeySecurity:  "tls",
				KeyCertFile:  "/tmp/cert.pem",
				KeyKeyFile:   "/tmp/key.pem",
				KeySNI:       "vmess-tcp.example",
			},
			want: want{
				transportKind: contracts.TransportTCP,
				securityKind:  contracts.SecurityTLS,
				checkTLS: func(t *testing.T, tls *TLSSpec) {
					if tls.SNI != "vmess-tcp.example" {
						t.Errorf("SNI = %q", tls.SNI)
					}
					if tls.CertFile != "/tmp/cert.pem" || tls.KeyFile != "/tmp/key.pem" {
						t.Errorf("cert path mismatch: %+v", tls)
					}
				},
			},
		},
		{
			name: "tcp-reality",
			extra: map[string]any{
				KeyTransport:          "tcp",
				KeySecurity:           "reality",
				KeyRealityTarget:      "www.microsoft.com:443",
				KeyRealityServerNames: []string{"www.microsoft.com"},
				KeyRealityShortIDs:    []string{testRealityShortID1},
				KeyRealityPrivateKey:  testRealityPriv,
				KeyRealityPublicKey:   testRealityPub,
			},
			want: want{
				transportKind: contracts.TransportTCP,
				securityKind:  contracts.SecurityReality,
				checkReality: func(t *testing.T, r *RealitySpec) {
					if r.Target != "www.microsoft.com:443" {
						t.Errorf("target = %q", r.Target)
					}
					if r.PrivateKey != testRealityPriv {
						t.Errorf("priv overwritten: got %q want %q", r.PrivateKey, testRealityPriv)
					}
				},
			},
		},
		{
			name: "ws-tls",
			extra: map[string]any{
				KeyTransport: "ws",
				KeyWSPath:    "/vmess-ws",
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
			name: "grpc-tls",
			extra: map[string]any{
				KeyTransport:       "grpc",
				KeyGRPCServiceName: "VmessTunnel",
				KeySecurity:        "tls",
				KeyCertFile:        "/tmp/cert.pem",
				KeyKeyFile:         "/tmp/key.pem",
			},
			want: want{
				transportKind: contracts.TransportGRPC,
				securityKind:  contracts.SecurityTLS,
			},
		},
		{
			name: "grpc-reality-autogen-keys",
			extra: map[string]any{
				KeyTransport:          "grpc",
				KeyGRPCServiceName:    "VmessTunnel",
				KeySecurity:           "reality",
				KeyRealityTarget:      "example.com:443",
				KeyRealityServerNames: []string{"example.com"},
				// No short_ids / no keys → parseReality auto-fills (xray parity)
			},
			want: want{
				transportKind: contracts.TransportGRPC,
				securityKind:  contracts.SecurityReality,
				checkReality: func(t *testing.T, r *RealitySpec) {
					if len(r.ShortIDs) != 1 {
						t.Fatalf("autofill ShortIDs count = %d", len(r.ShortIDs))
					}
					if err := ValidateRealityShortID(r.ShortIDs[0]); err != nil {
						t.Errorf("autofilled shortId invalid: %v", err)
					}
					if err := ValidateRealityBase64Key(r.PrivateKey); err != nil {
						t.Errorf("autofilled priv invalid: %v", err)
					}
					if err := ValidateRealityBase64Key(r.PublicKey); err != nil {
						t.Errorf("autofilled pub invalid: %v", err)
					}
				},
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
			if pp.Protocol != contracts.ProtocolVMess {
				t.Errorf("protocol = %q", pp.Protocol)
			}
			if pp.VMess == nil {
				t.Fatal("VMess sub-spec nil")
			}
			if pp.VMess.UUID != "11111111-2222-3333-4444-555555555555" {
				t.Errorf("UUID = %q", pp.VMess.UUID)
			}
			if pp.Transport == nil {
				t.Fatal("Transport nil")
			}
			if pp.Transport.Kind != tc.want.transportKind {
				t.Errorf("Transport.Kind = %q, want %q", pp.Transport.Kind, tc.want.transportKind)
			}
			if tc.want.securityKind == "" {
				if pp.Security != nil {
					t.Errorf("Security should be nil, got %+v", pp.Security)
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
			if tc.want.checkVMess != nil {
				tc.want.checkVMess(t, pp.VMess)
			}
		})
	}
}

func TestParseVMessRequiresUUID(t *testing.T) {
	_, err := Parse(map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

// TestParseVMessRejectsUnsupportedTransports locks the parseVMess gate
// against mihomo-unsupported transports. mihomo Alpha VmessOption only
// has ws-path and grpc-service-name; httpupgrade / xhttp / splithttp /
// h2 are never valid. Give the caller a pointed error rather than
// letting profilegen's default branch fail later.
func TestParseVMessRejectsUnsupportedTransports(t *testing.T) {
	for _, kind := range []string{"httpupgrade", "xhttp", "splithttp", "h2"} {
		t.Run(kind, func(t *testing.T) {
			_, err := Parse(map[string]any{
				KeyProtocol:  "vmess",
				KeyPort:      uint32(443),
				KeyUUID:      "abc",
				KeyTransport: kind,
			})
			if !errors.Is(err, ErrInvalidCombination) {
				t.Errorf("%s err = %v, want ErrInvalidCombination", kind, err)
			}
		})
	}
}

// TestParseVMessAlterIDDefault guards the xray-parity default: absent
// alter_id becomes 0, the implicit legacy value both xray buildSettings
// and mihomo VmessUser treat as "AEAD only".
func TestParseVMessAlterIDDefault(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VMess.AlterID != 0 {
		t.Errorf("AlterID default = %d, want 0", pp.VMess.AlterID)
	}
}

func TestParseVMessAlterIDExplicit(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
		KeyAlterID:  32,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VMess.AlterID != 32 {
		t.Errorf("AlterID = %d, want 32", pp.VMess.AlterID)
	}
}

// TestParseVMessCipherNoDefault locks the cipher-is-caller-only contract:
// neither xray nor mihomo VMess listeners accept a server-side cipher;
// VMess URIs carry no cipher; the clash converter hardcodes "auto" on
// the client side. Letting parseVMess fill a "default" would diverge
// from xray's behaviour, so Cipher MUST remain empty when unspecified.
func TestParseVMessCipherNoDefault(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VMess.Cipher != "" {
		t.Errorf("Cipher default = %q, want empty (xray parity)", pp.VMess.Cipher)
	}
}

func TestParseVMessCipherExplicit(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vmess",
		KeyPort:     uint32(443),
		KeyUUID:     "abc",
		KeyCipher:   "zero",
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pp.VMess.Cipher != "zero" {
		t.Errorf("Cipher = %q, want 'zero'", pp.VMess.Cipher)
	}
}

func TestParseVMessListenAddrDefaults(t *testing.T) {
	pp, err := Parse(map[string]any{
		KeyProtocol: "vmess",
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

// TestParseVMessPreservesInputShape complements the all-protocols
// integration_test check; the VMess branch is now a real implementation
// and must still leave the caller's map untouched.
func TestParseVMessPreservesInputShape(t *testing.T) {
	raw := map[string]any{
		KeyProtocol:  "vmess",
		KeyPort:      uint32(443),
		KeyUUID:      "abc",
		KeyTransport: "ws",
		KeyWSPath:    "/vm",
		KeySecurity:  "tls",
		KeyCertFile:  "/tmp/cert.pem",
		KeyKeyFile:   "/tmp/key.pem",
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
