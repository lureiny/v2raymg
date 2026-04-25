package mihomo

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

// vmessInbound is the VMess analogue of vlessInbound — starts from a bare
// ProtocolParams and lets each sub-test configure the transport/security
// it cares about.
func vmessInbound(tag string, port uint32, mutate func(*protocolparams.ProtocolParams)) *MihomoInbound {
	pp := &protocolparams.ProtocolParams{
		Protocol: contracts.ProtocolVMess,
		Tag:      tag,
		Port:     port,
		VMess: &protocolparams.VMessParams{
			UUID: "vmess-uuid-1",
		},
	}
	if mutate != nil {
		mutate(pp)
	}
	return NewMihomoInboundFromProtocolParams(pp)
}

func TestBuildListener_VMess_TCP_Plain(t *testing.T) {
	inb := vmessInbound("vmess-a", 20101, nil)
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":   "vmess-a",
		"type":   "vmess",
		"listen": "127.0.0.1",
		"port":   "20101",
		"users":  []map[string]any{{"uuid": "vmess-uuid-1", "alterId": 0}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("vmess tcp-plain mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestBuildListener_VMess_TCP_TLS(t *testing.T) {
	inb := vmessInbound("vmess-tcp-tls", 20102, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportTCP}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS: &protocolparams.TLSSpec{
				CertFile: "/tmp/cert.pem",
				KeyFile:  "/tmp/key.pem",
				SNI:      "vmess.example",
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
	// mihomo VmessOption has no standalone network field; tcp-tls must not
	// emit a transport-specific key.
	if _, hasWS := got["ws-path"]; hasWS {
		t.Error("tcp transport should not set ws-path")
	}
	if _, hasGRPC := got["grpc-service-name"]; hasGRPC {
		t.Error("tcp transport should not set grpc-service-name")
	}
}

func TestBuildListener_VMess_TCP_Reality(t *testing.T) {
	inb := vmessInbound("vmess-reality", 20103, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportTCP}
		pp.Security = &protocolparams.SecuritySpec{
			Kind: contracts.SecurityReality,
			Reality: &protocolparams.RealitySpec{
				Target:      "www.microsoft.com:443",
				ServerNames: []string{"www.microsoft.com"},
				ShortIDs:    []string{"aabbccddeeff0011"},
				PrivateKey:  "REALITY-PRIV",
			},
		}
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	rc, ok := got["reality-config"].(map[string]any)
	if !ok {
		t.Fatalf("reality-config missing or wrong type: %#v", got)
	}
	if rc["dest"] != "www.microsoft.com:443" {
		t.Errorf("reality dest = %v", rc["dest"])
	}
	if rc["private-key"] != "REALITY-PRIV" {
		t.Errorf("reality private-key = %v", rc["private-key"])
	}
	if sids, _ := rc["short-id"].([]string); !reflect.DeepEqual(sids, []string{"aabbccddeeff0011"}) {
		t.Errorf("short-id = %v", rc["short-id"])
	}
}

func TestBuildListener_VMess_WS_TLS(t *testing.T) {
	inb := vmessInbound("vmess-ws-tls", 20104, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:   contracts.TransportWS,
			WSPath: "/vmess-ws",
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
	if got["ws-path"] != "/vmess-ws" {
		t.Errorf("ws-path = %v", got["ws-path"])
	}
	if got["certificate"] != "/tmp/cert.pem" {
		t.Errorf("certificate missing")
	}
}

func TestBuildListener_VMess_GRPC_TLS(t *testing.T) {
	inb := vmessInbound("vmess-grpc-tls", 20105, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:            contracts.TransportGRPC,
			GRPCServiceName: "VmessTunnel",
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
	if got["grpc-service-name"] != "VmessTunnel" {
		t.Errorf("grpc-service-name = %v", got["grpc-service-name"])
	}
}

func TestBuildListener_VMess_GRPC_Reality(t *testing.T) {
	inb := vmessInbound("vmess-grpc-reality", 20106, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{
			Kind:            contracts.TransportGRPC,
			GRPCServiceName: "VmessTunnel",
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
	if got["grpc-service-name"] != "VmessTunnel" {
		t.Errorf("grpc-service-name missing: %#v", got)
	}
	if _, ok := got["reality-config"]; !ok {
		t.Errorf("reality-config missing: %#v", got)
	}
}

func TestBuildListener_VMess_UnsupportedTransport(t *testing.T) {
	// parseVMess pre-rejects xhttp / httpupgrade / h2, so the profilegen
	// default branch only fires for newly-added transport constants that
	// forgot to wire in the mihomo emit path. A contracts.TransportMKCP
	// (already in contracts but explicitly not FastAddable) stands in.
	inb := vmessInbound("vmess-bogus", 20107, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportMKCP}
	})
	_, err := BuildListener(inb)
	if err == nil {
		t.Fatal("BuildListener accepted unsupported transport (mkcp)")
	}
}

// TestBuildListener_VMess_LegacySharedCred confirms records written before
// Phase 2 (ProtocolParams=nil) still produce the exact same yaml the old
// profilegen emitted — users[{uuid, alterId:0}] and nothing else.
func TestBuildListener_VMess_LegacySharedCred(t *testing.T) {
	inb := NewMihomoInbound("vmess-legacy", contracts.ProtocolVMess, 20108, MihomoSharedCred{UUID: "legacy-uuid"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":   "vmess-legacy",
		"type":   "vmess",
		"listen": "127.0.0.1",
		"port":   "20108",
		"users":  []map[string]any{{"uuid": "legacy-uuid", "alterId": 0}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("legacy SharedCred output diverged:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestMihomoInboundPersistence_VMess_RoundTrip(t *testing.T) {
	orig := vmessInbound("vmess-persist", 20109, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportWS, WSPath: "/vm"}
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
	if back.Protocol() != contracts.ProtocolVMess {
		t.Errorf("protocol lost after round-trip")
	}
	if back.ProtocolParams == nil || back.ProtocolParams.VMess == nil {
		t.Fatal("ProtocolParams not restored")
	}
	if back.ProtocolParams.VMess.UUID != "vmess-uuid-1" {
		t.Errorf("uuid lost: %q", back.ProtocolParams.VMess.UUID)
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

func TestBuildListener_VMess_MissingUUID_Rejected(t *testing.T) {
	pp := &protocolparams.ProtocolParams{
		Protocol: contracts.ProtocolVMess,
		Tag:      "vmess-bad",
		Port:     20110,
		VMess:    &protocolparams.VMessParams{},
	}
	inb := NewMihomoInboundFromProtocolParams(pp)
	_, err := BuildListener(inb)
	if err == nil {
		t.Fatal("BuildListener accepted vmess without uuid")
	}
}
