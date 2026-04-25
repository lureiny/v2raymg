package mihomo

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Phase 2 vmess subscription matrix — mirrors the vless analogue file but
// covers mihomo-VMess-supported combos (tcp/ws/grpc × none/tls/reality).
// Each sub-test wires an inbound through FastAddInbound and asserts the
// resulting SubscriptionSpec / URI carries every field the clash
// converter needs.

// decodeVMessBody pulls the JSON payload out of a vmess:// URI. The codec
// emits RFC 4648 std base64 with padding (see codec/vmess.go Encode).
func decodeVMessBody(t *testing.T, uri string) map[string]any {
	t.Helper()
	body := strings.TrimPrefix(uri, "vmess://")
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("vmess base64 decode: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("vmess json decode: %v", err)
	}
	return v
}

func TestGetUserSubscriptions_VMess_TCP_Plain_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-tcp", map[string]any{
		"protocol": "vmess",
		"port":     20201,
		"uuid":     "u-vmess-tcp",
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
	if spec.Protocol != contracts.ProtocolVMess {
		t.Errorf("Protocol = %q, want vmess", spec.Protocol)
	}
	if spec.Password != "u-vmess-tcp" {
		t.Errorf("Password = %q, want shared uuid", spec.Password)
	}
	body := decodeVMessBody(t, spec.URI)
	if body["add"] != "vps.example.com" {
		t.Errorf("URI add = %v", body["add"])
	}
	if body["id"] != "u-vmess-tcp" {
		t.Errorf("URI id = %v", body["id"])
	}
	// tcp-plain: no tls / transport keys in Extensions.
	if spec.Extensions["security"] != nil {
		t.Errorf("security should be absent, got %v", spec.Extensions["security"])
	}
	if spec.Extensions["transport"] != nil {
		t.Errorf("transport should be absent for tcp, got %v", spec.Extensions["transport"])
	}
}

func TestGetUserSubscriptions_VMess_WS_TLS_SelfSigned_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-ws", map[string]any{
		"protocol":    "vmess",
		"port":        20202,
		"uuid":        "u-vmess-ws",
		"transport":   "ws",
		"ws_path":     "/vmess-ws",
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
		t.Fatalf("got %d specs", len(specs))
	}
	spec := specs[0]

	body := decodeVMessBody(t, spec.URI)
	if body["net"] != "ws" {
		t.Errorf("URI net = %v, want ws", body["net"])
	}
	if body["path"] != "/vmess-ws" {
		t.Errorf("URI path = %v", body["path"])
	}
	if body["host"] != "cdn.example.com" {
		t.Errorf("URI host = %v", body["host"])
	}
	if body["tls"] != "tls" {
		t.Errorf("URI tls = %v, want tls", body["tls"])
	}

	if spec.Extensions["transport"] != "ws" {
		t.Errorf("Extensions[transport] = %v", spec.Extensions["transport"])
	}
	if spec.Extensions["ws_path"] != "/vmess-ws" {
		t.Errorf("Extensions[ws_path] = %v", spec.Extensions["ws_path"])
	}
	if spec.Extensions["security"] != "tls" {
		t.Errorf("Extensions[security] = %v, want tls", spec.Extensions["security"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] = %v, want true (self_signed)", spec.Extensions["skip_cert_verify"])
	}
	if spec.Extensions["server_name"] != "cdn.example.com" {
		t.Errorf("Extensions[server_name] = %v", spec.Extensions["server_name"])
	}
}

func TestGetUserSubscriptions_VMess_TCP_Reality_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-reality", map[string]any{
		"protocol":             "vmess",
		"port":                 20203,
		"uuid":                 "u-vmess-rl",
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

	if spec.Extensions["security"] != "reality" {
		t.Errorf("Extensions[security] = %v, want reality", spec.Extensions["security"])
	}
	if spec.Extensions["reality_public_key"] != subTestRealityPub {
		t.Errorf("Extensions[reality_public_key] = %v", spec.Extensions["reality_public_key"])
	}
	sids, _ := spec.Extensions["reality_short_ids"].([]string)
	if len(sids) == 0 || sids[0] != subTestShortID {
		t.Errorf("Extensions[reality_short_ids] = %v", spec.Extensions["reality_short_ids"])
	}
	if spec.Extensions["server_name"] != "www.microsoft.com" {
		t.Errorf("Extensions[server_name] = %v, want SNI from reality ServerNames[0]", spec.Extensions["server_name"])
	}
}

func TestGetUserSubscriptions_VMess_GRPC_TLS_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vmess-grpc", map[string]any{
		"protocol":          "vmess",
		"port":              20204,
		"uuid":              "u-vmess-grpc",
		"transport":         "grpc",
		"grpc_service_name": "VmessTunnel",
		"security":          "tls",
		"cert_file":         "/tmp/cert.pem",
		"key_file":          "/tmp/key.pem",
		"cert_source":       "file",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	spec := specs[0]
	body := decodeVMessBody(t, spec.URI)
	if body["net"] != "grpc" {
		t.Errorf("URI net = %v, want grpc", body["net"])
	}
	if body["path"] != "VmessTunnel" {
		t.Errorf("URI path (grpc service name) = %v", body["path"])
	}
	if spec.Extensions["transport"] != "grpc" {
		t.Errorf("Extensions[transport] = %v", spec.Extensions["transport"])
	}
	if spec.Extensions["grpc_service_name"] != "VmessTunnel" {
		t.Errorf("Extensions[grpc_service_name] = %v", spec.Extensions["grpc_service_name"])
	}
	// cert_source=file: skip_cert_verify must NOT be set.
	if spec.Extensions["skip_cert_verify"] != nil {
		t.Errorf("skip_cert_verify should be absent for cert_source=file, got %v", spec.Extensions["skip_cert_verify"])
	}
}
