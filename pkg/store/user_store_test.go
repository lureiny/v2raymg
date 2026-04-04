package store_test

import (
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// openUserStore returns a SQLiteUserStore backed by a fresh temp DB.
func openUserStore(t *testing.T) *store.SQLiteUserStore {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store.NewSQLiteUserStore(db)
}

func TestUserStore_Role_RoundTrip(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "alice", Password: "pass", Role: "admin"}
	if err := s.Save(u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Role != "admin" {
		t.Errorf("expected role=admin, got %q", users[0].Role)
	}
}

func TestUserStore_LoginPassword_RoundTrip(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "bob", Password: "pass", LoginPassword: "$2a$10$fakehashvalue"}
	if err := s.Save(u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].LoginPassword != "$2a$10$fakehashvalue" {
		t.Errorf("expected LoginPassword to be preserved, got %q", users[0].LoginPassword)
	}
}

func TestUserStore_DefaultRole(t *testing.T) {
	// Users saved with empty role must be persisted as "normal" (the documented default).
	// Save normalises "" → "normal" before writing to the DB.
	s := openUserStore(t)

	u := &contracts.User{Username: "carol", Password: "pass"}
	// Role intentionally left empty; Save should normalise to "normal".
	if err := s.Save(u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	// Empty role must be stored as "normal", not as an empty string.
	if users[0].Role != "normal" {
		t.Errorf("expected role=normal (default), got %q", users[0].Role)
	}
}

func TestUserStore_RoleAndLoginPassword_DoNotInterferWithOtherFields(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{
		Username:      "dave",
		Password:      "proxy-pass",
		Role:          "normal",
		LoginPassword: "$2a$10$loginhash",
		TrafficLimit:  1024 * 1024,
		Level:         2,
	}
	if err := s.Save(u); err != nil {
		t.Fatalf("Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	got := users[0]
	if got.Password != "proxy-pass" {
		t.Errorf("Password: got %q, want proxy-pass", got.Password)
	}
	if got.Role != "normal" {
		t.Errorf("Role: got %q, want normal", got.Role)
	}
	if got.LoginPassword != "$2a$10$loginhash" {
		t.Errorf("LoginPassword: got %q, want $2a$10$loginhash", got.LoginPassword)
	}
	if got.TrafficLimit != 1024*1024 {
		t.Errorf("TrafficLimit: got %d, want %d", got.TrafficLimit, 1024*1024)
	}
	if got.Level != 2 {
		t.Errorf("Level: got %d, want 2", got.Level)
	}
}

func TestUserStore_UpdateRole(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "eve", Password: "pass", Role: "normal"}
	if err := s.Save(u); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	// Promote to admin
	u.Role = "admin"
	if err := s.Save(u); err != nil {
		t.Fatalf("update Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Role != "admin" {
		t.Errorf("expected role=admin after update, got %q", users[0].Role)
	}
}

func TestUserStore_UpdateLoginPassword(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "frank", Password: "pass", LoginPassword: "$2a$10$oldhash"}
	if err := s.Save(u); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	u.LoginPassword = "$2a$10$newhash"
	if err := s.Save(u); err != nil {
		t.Fatalf("update Save: %v", err)
	}

	users, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if users[0].LoginPassword != "$2a$10$newhash" {
		t.Errorf("expected updated LoginPassword, got %q", users[0].LoginPassword)
	}
}
