package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

func TestGetUserSubscriptions_Hy2_TLS_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "hy2-tls", map[string]any{
		"protocol":    "hysteria2",
		"port":        20500,
		"password":    "hy2-secret",
		"cert_file":   "/tmp/cert.pem",
		"key_file":    "/tmp/key.pem",
		"cert_source": "self_signed",
		"sni":         "hy2.example.com",
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
	if spec.Protocol != contracts.ProtocolHysteria2 {
		t.Errorf("Protocol = %q, want hysteria2", spec.Protocol)
	}
	if spec.Password != "hy2-secret" {
		t.Errorf("Password = %q", spec.Password)
	}
	if spec.Extensions["server_name"] != "hy2.example.com" {
		t.Errorf("Extensions[server_name] = %v", spec.Extensions["server_name"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] should be true for self_signed cert, got %v",
			spec.Extensions["skip_cert_verify"])
	}

	// URI must round-trip cleanly through DecodeHysteria2 — the URI does
	// NOT carry up/down/masquerade (codec deliberately omits non-standard
	// query keys), but the basic fields must survive.
	node, err := codec.DecodeHysteria2(spec.URI)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if node.Password != "hy2-secret" {
		t.Errorf("URI password = %q", node.Password)
	}
	if node.SNI != "hy2.example.com" {
		t.Errorf("URI SNI = %q", node.SNI)
	}
	if !node.SkipCertVerify {
		t.Errorf("URI insecure flag should be set for self_signed cert")
	}
}

func TestGetUserSubscriptions_Hy2_Salamander_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("bob", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "bob", "hy2-obfs", map[string]any{
		"protocol":      "hysteria2",
		"port":          20501,
		"password":      "obfs-pass",
		"obfs":          "salamander",
		"obfs_password": "shh",
		"cert_file":     "/tmp/cert.pem",
		"key_file":      "/tmp/key.pem",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "bob"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs", len(specs))
	}
	spec := specs[0]
	if spec.Extensions["obfs"] != "salamander" {
		t.Errorf("Extensions[obfs] = %v", spec.Extensions["obfs"])
	}
	if spec.Extensions["obfs_password"] != "shh" {
		t.Errorf("Extensions[obfs_password] = %v", spec.Extensions["obfs_password"])
	}
	node, err := codec.DecodeHysteria2(spec.URI)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if node.Obfs != "salamander" || node.ObfsPassword != "shh" {
		t.Errorf("URI obfs fields = (%q, %q)", node.Obfs, node.ObfsPassword)
	}
}

func TestGetUserSubscriptions_Hy2_Bandwidth_Masquerade_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("carol", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "carol", "hy2-bw", map[string]any{
		"protocol":   "hysteria2",
		"port":       20502,
		"password":   "bw-pass",
		"up":         "50 Mbps",
		"down":       "100 Mbps",
		"masquerade": "https://www.microsoft.com",
		"cert_file":  "/tmp/cert.pem",
		"key_file":   "/tmp/key.pem",
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "carol"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs", len(specs))
	}
	spec := specs[0]
	if spec.Extensions["up"] != "50 Mbps" {
		t.Errorf("Extensions[up] = %v", spec.Extensions["up"])
	}
	if spec.Extensions["down"] != "100 Mbps" {
		t.Errorf("Extensions[down] = %v", spec.Extensions["down"])
	}
	if spec.Extensions["masquerade"] != "https://www.microsoft.com" {
		t.Errorf("Extensions[masquerade] = %v", spec.Extensions["masquerade"])
	}
	// URI must NOT carry up/down/masquerade (codec keeps them off the
	// URI on purpose). Decoding the URI should leave these fields zero.
	node, err := codec.DecodeHysteria2(spec.URI)
	if err != nil {
		t.Fatalf("DecodeHysteria2: %v", err)
	}
	if node.Up != "" || node.Down != "" || node.Masquerade != "" {
		t.Errorf("URI must not carry up/down/masquerade, got up=%q down=%q masq=%q",
			node.Up, node.Down, node.Masquerade)
	}
}
