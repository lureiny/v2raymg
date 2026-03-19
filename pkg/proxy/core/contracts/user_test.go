package contracts_test

import (
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
