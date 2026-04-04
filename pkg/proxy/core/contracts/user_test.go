package contracts_test

import (
	"encoding/json"
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

func TestUserSpec_GetExtension_NilMap(t *testing.T) {
	u := &contracts.UserSpec{}
	val, ok := u.GetExtension("key")
	if ok || val != nil {
		t.Error("expected nil, false for nil Extensions map")
	}
}

func TestUserSpec_SetExtension_NilMap(t *testing.T) {
	u := &contracts.UserSpec{}
	u.SetExtension("key", "value")
	val, ok := u.GetExtension("key")
	if !ok || val != "value" {
		t.Errorf("expected value after SetExtension, got %v, %v", val, ok)
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

func TestUserSpec_GetSetExtension(t *testing.T) {
	u := &contracts.UserSpec{
		Extensions: map[string]any{"existing": 42},
	}
	val, ok := u.GetExtension("existing")
	if !ok || val != 42 {
		t.Errorf("GetExtension: got %v, %v", val, ok)
	}

	u.SetExtension("new", "data")
	val, ok = u.GetExtension("new")
	if !ok || val != "data" {
		t.Errorf("after SetExtension: got %v, %v", val, ok)
	}
}
