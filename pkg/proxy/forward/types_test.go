package forward

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestForwardRule_Validate_OK(t *testing.T) {
	r := ForwardRule{
		Username:   "user@test.com",
		TargetAddr: "127.0.0.1:443",
	}
	if err := r.Validate(); err != nil {
		t.Errorf("valid rule returned error: %v", err)
	}
}

func TestForwardRule_Validate_EmptyUserEmail(t *testing.T) {
	r := ForwardRule{TargetAddr: "127.0.0.1:443"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty user_email")
	}
}

func TestForwardRule_Validate_EmptyTargetAddr(t *testing.T) {
	r := ForwardRule{Username: "u@t.com"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected error for empty target_addr")
	}
}

func TestForwardRule_Validate_PortRange(t *testing.T) {
	r := ForwardRule{Username: "u@t.com", TargetAddr: "127.0.0.1:443"}

	// Port 0 is valid (means auto-allocate)
	r.ListenPort = 0
	if err := r.Validate(); err != nil {
		t.Errorf("port 0 should be valid (auto): %v", err)
	}

	// Port 99 is invalid
	r.ListenPort = 99
	if err := r.Validate(); err == nil {
		t.Error("expected error for port < 100")
	}

	// Port 65536 is invalid
	r.ListenPort = 65536
	if err := r.Validate(); err == nil {
		t.Error("expected error for port > 65535")
	}

	// Port 100 is valid
	r.ListenPort = 100
	if err := r.Validate(); err != nil {
		t.Errorf("port 100 should be valid: %v", err)
	}
}

func TestForwardRule_RuleKey(t *testing.T) {
	r := ForwardRule{
		Username:      "user@test.com",
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vmess-tcp",
	}
	expected := "xray:vmess-tcp:user@test.com"
	if got := r.RuleKey(); got != expected {
		t.Errorf("RuleKey() = %q, want %q", got, expected)
	}
}
