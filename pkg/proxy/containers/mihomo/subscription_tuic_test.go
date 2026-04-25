package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

const tuicSubTestUUID = "00112233-4455-6677-8899-aabbccddeeff"

func TestGetUserSubscriptions_Tuic_Baseline_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "tuic-base", map[string]any{
		"protocol":    "tuic",
		"port":        20600,
		"uuid":        tuicSubTestUUID,
		"password":    "tuic-secret",
		"cert_file":   "/tmp/cert.pem",
		"key_file":    "/tmp/key.pem",
		"cert_source": "self_signed",
		"sni":         "tuic.example.com",
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
	if spec.Protocol != contracts.ProtocolTUIC {
		t.Errorf("Protocol = %q, want tuic", spec.Protocol)
	}
	if spec.Password != "tuic-secret" {
		t.Errorf("Password = %q", spec.Password)
	}
	// uuid must come through Extensions — convertTuic reads ClashProxy.UUID
	// from there (spec.Password carries password, not uuid).
	if spec.Extensions["uuid"] != tuicSubTestUUID {
		t.Errorf("Extensions[uuid] = %v, want %s", spec.Extensions["uuid"], tuicSubTestUUID)
	}
	if spec.Extensions["server_name"] != "tuic.example.com" {
		t.Errorf("Extensions[server_name] = %v", spec.Extensions["server_name"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] should be true for self_signed cert, got %v",
			spec.Extensions["skip_cert_verify"])
	}

	// URI must round-trip cleanly through DecodeTuic.
	node, err := codec.DecodeTuic(spec.URI)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if node.UUID != tuicSubTestUUID {
		t.Errorf("URI uuid = %q", node.UUID)
	}
	if node.Password != "tuic-secret" {
		t.Errorf("URI password = %q", node.Password)
	}
	if node.SNI != "tuic.example.com" {
		t.Errorf("URI SNI = %q", node.SNI)
	}
	// Codec Encode never emits allow_insecure (mihomo strips it on parse)
	// — the SkipCertVerify hint reaches the client through Extensions only.
	if node.SkipCertVerify {
		t.Errorf("URI must not carry allow_insecure even when self_signed; got SkipCertVerify=true")
	}
}

func TestGetUserSubscriptions_Tuic_TuningKnobs_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("bob", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "bob", "tuic-tune", map[string]any{
		"protocol":              "tuic",
		"port":                  20601,
		"uuid":                  tuicSubTestUUID,
		"password":              "tune-pass",
		"cert_file":              "/tmp/cert.pem",
		"key_file":               "/tmp/key.pem",
		"congestion_controller": "bbr",
		"udp_relay_mode":        "quic",
		"zero_rtt_handshake":    true,
		"heartbeat_interval":    "10s",
		"disable_sni":           true,
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
	if spec.Extensions["congestion_controller"] != "bbr" {
		t.Errorf("Extensions[congestion_controller] = %v", spec.Extensions["congestion_controller"])
	}
	if spec.Extensions["udp_relay_mode"] != "quic" {
		t.Errorf("Extensions[udp_relay_mode] = %v", spec.Extensions["udp_relay_mode"])
	}
	if spec.Extensions["zero_rtt_handshake"] != true {
		t.Errorf("Extensions[zero_rtt_handshake] = %v", spec.Extensions["zero_rtt_handshake"])
	}
	if spec.Extensions["heartbeat_interval"] != "10s" {
		t.Errorf("Extensions[heartbeat_interval] = %v", spec.Extensions["heartbeat_interval"])
	}
	if spec.Extensions["disable_sni"] != true {
		t.Errorf("Extensions[disable_sni] = %v, want true", spec.Extensions["disable_sni"])
	}

	// URI carries congestion_control + udp_relay_mode + disable_sni (dae-spec
	// query keys with underscore). zero_rtt_handshake / heartbeat_interval
	// are NOT in the dae URI spec — they ride only on Extensions.
	node, err := codec.DecodeTuic(spec.URI)
	if err != nil {
		t.Fatalf("DecodeTuic: %v", err)
	}
	if node.CongestionControl != "bbr" {
		t.Errorf("URI congestion_control = %q", node.CongestionControl)
	}
	if node.UDPRelayMode != "quic" {
		t.Errorf("URI udp_relay_mode = %q", node.UDPRelayMode)
	}
	if !node.DisableSNI {
		t.Error("URI disable_sni should round-trip")
	}
	// Decode never sets these from URI — they're not in the dae spec.
	if node.ZeroRTTHandshake {
		t.Errorf("URI must not carry zero-rtt-handshake; got ZeroRTTHandshake=true")
	}
	if node.HeartbeatInterval != "" {
		t.Errorf("URI must not carry heartbeat-interval; got %q", node.HeartbeatInterval)
	}
}
