package mihomo

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

func TestGetUserSubscriptions_AnyTLS_Baseline_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "alice", "anytls-base", map[string]any{
		"protocol":    "anytls",
		"port":        20700,
		"password":    "anytls-secret",
		"cert_file":   "/tmp/cert.pem",
		"key_file":    "/tmp/key.pem",
		"cert_source": "self_signed",
		"sni":         "anytls.example.com",
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
	if spec.Protocol != contracts.ProtocolAnyTLS {
		t.Errorf("Protocol = %q, want anytls", spec.Protocol)
	}
	if spec.Password != "anytls-secret" {
		t.Errorf("Password = %q", spec.Password)
	}
	if spec.Extensions["server_name"] != "anytls.example.com" {
		t.Errorf("Extensions[server_name] = %v", spec.Extensions["server_name"])
	}
	if spec.Extensions["skip_cert_verify"] != true {
		t.Errorf("Extensions[skip_cert_verify] should be true for self_signed cert, got %v",
			spec.Extensions["skip_cert_verify"])
	}

	// URI must round-trip cleanly through DecodeAnyTLS.
	node, err := codec.DecodeAnyTLS(spec.URI)
	if err != nil {
		t.Fatalf("DecodeAnyTLS: %v", err)
	}
	if node.Password != "anytls-secret" {
		t.Errorf("URI password = %q", node.Password)
	}
	if node.SNI != "anytls.example.com" {
		t.Errorf("URI SNI = %q", node.SNI)
	}
	// Self-signed should propagate insecure=1 in URI for cross-tool clients
	// that do not consume Extensions.
	if !node.SkipCertVerify {
		t.Errorf("URI must carry insecure=1 for self_signed cert; got SkipCertVerify=false")
	}
}

func TestGetUserSubscriptions_AnyTLS_IdleKnobs_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("bob", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "bob", "anytls-tune", map[string]any{
		"protocol":                            "anytls",
		"port":                                20701,
		"password":                            "anytls-tune-pw",
		"cert_file":                           "/tmp/cert.pem",
		"key_file":                            "/tmp/key.pem",
		"sni":                                 "tune.example.com",
		"padding_scheme":                      "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000",
		"idle_session_check_interval_seconds": 30,
		"idle_session_timeout_seconds":        60,
		"min_idle_session":                    3,
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "bob"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]

	// Client-only knobs survive into Extensions.
	if spec.Extensions["idle_session_check_interval_seconds"] != 30 {
		t.Errorf("Extensions[idle_session_check_interval_seconds] = %v, want 30",
			spec.Extensions["idle_session_check_interval_seconds"])
	}
	if spec.Extensions["idle_session_timeout_seconds"] != 60 {
		t.Errorf("Extensions[idle_session_timeout_seconds] = %v, want 60",
			spec.Extensions["idle_session_timeout_seconds"])
	}
	if spec.Extensions["min_idle_session"] != 3 {
		t.Errorf("Extensions[min_idle_session] = %v, want 3",
			spec.Extensions["min_idle_session"])
	}

	// Server-only padding-scheme must NOT leak to Extensions — clash
	// converter would silently ignore it but we'd rather catch the leak
	// loud here than rely on downstream silence.
	if _, present := spec.Extensions["padding_scheme"]; present {
		t.Errorf("Extensions must not carry padding_scheme (server-only); got %v",
			spec.Extensions["padding_scheme"])
	}
}

// validateAnyTLS already gates empty-password records on the FastAdd /
// Restore path; an explicit subscription-side defensive test would have to
// reach inside c via APIs that aren't part of the test surface. Coverage
// for the empty-password rejection lives in TestValidateAnyTLS_BaselineOK
// and the parser tests in protocolparams.

// TestGetUserSubscriptions_AnyTLS_LowIdleSurvives_Pp locks the
// "v2raymg never silently substitutes mihomo's runtime ≤5s→30s fallback"
// contract end-to-end through the subscription pipeline. The parser-level
// test (TestParseAnyTLS_LowIdleNotBumped in protocolparams) covers Parse
// alone; this one extends the assertion to the entire fill→Extensions→
// converter chain so a future helper that "rounds up small values" in
// the subscription path would surface here.
func TestGetUserSubscriptions_AnyTLS_LowIdleSurvives_Pp(t *testing.T) {
	c, um, _ := newTestContainerWithUserMgr(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	if err := um.AddUserForTest("dave", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	wireUserAndInbound(t, c, "dave", "anytls-low", map[string]any{
		"protocol":                            "anytls",
		"port":                                20703,
		"password":                            "anytls-low-pw",
		"cert_file":                           "/tmp/cert.pem",
		"key_file":                            "/tmp/key.pem",
		"sni":                                 "low.example.com",
		"idle_session_check_interval_seconds": 3,
		"idle_session_timeout_seconds":        1,
		"min_idle_session":                    1,
	})

	specs, err := c.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: "dave"},
		Host: "vps.example.com",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	spec := specs[0]
	// Values must survive verbatim through the subscription Extensions
	// — mihomo's ≤5s runtime fallback is the *receiver's* responsibility.
	if got := spec.Extensions["idle_session_check_interval_seconds"]; got != 3 {
		t.Errorf("Extensions[idle_session_check_interval_seconds] = %v, want 3 (no parser/subscription tutoring)", got)
	}
	if got := spec.Extensions["idle_session_timeout_seconds"]; got != 1 {
		t.Errorf("Extensions[idle_session_timeout_seconds] = %v, want 1 (no parser/subscription tutoring)", got)
	}
	if got := spec.Extensions["min_idle_session"]; got != 1 {
		t.Errorf("Extensions[min_idle_session] = %v, want 1 (no parser/subscription tutoring)", got)
	}
}
