package mihomo

import (
	"reflect"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

func trojanInbound(tag string, port uint32, mutate func(*protocolparams.ProtocolParams)) *MihomoInbound {
	pp := &protocolparams.ProtocolParams{
		Protocol: contracts.ProtocolTrojan,
		Tag:      tag,
		Port:     port,
		Transport: &protocolparams.TransportSpec{
			Kind: contracts.TransportTCP,
		},
		Security: &protocolparams.SecuritySpec{
			Kind: contracts.SecurityTLS,
			TLS:  &protocolparams.TLSSpec{},
		},
		Trojan: &protocolparams.TrojanParams{
			Password: "trojan-pass",
		},
	}
	if mutate != nil {
		mutate(pp)
	}
	return NewMihomoInboundFromProtocolParams(pp)
}

func TestBuildListener_Trojan_TCP_TLS(t *testing.T) {
	inb := trojanInbound("trojan-a", 20301, func(pp *protocolparams.ProtocolParams) {
		pp.Security.TLS.CertFile = "/tmp/cert.pem"
		pp.Security.TLS.KeyFile = "/tmp/key.pem"
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":        "trojan-a",
		"type":        "trojan",
		"listen":      "127.0.0.1",
		"port":        "20301",
		"users":       []map[string]any{{"password": "trojan-pass"}},
		"certificate": "/tmp/cert.pem",
		"private-key": "/tmp/key.pem",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trojan tcp-tls mismatch:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestBuildListener_Trojan_WS_TLS(t *testing.T) {
	inb := trojanInbound("trojan-ws", 20302, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportWS, WSPath: "/trojan-ws", WSHost: "cdn.example.com"}
		pp.Security.TLS.CertFile = "/tmp/cert.pem"
		pp.Security.TLS.KeyFile = "/tmp/key.pem"
	})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	if got["ws-path"] != "/trojan-ws" {
		t.Errorf("ws-path = %v", got["ws-path"])
	}
	if got["certificate"] != "/tmp/cert.pem" {
		t.Errorf("certificate missing")
	}
}

func TestBuildListener_Trojan_GRPC_Reality(t *testing.T) {
	inb := trojanInbound("trojan-grpc-reality", 20303, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportGRPC, GRPCServiceName: "TrojanTunnel"}
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
	if got["grpc-service-name"] != "TrojanTunnel" {
		t.Errorf("grpc-service-name = %v", got["grpc-service-name"])
	}
	rc, ok := got["reality-config"].(map[string]any)
	if !ok {
		t.Fatalf("reality-config missing: %#v", got)
	}
	if rc["dest"] != "www.microsoft.com:443" {
		t.Errorf("reality dest = %v", rc["dest"])
	}
	if rc["private-key"] != "REALITY-PRIV" {
		t.Errorf("private-key = %v", rc["private-key"])
	}
}

func TestBuildListener_Trojan_LegacySharedCred(t *testing.T) {
	inb := NewMihomoInbound("trojan-legacy", contracts.ProtocolTrojan, 20304, MihomoSharedCred{Password: "legacy-pass"})
	got, err := BuildListener(inb)
	if err != nil {
		t.Fatalf("BuildListener: %v", err)
	}
	want := map[string]any{
		"name":   "trojan-legacy",
		"type":   "trojan",
		"listen": "127.0.0.1",
		"port":   "20304",
		"users":  []map[string]any{{"password": "legacy-pass"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("legacy SharedCred output diverged:\n got:  %#v\n want: %#v", got, want)
	}
}

func TestMihomoInboundPersistence_Trojan_RoundTrip(t *testing.T) {
	orig := trojanInbound("trojan-persist", 20305, func(pp *protocolparams.ProtocolParams) {
		pp.Transport = &protocolparams.TransportSpec{Kind: contracts.TransportWS, WSPath: "/tr"}
		pp.Security.TLS.CertFile = "/tmp/cert.pem"
		pp.Security.TLS.KeyFile = "/tmp/key.pem"
		pp.Security.TLS.SNI = "trojan.example"
	})
	data, err := orig.ToNative()
	if err != nil {
		t.Fatalf("ToNative: %v", err)
	}
	back, err := FromNative(data)
	if err != nil {
		t.Fatalf("FromNative: %v", err)
	}
	if back.ProtocolParams == nil || back.ProtocolParams.Trojan == nil {
		t.Fatal("ProtocolParams.Trojan not restored")
	}
	if back.ProtocolParams.Trojan.Password != "trojan-pass" {
		t.Errorf("password lost: %q", back.ProtocolParams.Trojan.Password)
	}
	if back.ProtocolParams.Transport == nil || back.ProtocolParams.Transport.Kind != contracts.TransportWS {
		t.Errorf("transport not restored: %+v", back.ProtocolParams.Transport)
	}
	if back.ProtocolParams.Security == nil || back.ProtocolParams.Security.TLS.SNI != "trojan.example" {
		t.Errorf("TLS SNI not restored")
	}
}

func TestMihomoInbound_TrojanProtocolParamsCleanupCertFiles(t *testing.T) {
	inb := trojanInbound("trojan-cleanup", 20306, func(pp *protocolparams.ProtocolParams) {
		pp.Security.TLS.CertFile = "/tmp/cert.pem"
		pp.Security.TLS.KeyFile = "/tmp/key.pem"
		pp.Security.TLS.CertSource = "self_signed"
	})
	got := inb.cleanupCertFiles()
	want := []string{"/tmp/cert.pem", "/tmp/key.pem"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cleanupCertFiles = %#v, want %#v", got, want)
	}
}
