package usermanager

import (
	"fmt"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
)

// fakeHasher is a deterministic stand-in for auth.HashLoginPassword in these tests.
// We use auth.HashLoginPassword directly so that VerifyLoginPassword works correctly.

// failingHasher always returns an error, simulating a broken bcrypt implementation.
func failingHasher(_ string) (string, error) {
	return "", fmt.Errorf("simulated hash failure")
}

func newUserManagerWithHasher(t *testing.T) *UserManager {
	t.Helper()
	storeMgr := openTestStoreManager(t)
	m, err := NewUserManagerWithStore(newMockForwardManager(), storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	m.SetLoginPasswordHasher(auth.HashLoginPassword)
	return m
}

func TestAddUser_LoginPasswordSetImmediately(t *testing.T) {
	m := newUserManagerWithHasher(t)

	if err := m.AddUser(AddUserRequest{Username: "alice", Password: "secret"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	user, err := m.GetUser("alice")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.LoginPassword == "" {
		t.Fatal("expected LoginPassword to be set immediately after AddUser")
	}
	if !auth.VerifyLoginPassword(user.LoginPassword, "secret") {
		t.Error("LoginPassword does not verify against original password")
	}
}

func TestUpdateUser_LoginPasswordUpdatedImmediately(t *testing.T) {
	m := newUserManagerWithHasher(t)

	if err := m.AddUser(AddUserRequest{Username: "bob", Password: "oldpass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if err := m.UpdateUser("bob", "newpass", 0); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	user, err := m.GetUser("bob")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !auth.VerifyLoginPassword(user.LoginPassword, "newpass") {
		t.Error("expected LoginPassword to verify against new password after UpdateUser")
	}
	if auth.VerifyLoginPassword(user.LoginPassword, "oldpass") {
		t.Error("expected old password to no longer verify after UpdateUser")
	}
}

func TestUpdateUserPassword_LoginPasswordUpdatedImmediately(t *testing.T) {
	m := newUserManagerWithHasher(t)

	if err := m.AddUser(AddUserRequest{Username: "carol", Password: "oldpass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	if err := m.UpdateUserPassword(UpdateUserPasswordRequest{Username: "carol", NewPassword: "newpass"}); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	user, err := m.GetUser("carol")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !auth.VerifyLoginPassword(user.LoginPassword, "newpass") {
		t.Error("expected LoginPassword to verify against new password after UpdateUserPassword")
	}
	if auth.VerifyLoginPassword(user.LoginPassword, "oldpass") {
		t.Error("expected old password to no longer verify after UpdateUserPassword")
	}
}

func TestAddUser_WithoutHasher_LoginPasswordEmpty(t *testing.T) {
	// Without a hasher, LoginPassword should remain empty — no panic.
	storeMgr := openTestStoreManager(t)
	m, err := NewUserManagerWithStore(newMockForwardManager(), storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	// No SetLoginPasswordHasher call.

	if err := m.AddUser(AddUserRequest{Username: "dave", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	user, err := m.GetUser("dave")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	// login_password stays empty when no hasher is set (legacy / test mode)
	if user.LoginPassword != "" {
		t.Errorf("expected empty LoginPassword without hasher, got %q", user.LoginPassword)
	}
}

func TestAddUser_HasherFailure_ReturnsError(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	m, err := NewUserManagerWithStore(newMockForwardManager(), storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	m.SetLoginPasswordHasher(failingHasher)

	err = m.AddUser(AddUserRequest{Username: "alice", Password: "pass"})
	if err == nil {
		t.Fatal("expected error when hasher fails during AddUser")
	}
	// User must not have been added (no partial state).
	if _, getErr := m.GetUser("alice"); getErr == nil {
		t.Error("user should not exist after failed AddUser")
	}
}

func TestUpdateUser_HasherFailure_ReturnsError(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	m, err := NewUserManagerWithStore(newMockForwardManager(), storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	m.SetLoginPasswordHasher(auth.HashLoginPassword)

	if err := m.AddUser(AddUserRequest{Username: "bob", Password: "oldpass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	oldLP, _ := m.GetUser("bob")

	// Switch to failing hasher before UpdateUser.
	m.SetLoginPasswordHasher(failingHasher)
	err = m.UpdateUser("bob", "newpass", 0)
	if err == nil {
		t.Fatal("expected error when hasher fails during UpdateUser")
	}

	// Password must not have changed.
	after, _ := m.GetUser("bob")
	if after.Password != "oldpass" {
		t.Errorf("password changed despite hasher failure: got %q", after.Password)
	}
	if after.LoginPassword != oldLP.LoginPassword {
		t.Error("LoginPassword changed despite hasher failure")
	}
}

func TestUpdateUserPassword_HasherFailure_ReturnsError(t *testing.T) {
	storeMgr := openTestStoreManager(t)
	m, err := NewUserManagerWithStore(newMockForwardManager(), storeMgr)
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	m.SetLoginPasswordHasher(auth.HashLoginPassword)

	if err := m.AddUser(AddUserRequest{Username: "carol", Password: "oldpass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	oldLP, _ := m.GetUser("carol")

	// Switch to failing hasher before UpdateUserPassword.
	m.SetLoginPasswordHasher(failingHasher)
	err = m.UpdateUserPassword(UpdateUserPasswordRequest{Username: "carol", NewPassword: "newpass"})
	if err == nil {
		t.Fatal("expected error when hasher fails during UpdateUserPassword")
	}

	// Password and LoginPassword must not have changed.
	after, _ := m.GetUser("carol")
	if after.Password != "oldpass" {
		t.Errorf("password changed despite hasher failure: got %q", after.Password)
	}
	if after.LoginPassword != oldLP.LoginPassword {
		t.Error("LoginPassword changed despite hasher failure")
	}
}
