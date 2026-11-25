package contracts_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestUserSpec_IsExpired_ZeroValue(t *testing.T) {
	u := &contracts.UserSpec{}
	if u.IsExpired() {
		t.Error("zero ExpiryTime should not be expired")
	}
}

func TestUserSpec_IsExpired_Past(t *testing.T) {
	u := &contracts.UserSpec{
		ExpiryTime: time.Now().Add(-1 * time.Hour),
	}
	if !u.IsExpired() {
		t.Error("past ExpiryTime should be expired")
	}
}

func TestUserSpec_IsExpired_Future(t *testing.T) {
	u := &contracts.UserSpec{
		ExpiryTime: time.Now().Add(1 * time.Hour),
	}
	if u.IsExpired() {
		t.Error("future ExpiryTime should not be expired")
	}
}

func TestUserSpec_IsDeleting(t *testing.T) {
	u := &contracts.UserSpec{}
	if u.IsDeleting() {
		t.Error("new user should not be deleting")
	}
}

func TestUserSpec_MarkDeleting(t *testing.T) {
	u := &contracts.UserSpec{}
	u.MarkDeleting()
	if !u.IsDeleting() {
		t.Error("expected IsDeleting true after MarkDeleting")
	}
	if u.DeletionState != "deleting" {
		t.Errorf("DeletionState: got %q, want %q", u.DeletionState, "deleting")
	}
}

func TestUserSpec_MarkActive(t *testing.T) {
	u := &contracts.UserSpec{}
	u.MarkDeleting()
	u.MarkActive()
	if u.IsDeleting() {
		t.Error("expected IsDeleting false after MarkActive")
	}
	if u.DeletionState != "" {
		t.Errorf("DeletionState: got %q, want %q", u.DeletionState, "")
	}
}

// TestUserSpec_NewFields locks the zero-value semantics and JSON behaviour of the
// Role and LoginPassword fields added for frontend authentication.
func TestUserSpec_NewFields(t *testing.T) {
	t.Run("Role_ZeroValue", func(t *testing.T) {
		u := &contracts.UserSpec{}
		if u.Role != "" {
			t.Errorf("Role zero value must be empty string, got %q", u.Role)
		}
	})

	t.Run("LoginPassword_ZeroValue", func(t *testing.T) {
		u := &contracts.UserSpec{}
		if u.LoginPassword != "" {
			t.Errorf("LoginPassword zero value must be empty string, got %q", u.LoginPassword)
		}
	})

	t.Run("LoginPassword_NotInJSON", func(t *testing.T) {
		u := &contracts.UserSpec{Username: "alice", LoginPassword: "secret-bcrypt-hash"}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if strings.Contains(string(data), "login_password") || strings.Contains(string(data), "secret-bcrypt-hash") {
			t.Errorf("LoginPassword must not appear in JSON output, got: %s", string(data))
		}
	})

	t.Run("Role_PreservedInJSON_RoundTrip", func(t *testing.T) {
		u := &contracts.UserSpec{Username: "bob", Role: "admin"}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var decoded contracts.UserSpec
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if decoded.Role != "admin" {
			t.Errorf("Role not preserved after JSON round-trip: got %q, want admin", decoded.Role)
		}
		// LoginPassword must not have been populated from JSON (it was never in the output).
		if decoded.LoginPassword != "" {
			t.Errorf("LoginPassword must remain empty after JSON round-trip, got %q", decoded.LoginPassword)
		}
	})
}

func TestUserSpec_RoleField(t *testing.T) {
	u := &contracts.UserSpec{Role: "admin"}
	if u.Role != "admin" {
		t.Errorf("expected role=admin, got %q", u.Role)
	}
	u.Role = "normal"
	if u.Role != "normal" {
		t.Errorf("expected role=normal after update, got %q", u.Role)
	}
}

func TestUserSpec_LoginPasswordField(t *testing.T) {
	// LoginPassword is stored on the struct and must not be serialised to JSON (json:"-").
	u := &contracts.UserSpec{Username: "alice", LoginPassword: "$2a$10$fakehash"}
	if u.LoginPassword != "$2a$10$fakehash" {
		t.Errorf("expected LoginPassword to be stored, got %q", u.LoginPassword)
	}
}

func TestUserSpec_InboundTags_JSONAndClone(t *testing.T) {
	t.Run("omitempty_when_empty", func(t *testing.T) {
		u := &contracts.UserSpec{Username: "alice"}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if strings.Contains(string(data), "inbound_tags") {
			t.Errorf("expected empty InboundTags to be omitted in JSON, got %s", string(data))
		}
	})

	t.Run("json_round_trip", func(t *testing.T) {
		u := &contracts.UserSpec{Username: "alice", InboundTags: []string{"home-sh", "vip"}}
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var decoded contracts.UserSpec
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if !reflect.DeepEqual(decoded.InboundTags, []string{"home-sh", "vip"}) {
			t.Errorf("InboundTags = %v, want [home-sh vip]", decoded.InboundTags)
		}
	})

	t.Run("clone_deep_copies_inbound_tags", func(t *testing.T) {
		var nilUser *contracts.UserSpec
		if nilUser.Clone() != nil {
			t.Fatal("nil Clone should return nil")
		}

		orig := &contracts.UserSpec{
			Username:     "alice",
			BindPorts:    []uint32{10001},
			InboundTags:  []string{"home-sh", "vip"},
			PortMappings: map[uint32]uint32{443: 10001},
		}
		cloned := orig.Clone()
		if !reflect.DeepEqual(cloned.InboundTags, orig.InboundTags) {
			t.Fatalf("cloned InboundTags = %v, want %v", cloned.InboundTags, orig.InboundTags)
		}

		// Mutating original slice elements and map must not affect clone.
		orig.InboundTags[0] = "mutated"
		orig.BindPorts[0] = 99999
		orig.PortMappings[443] = 99999
		if cloned.InboundTags[0] != "home-sh" {
			t.Errorf("clone InboundTags aliased original: got %v", cloned.InboundTags)
		}
		if cloned.BindPorts[0] != 10001 {
			t.Errorf("clone BindPorts aliased original: got %v", cloned.BindPorts)
		}
		if cloned.PortMappings[443] != 10001 {
			t.Errorf("clone PortMappings aliased original: got %v", cloned.PortMappings)
		}

		// Mutating clone must not affect original.
		cloned.InboundTags[1] = "clone-only"
		if orig.InboundTags[1] != "vip" {
			t.Errorf("original InboundTags aliased clone: got %v", orig.InboundTags)
		}
	})
}
