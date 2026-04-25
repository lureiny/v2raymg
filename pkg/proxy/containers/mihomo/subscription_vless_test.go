package mihomo

import (
	"net/url"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/params/protocolparams"
)

// Reality test fixtures. parseReality validates shortIds against the
// xray-parity 16-hex format and keys against base64url/base64 32-byte
// form — placeholders like "PRIV" no longer pass. Generate a real pair
// once per test process.
var (
	subTestRealityPriv string
	subTestRealityPub  string
	subTestShortID     = "aabbccddeeff0011"
	subTestShortIDAlt  = "1122334455667788"
)

func init() {
	priv, pub, err := protocolparams.GenerateRealityKeyPair()
	if err != nil {
		panic("mihomo subscription_vless_test init: " + err.Error())
	}
	subTestRealityPriv = priv
	subTestRealityPub = pub
}

// vless subscription matrix — drives FastAddInbound through the vless
// path and asserts the resulting SubscriptionSpec + URI carries every
// transport/security detail the clash converter will need.

func TestGetUserSubscriptions_VLess_TCP_Plain(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vless-tcp", map[string]any{
		"protocol": "vless",
		"port":     10201,
		"uuid":     "u-vless-tcp",
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
	if spec.Protocol != contracts.ProtocolVLess {
		t.Errorf("Protocol = %q, want vless", spec.Protocol)
	}
	if spec.Password != "u-vless-tcp" {
		t.Errorf("Password = %q, want shared uuid", spec.Password)
	}
	if !strings.HasPrefix(spec.URI, "vless://u-vless-tcp@vps.example.com:") {
		t.Errorf("URI prefix wrong: %q", spec.URI)
	}
}

func TestGetUserSubscriptions_VLess_TCP_Reality(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vless-reality", map[string]any{
		"protocol":             "vless",
		"port":                 10202,
		"uuid":                 "u-vless-rl",
		"flow":                 "xtls-rprx-vision",
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
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]

	// URI query should have security=reality + flow + pbk + sid + sni
	u, err := url.Parse(spec.URI)
	if err != nil {
		t.Fatalf("URI parse: %v", err)
	}
	q := u.Query()
	if q.Get("security") != "reality" {
		t.Errorf("security query = %q, want reality", q.Get("security"))
	}
	if q.Get("flow") != "xtls-rprx-vision" {
		t.Errorf("flow query = %q", q.Get("flow"))
	}
	if q.Get("pbk") != subTestRealityPub {
		t.Errorf("pbk query = %q, want %q", q.Get("pbk"), subTestRealityPub)
	}
	if q.Get("sid") != subTestShortID {
		t.Errorf("sid query = %q, want first ShortID %q", q.Get("sid"), subTestShortID)
	}
	if q.Get("sni") != "www.microsoft.com" {
		t.Errorf("sni query = %q", q.Get("sni"))
	}

	// Extensions should echo security details so clash converter doesn't
	// re-decode the URI.
	if spec.Extensions["security"] != "reality" {
		t.Errorf("Extensions[security] = %v", spec.Extensions["security"])
	}
	if spec.Extensions["flow"] != "xtls-rprx-vision" {
		t.Errorf("Extensions[flow] = %v", spec.Extensions["flow"])
	}
	if spec.Extensions["reality_public_key"] != subTestRealityPub {
		t.Errorf("Extensions[reality_public_key] = %v", spec.Extensions["reality_public_key"])
	}
	sids, _ := spec.Extensions["reality_short_ids"].([]string)
	if len(sids) == 0 || sids[0] != subTestShortID {
		t.Errorf("Extensions[reality_short_ids] = %v", spec.Extensions["reality_short_ids"])
	}
}

func TestGetUserSubscriptions_VLess_WS_TLS_SelfSigned(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vless-ws", map[string]any{
		"protocol":  "vless",
		"port":      10203,
		"uuid":      "u-vless-ws",
		"transport": "ws",
		"ws_path":   "/vless-ws",
		"ws_host":   "cdn.example.com",
		"security":  "tls",
		"cert_file": "/tmp/cert.pem",
		"key_file":  "/tmp/key.pem",
		// cert_source=self_signed — subscription should mark
		// skip-cert-verify so self-signed clients can connect.
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

	u, _ := url.Parse(spec.URI)
	q := u.Query()
	if q.Get("type") != "ws" {
		t.Errorf("type query = %q, want ws", q.Get("type"))
	}
	if q.Get("path") != "/vless-ws" {
		t.Errorf("path query = %q", q.Get("path"))
	}
	if q.Get("host") != "cdn.example.com" {
		t.Errorf("host query = %q", q.Get("host"))
	}
	if q.Get("security") != "tls" {
		t.Errorf("security query = %q", q.Get("security"))
	}
	if q.Get("insecure") != "1" {
		t.Errorf("insecure query = %q, want 1 (self_signed)", q.Get("insecure"))
	}

	if spec.Extensions["transport"] != "ws" {
		t.Errorf("Extensions[transport] = %v", spec.Extensions["transport"])
	}
	if spec.Extensions["ws_path"] != "/vless-ws" {
		t.Errorf("Extensions[ws_path] = %v", spec.Extensions["ws_path"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] = %v, want true", spec.Extensions["skip_cert_verify"])
	}
}

func TestGetUserSubscriptions_VLess_GRPC_Reality(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vless-grpc", map[string]any{
		"protocol":             "vless",
		"port":                 10204,
		"uuid":                 "u-grpc",
		"transport":            "grpc",
		"grpc_service_name":    "TunnelService",
		"security":             "reality",
		"reality_target":       "example.com:443",
		"reality_server_names": []string{"example.com"},
		// No keys and no short_ids — parseReality auto-fills (xray parity).
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	spec := specs[0]

	if spec.Extensions["transport"] != "grpc" {
		t.Errorf("Extensions[transport] = %v", spec.Extensions["transport"])
	}
	if spec.Extensions["grpc_service_name"] != "TunnelService" {
		t.Errorf("Extensions[grpc_service_name] = %v", spec.Extensions["grpc_service_name"])
	}
	if spec.Extensions["security"] != "reality" {
		t.Errorf("Extensions[security] = %v", spec.Extensions["security"])
	}
}

func TestGetUserSubscriptions_VLess_XHTTP_TLS(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "vless-xh", map[string]any{
		"protocol":    "vless",
		"port":        10205,
		"uuid":        "u-xh",
		"transport":   "xhttp",
		"xhttp_path":  "/xh",
		"xhttp_mode":  "auto",
		"security":    "tls",
		"cert_file":   "/tmp/cert.pem",
		"key_file":    "/tmp/key.pem",
		"cert_source": "file",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "alice"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	spec := specs[0]

	u, _ := url.Parse(spec.URI)
	q := u.Query()
	if q.Get("type") != "xhttp" {
		t.Errorf("type = %q, want xhttp", q.Get("type"))
	}
	if q.Get("mode") != "auto" {
		t.Errorf("mode = %q", q.Get("mode"))
	}
	if q.Get("insecure") == "1" {
		t.Errorf("insecure should NOT be set for cert_source=file")
	}
}
