package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

func TestGetUserSubscriptions_Trojan_WS_TLS_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "trojan-ws", map[string]any{
		"protocol":    "trojan",
		"port":        20311,
		"password":    "trojan-pass",
		"transport":   "ws",
		"ws_path":     "/trojan-ws",
		"ws_host":     "cdn.example.com",
		"security":    "tls",
		"cert_file":   "/tmp/cert.pem",
		"key_file":    "/tmp/key.pem",
		"cert_source": "self_signed",
		"sni":         "cdn.example.com",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]
	if spec.Protocol != contracts.ProtocolTrojan {
		t.Errorf("Protocol = %q, want trojan", spec.Protocol)
	}
	if spec.Password != "trojan-pass" {
		t.Errorf("Password = %q", spec.Password)
	}
	node, err := codec.DecodeTrojan(spec.URI)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if node.Transport != "ws" || node.WSPath != "/trojan-ws" || node.WSHost != "cdn.example.com" {
		t.Errorf("URI transport fields = %+v", node)
	}
	if node.SNI != "cdn.example.com" {
		t.Errorf("URI SNI = %q", node.SNI)
	}
	if !node.SkipCertVerify {
		t.Errorf("URI should set insecure=1 for self_signed cert")
	}
	if spec.Extensions["transport"] != "ws" {
		t.Errorf("Extensions[transport] = %v", spec.Extensions["transport"])
	}
	if spec.Extensions["ws_path"] != "/trojan-ws" {
		t.Errorf("Extensions[ws_path] = %v", spec.Extensions["ws_path"])
	}
	if spec.Extensions["security"] != "tls" {
		t.Errorf("Extensions[security] = %v", spec.Extensions["security"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] = %v", spec.Extensions["skip_cert_verify"])
	}
}

func TestGetUserSubscriptions_Trojan_GRPC_Reality_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "trojan-reality", map[string]any{
		"protocol":             "trojan",
		"port":                 20312,
		"password":             "trojan-pass",
		"transport":            "grpc",
		"grpc_service_name":    "TrojanTunnel",
		"security":             "reality",
		"reality_target":       "www.microsoft.com:443",
		"reality_server_names": []string{"www.microsoft.com"},
		"reality_short_ids":    []string{subTestShortID, subTestShortIDAlt},
		"reality_private_key":  subTestRealityPriv,
		"reality_public_key":   subTestRealityPub,
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs", len(specs))
	}
	spec := specs[0]
	node, err := codec.DecodeTrojan(spec.URI)
	if err != nil {
		t.Fatalf("DecodeTrojan: %v", err)
	}
	if node.Security != "reality" {
		t.Errorf("URI Security = %q", node.Security)
	}
	if node.RealityPublicKey != subTestRealityPub {
		t.Errorf("URI pbk = %q", node.RealityPublicKey)
	}
	if node.RealityShortID != subTestShortID {
		t.Errorf("URI sid = %q", node.RealityShortID)
	}
	if node.Transport != "grpc" || node.GRPCServiceName != "TrojanTunnel" {
		t.Errorf("URI grpc fields = %+v", node)
	}
	if spec.Extensions["security"] != "reality" {
		t.Errorf("Extensions[security] = %v", spec.Extensions["security"])
	}
	if spec.Extensions["reality_public_key"] != subTestRealityPub {
		t.Errorf("Extensions[reality_public_key] = %v", spec.Extensions["reality_public_key"])
	}
	sids, _ := spec.Extensions["reality_short_ids"].([]string)
	if len(sids) == 0 || sids[0] != subTestShortID {
		t.Errorf("Extensions[reality_short_ids] = %v", spec.Extensions["reality_short_ids"])
	}
	if spec.Extensions["grpc_service_name"] != "TrojanTunnel" {
		t.Errorf("Extensions[grpc_service_name] = %v", spec.Extensions["grpc_service_name"])
	}
}

func TestGetUserSubscriptions_Trojan_LegacySharedCred(t *testing.T) {
	spec, err := buildSubscriptionSpec(
		NewMihomoInbound("trojan-legacy", contracts.ProtocolTrojan, 20313, MihomoSharedCred{Password: "legacy-pass"}),
		contracts.SubscriptionRequest{User: contracts.UserSpec{Username: "alice"}, Host: "vps.example.com"},
		443,
	)
	if err != nil {
		t.Fatalf("buildSubscriptionSpec: %v", err)
	}
	if spec.Protocol != contracts.ProtocolTrojan || spec.Password != "legacy-pass" {
		t.Errorf("legacy spec = %+v", spec)
	}
	if len(spec.Extensions) != 0 {
		t.Errorf("legacy Extensions should stay empty, got %+v", spec.Extensions)
	}
}
