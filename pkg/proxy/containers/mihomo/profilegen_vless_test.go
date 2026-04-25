package mihomo

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

// vlessInbound is a tiny builder shared across the matrix. Each sub-test
// starts from a minimum ProtocolParams{VLESS:{UUID}} and mutates the
// copied pp to configure transport/security.
func vlessInbound(tag string, port uint32, mutate func(*protocolparams.ProtocolParams)) *MihomoInbound {
	pp := &protocolparams.ProtocolParams{
		Protocol: contracts.ProtocolVLess,
		Tag:      tag,
		Port:     port,
		VLESS: &protocolparams.VLESSParams{
			UUID:       "vless-uuid-1",
			Decryption: "none",
		},
	}
	if mutate != nil {
		mutate(pp)
	}
	return NewMihomoInboundFromProtocolParams(pp)
}

func TestBuildListener_VLess_TCP_Plain(t *testing.T) {
	inb := vlessInbound("vless-a", 10101, nil)
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":       "vless-a",
		"type":       "vless",
		"listen":     "127.0.0.1",
		"port":       "10101",
		"users":      []map[string]any{{"uuid": "vless-uuid-1"}},
		"decryption": "none",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vless tcp-plain mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestBuildListener_VLess_TCP_TLS(t *testing.T) {
	inb := vlessInbound("vless-tcp-tls", 10102, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportTCP}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS: &protocolparams.TLSSpec{
				CertFile: "/tmp/cert.pem",
				KeyFile:  "/tmp/key.pem",
				SNI:      "vless.example",
			},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if got["certificate"] != "/tmp/cert.pem" || got["private-key"] != "/tmp/key.pem" {
		t.Errorf("cert/key fields not populated: %#v", got)
	}
	if _, hasNetwork := got["ws-path"]; hasNetwork {
		t.Error("tcp transport should not set ws-path")
	}
}

func TestBuildListener_VLess_TCP_Reality(t *testing.T) {
	inb := vlessInbound("vless-reality", 10103, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportTCP}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityReality,
			Reality: &protocolparams.RealitySpec{
				Target:      "www.microsoft.com:443",
				ServerNames: []string{"www.microsoft.com"},
				ShortIDs:    []string{"aa", "bb"},
				PrivateKey:  "REALITY-PRIV",
			},
		}
		pp.VLESS.Flow = "xtls-rprx-vision"
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	// Flow in users
	users, _ := got["users"].([]map[string]any)
	if len(users) != 1 || users[0]["flow"] != "xtls-rprx-vision" {
		t.Errorf("users[0].flow not set: %#v", users)
	}
	// Reality config keys match mihomo Alpha schema
	rc, ok := got["reality-config"].(map[string]any)
	if !ok {
		t.Fatalf("reality-config missing or wrong type: %#v", got)
	}
	if rc["dest"] != "www.microsoft.com:443" {
		t.Errorf("reality dest = %v, want www.microsoft.com:443", rc["dest"])
	}
	serverNames, _ := rc["server-names"].([]string)
	if len(serverNames) != 1 || serverNames[0] != "www.microsoft.com" {
		t.Errorf("server-names = %v", rc["server-names"])
	}
	shortIDs, _ := rc["short-id"].([]string)
	if !reflect.DeepEqual(shortIDs, []string{"aa", "bb"}) {
		t.Errorf("short-id = %v", rc["short-id"])
	}
	if rc["private-key"] != "REALITY-PRIV" {
		t.Errorf("reality private-key = %v", rc["private-key"])
	}
}

func TestBuildListener_VLess_WS_TLS(t *testing.T) {
	inb := vlessInbound("vless-ws-tls", 10104, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:   contracts.TransportWS,
			WSPath: "/vless-ws",
			WSHost: "cdn.example.com",
		}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS: &protocolparams.TLSSpec{
				CertFile: "/tmp/cert.pem",
				KeyFile:  "/tmp/key.pem",
			},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if got["ws-path"] != "/vless-ws" {
		t.Errorf("ws-path = %v", got["ws-path"])
	}
}

func TestBuildListener_VLess_GRPC_TLS(t *testing.T) {
	inb := vlessInbound("vless-grpc-tls", 10105, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:            contracts.TransportGRPC,
			GRPCServiceName: "TunnelService",
		}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS:  &protocolparams.TLSSpec{CertFile: "/tmp/cert.pem", KeyFile: "/tmp/key.pem"},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if got["grpc-service-name"] != "TunnelService" {
		t.Errorf("grpc-service-name = %v", got["grpc-service-name"])
	}
}

func TestBuildListener_VLess_GRPC_Reality(t *testing.T) {
	inb := vlessInbound("vless-grpc-reality", 10106, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:            contracts.TransportGRPC,
			GRPCServiceName: "TunnelService",
		}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityReality,
			Reality: &protocolparams.RealitySpec{
				Target:      "example.com:443",
				ServerNames: []string{"example.com"},
				PrivateKey:  "PRIV",
			},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if got["grpc-service-name"] != "TunnelService" {
		t.Errorf("grpc-service-name missing: %#v", got)
	}
	if _, ok := got["reality-config"]; !ok {
		t.Errorf("reality-config missing: %#v", got)
	}
}

func TestBuildListener_VLess_XHTTP_TLS(t *testing.T) {
	inb := vlessInbound("vless-xhttp-tls", 10107, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:      contracts.TransportXHTTP,
			XHTTPPath: "/xh",
			XHTTPMode: "auto",
		}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS:  &protocolparams.TLSSpec{CertFile: "/tmp/cert.pem", KeyFile: "/tmp/key.pem"},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	xh, ok := got["xhttp-config"].(map[string]any)
	if !ok {
		t.Fatalf("xhttp-config missing or wrong type: %#v", got)
	}
	if xh["path"] != "/xh" || xh["mode"] != "auto" {
		t.Errorf("xhttp-config mismatch: %#v", xh)
	}
}

func TestBuildListener_VLess_UnsupportedTransport(t *testing.T) {
	inb := vlessInbound("vless-bogus", 10108, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportMKCP}
	})
	// Validate passes (Validate doesn't gate transport kind), but
	// BuildListener must reject unsupported transports before we hand yaml
	// to mihomo.
	_, err := BuildListener(inb)
	if err == nil {
		t.Fatal("BuildListener accepted unsupported transport (mkcp)")
	}
}

func TestMihomoInboundPersistence_VLess_RoundTrip(t *testing.T) {
	orig := vlessInbound("vless-persist", 10109, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportWS, WSPath: "/p"}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS:  &protocolparams.TLSSpec{CertFile: "/tmp/cert.pem", KeyFile: "/tmp/key.pem", SNI: "x.example"},
		}
	})
	data, err := orig.ToNative()
	if err != nil {
		t.Fatalf("ToNative: %v", err)
	}
	back, err := FromNative(data)
	if err != nil {
		t.Fatalf("FromNative: %v", err)
	}
	if back.Protocol() != contracts.ProtocolVLess {
		t.Errorf("protocol lost after round-trip")
	}
	if back.ProtocolParams == nil || back.ProtocolParams.VLESS == nil {
		t.Fatal("ProtocolParams not restored")
	}
	if back.ProtocolParams.VLESS.UUID != "vless-uuid-1" {
		t.Errorf("uuid lost: %q", back.ProtocolParams.VLESS.UUID)
	}
	if back.ProtocolParams.Transport == nil || back.ProtocolParams.Transport.Kind != contracts.TransportWS {
		t.Errorf("transport not restored: %+v", back.ProtocolParams.Transport)
	}
	if back.ProtocolParams.Security == nil || back.ProtocolParams.Security.Kind != contracts.SecurityTLS {
		t.Errorf("security not restored")
	}
	if back.ProtocolParams.Security.TLS.SNI != "x.example" {
		t.Errorf("TLS.SNI lost: %q", back.ProtocolParams.Security.TLS.SNI)
	}
}

func TestBuildListener_VLess_MissingUUID_Rejected(t *testing.T) {
	pp := &protocolparams.ProtocolParams{
		Protocol: contracts.ProtocolVLess,
		Tag:      "vless-bad",
		Port:     10110,
		VLESS:    &protocolparams.VLESSParams{},
	}
	inb := NewMihomoInboundFromProtocolParams(pp)
	_, err := BuildListener(inb)
	if err == nil {
		t.Fatal("BuildListener accepted vless without uuid")
	}
}
