package store_test

import (
	"path/filepath"
	"reflect"
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

	u := &contracts.User{Username: "alice", AuthToken: "pass", Role: "admin"}
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

	u := &contracts.User{Username: "bob", AuthToken: "pass", LoginPassword: "$2a$10$fakehashvalue"}
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

	u := &contracts.User{Username: "carol", AuthToken: "pass"}
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
		AuthToken:     "proxy-pass",
		Role:          "normal",
		LoginPassword: "$2a$10$loginhash",
		TrafficLimit:  1024 * 1024,
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
	if got.AuthToken != "proxy-pass" {
		t.Errorf("AuthToken: got %q, want proxy-pass", got.AuthToken)
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
}

func TestUserStore_UpdateRole(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "eve", AuthToken: "pass", Role: "normal"}
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

func TestUserStore_ListByGroup(t *testing.T) {
	s := openUserStore(t)

	// Save users in different groups.
	for _, u := range []*contracts.User{
		{Username: "alice", AuthToken: "tok-a", TargetGroup: "groupA", DeletionState: ""},
		{Username: "bob", AuthToken: "tok-b", TargetGroup: "groupA", DeletionState: "deleting"},
		{Username: "carol", AuthToken: "tok-c", TargetGroup: "groupB"},
		{Username: "dave", AuthToken: "tok-d", TargetGroup: ""},
	} {
		if err := s.Save(u); err != nil {
			t.Fatalf("Save(%s): %v", u.Username, err)
		}
	}

	// Query groupA — should return alice and bob (including tombstones).
	users, err := s.ListByGroup("groupA")
	if err != nil {
		t.Fatalf("ListByGroup(groupA): %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users in groupA, got %d", len(users))
	}
	// Verify ordering (ORDER BY username).
	if users[0].Username != "alice" || users[1].Username != "bob" {
		t.Errorf("unexpected order: %s, %s", users[0].Username, users[1].Username)
	}
	// Verify DeletionState is scanned correctly.
	if users[1].DeletionState != "deleting" {
		t.Errorf("expected bob DeletionState=deleting, got %q", users[1].DeletionState)
	}

	// Query groupB — should return carol only.
	users, err = s.ListByGroup("groupB")
	if err != nil {
		t.Fatalf("ListByGroup(groupB): %v", err)
	}
	if len(users) != 1 || users[0].Username != "carol" {
		t.Errorf("expected carol in groupB, got %v", users)
	}

	// Query empty group — should return dave.
	users, err = s.ListByGroup("")
	if err != nil {
		t.Fatalf("ListByGroup(): %v", err)
	}
	if len(users) != 1 || users[0].Username != "dave" {
		t.Errorf("expected dave in empty group, got %v", users)
	}

	// Query nonexistent group — should return empty slice.
	users, err = s.ListByGroup("nope")
	if err != nil {
		t.Fatalf("ListByGroup(nope): %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users in nope, got %d", len(users))
	}
}

func TestUserStore_UpdateLoginPassword(t *testing.T) {
	s := openUserStore(t)

	u := &contracts.User{Username: "frank", AuthToken: "pass", LoginPassword: "$2a$10$oldhash"}
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

func TestUserStore_InboundTags_RoundTrip(t *testing.T) {
	s := openUserStore(t)

	alice := &contracts.User{
		Username:    "alice",
		AuthToken:   "tok-a",
		TargetGroup: "vip",
		InboundTags: []string{"home-sh", "vip"},
	}
	bob := &contracts.User{
		Username:    "bob",
		AuthToken:   "tok-b",
		TargetGroup: "vip",
	}
	if err := s.Save(alice); err != nil {
		t.Fatalf("Save alice: %v", err)
	}
	if err := s.Save(bob); err != nil {
		t.Fatalf("Save bob: %v", err)
	}

	// Verify Load
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 users, got %d", len(loaded))
	}

	// Verify Get
	gotAlice, err := s.Get("alice")
	if err != nil {
		t.Fatalf("Get alice: %v", err)
	}
	if !reflect.DeepEqual(gotAlice.InboundTags, []string{"home-sh", "vip"}) {
		t.Errorf("Get alice InboundTags = %v, want [home-sh vip]", gotAlice.InboundTags)
	}
	gotBob, err := s.Get("bob")
	if err != nil {
		t.Fatalf("Get bob: %v", err)
	}
	if len(gotBob.InboundTags) != 0 {
		t.Errorf("Get bob InboundTags = %v, want empty", gotBob.InboundTags)
	}

	// Verify ListByGroup
	byGroup, err := s.ListByGroup("vip")
	if err != nil {
		t.Fatalf("ListByGroup: %v", err)
	}
	if len(byGroup) != 2 || !reflect.DeepEqual(byGroup[0].InboundTags, []string{"home-sh", "vip"}) {
		t.Errorf("ListByGroup alice InboundTags = %v", byGroup[0].InboundTags)
	}

	// Clear tags and verify update persists
	alice.InboundTags = nil
	if err := s.Save(alice); err != nil {
		t.Fatalf("Save cleared alice: %v", err)
	}
	gotCleared, err := s.Get("alice")
	if err != nil {
		t.Fatalf("Get cleared alice: %v", err)
	}
	if len(gotCleared.InboundTags) != 0 {
		t.Errorf("expected empty InboundTags after clear, got %v", gotCleared.InboundTags)
	}
}
