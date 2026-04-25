package protocolparams

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestParseTrojanMatrix(t *testing.T) {
	baseRaw := map[string]any{
		KeyProtocol: "trojan",
		KeyPort:     uint32(443),
		KeyPassword: "trojan-pass",
	}

	cases := []struct {
		name          string
		extra         map[string]any
		wantTransport contracts.Transport
		wantSecurity  contracts.Security
	}{
		{
			name:          "tcp-default-tls",
			extra:         map[string]any{},
			wantTransport: contracts.TransportTCP,
			wantSecurity:  contracts.SecurityTLS,
		},
		{
			name: "ws-tls",
			extra: map[string]any{
				KeyTransport:  "ws",
				KeyWSPath:     "/trojan-ws",
				KeyWSHost:     "cdn.example.com",
				KeySecurity:   "tls",
				KeyCertFile:   "/tmp/cert.pem",
				KeyKeyFile:    "/tmp/key.pem",
				KeyServerName: "ignored-by-parser",
				KeySNI:        "trojan.example",
			},
			wantTransport: contracts.TransportWS,
			wantSecurity:  contracts.SecurityTLS,
		},
		{
			name: "grpc-reality",
			extra: map[string]any{
				KeyTransport:          "grpc",
				KeyGRPCServiceName:    "TrojanTunnel",
				KeySecurity:           "reality",
				KeyRealityTarget:      "www.microsoft.com:443",
				KeyRealityServerNames: []string{"www.microsoft.com"},
				KeyRealityShortIDs:    []string{testRealityShortID1},
				KeyRealityPrivateKey:  testRealityPriv,
				KeyRealityPublicKey:   testRealityPub,
			},
			wantTransport: contracts.TransportGRPC,
			wantSecurity:  contracts.SecurityReality,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pp, err := Parse(mergeMaps(baseRaw, tc.extra))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if pp.Protocol != contracts.ProtocolTrojan {
				t.Errorf("Protocol = %q", pp.Protocol)
			}
			if pp.Trojan == nil || pp.Trojan.Password != "trojan-pass" {
				t.Fatalf("Trojan params = %+v", pp.Trojan)
			}
			if pp.Transport == nil || pp.Transport.Kind != tc.wantTransport {
				t.Errorf("Transport = %+v, want %q", pp.Transport, tc.wantTransport)
			}
			if pp.Security == nil || pp.Security.Kind != tc.wantSecurity {
				t.Fatalf("Security = %+v, want %q", pp.Security, tc.wantSecurity)
			}
			if tc.wantSecurity == contracts.SecurityTLS && pp.Security.TLS == nil {
				t.Fatal("TLS spec nil")
			}
			if tc.name == "ws-tls" && pp.Security.TLS.SNI != "trojan.example" {
				t.Errorf("TLS.SNI = %q, want trojan.example", pp.Security.TLS.SNI)
			}
			if tc.wantSecurity == contracts.SecurityReality && pp.Security.Reality == nil {
				t.Fatal("Reality spec nil")
			}
		})
	}
}

func TestParseTrojanRequiresPassword(t *testing.T) {
	_, err := Parse(map[string]any{
		KeyProtocol: "trojan",
		KeyPort:     uint32(443),
	})
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

func TestParseTrojanRejectsSecurityNone(t *testing.T) {
	_, err := Parse(map[string]any{
		KeyProtocol: "trojan",
		KeyPort:     uint32(443),
		KeyPassword: "p",
		KeySecurity: "none",
	})
	if !errors.Is(err, ErrInvalidCombination) {
		t.Errorf("err = %v, want ErrInvalidCombination", err)
	}
}

func TestParseTrojanRejectsUnsupportedTransports(t *testing.T) {
	for _, kind := range []string{"httpupgrade", "xhttp", "splithttp", "h2"} {
		t.Run(kind, func(t *testing.T) {
			_, err := Parse(map[string]any{
				KeyProtocol:  "trojan",
				KeyPort:      uint32(443),
				KeyPassword:  "p",
				KeyTransport: kind,
			})
			if !errors.Is(err, ErrInvalidCombination) {
				t.Errorf("%s err = %v, want ErrInvalidCombination", kind, err)
			}
		})
	}
}

func TestParseTrojanPreservesInputShape(t *testing.T) {
	raw := map[string]any{
		KeyProtocol:  "trojan",
		KeyPort:      uint32(443),
		KeyPassword:  "p",
		KeyTransport: "ws",
		KeyWSPath:    "/ws",
	}
	before := make(map[string]any, len(raw))
	for k, v := range raw {
		before[k] = v
	}
	if _, err := Parse(raw); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !reflect.DeepEqual(raw, before) {
		t.Fatalf("Parse mutated raw map: before=%v after=%v", before, raw)
	}
}
