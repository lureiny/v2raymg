package store_test

import (
	"path/filepath"
	"testing"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	custore "github.com/lureiny/v2raymg/pkg/cluster_user/store"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func openClusterUserStore(t *testing.T) custore.ClusterUserStore {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return custore.NewSQLiteClusterUserStore(db)
}

func sampleUser(username string) *clusteruser.ClusterUser {
	return &clusteruser.ClusterUser{
		Username:    username,
		Password:    "pass",
		Expire:      0,
		Role:        "normal",
		TargetGroup: "default",
		Deleted:     false,
		UpdatedAtUs: 1000000,
		OriginNode:  "node-1",
		Hash:        "abc123",
	}
}

func TestClusterUserStore_ListEmpty(t *testing.T) {
	s := openClusterUserStore(t)

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if users == nil {
		t.Error("List returned nil, expected empty non-nil slice")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestClusterUserStore_Upsert_Get(t *testing.T) {
	s := openClusterUserStore(t)

	u := sampleUser("alice")
	if err := s.Upsert(u); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get("alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil, expected a user")
	}
	if got.Username != "alice" {
		t.Errorf("Username: got %q, want alice", got.Username)
	}
	if got.Password != "pass" {
		t.Errorf("Password: got %q, want pass", got.Password)
	}
	if got.Role != "normal" {
		t.Errorf("Role: got %q, want normal", got.Role)
	}
	if got.TargetGroup != "default" {
		t.Errorf("TargetGroup: got %q, want default", got.TargetGroup)
	}
	if got.Deleted {
		t.Error("Deleted: got true, want false")
	}
	if got.UpdatedAtUs != 1000000 {
		t.Errorf("UpdatedAtUs: got %d, want 1000000", got.UpdatedAtUs)
	}
	if got.OriginNode != "node-1" {
		t.Errorf("OriginNode: got %q, want node-1", got.OriginNode)
	}
	if got.Hash != "abc123" {
		t.Errorf("Hash: got %q, want abc123", got.Hash)
	}
}

func TestClusterUserStore_Get_NotFound(t *testing.T) {
	s := openClusterUserStore(t)

	got, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get returned %+v, expected nil for not-found", got)
	}
}

func TestClusterUserStore_Upsert_Overwrites(t *testing.T) {
	s := openClusterUserStore(t)

	u := sampleUser("bob")
	if err := s.Upsert(u); err != nil {
		t.Fatalf("initial Upsert: %v", err)
	}

	u.Password = "newpass"
	u.UpdatedAtUs = 2000000
	u.Hash = "def456"
	if err := s.Upsert(u); err != nil {
		t.Fatalf("update Upsert: %v", err)
	}

	got, err := s.Get("bob")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Password != "newpass" {
		t.Errorf("Password after update: got %q, want newpass", got.Password)
	}
	if got.UpdatedAtUs != 2000000 {
		t.Errorf("UpdatedAtUs after update: got %d, want 2000000", got.UpdatedAtUs)
	}
}

func TestClusterUserStore_List_Multiple(t *testing.T) {
	s := openClusterUserStore(t)

	for _, name := range []string{"alice", "bob", "carol"} {
		if err := s.Upsert(sampleUser(name)); err != nil {
			t.Fatalf("Upsert %q: %v", name, err)
		}
	}

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}
}

func TestClusterUserStore_List_IncludesDeleted(t *testing.T) {
	s := openClusterUserStore(t)

	u := sampleUser("dave")
	if err := s.Upsert(u); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	u.Deleted = true
	if err := s.Upsert(u); err != nil {
		t.Fatalf("Upsert deleted: %v", err)
	}

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if !users[0].Deleted {
		t.Error("expected Deleted=true")
	}
}

func TestClusterUserStore_Count(t *testing.T) {
	s := openClusterUserStore(t)

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}

	for _, name := range []string{"x", "y"} {
		if err := s.Upsert(sampleUser(name)); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
	}

	count, err = s.Count()
	if err != nil {
		t.Fatalf("Count after inserts: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}
}

func TestClusterUserStore_Close(t *testing.T) {
	s := openClusterUserStore(t)
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestClusterUserStore_ListByGroup_Empty(t *testing.T) {
	s := openClusterUserStore(t)

	users, err := s.ListByGroup("nonexistent-group")
	if err != nil {
		t.Fatalf("ListByGroup: %v", err)
	}
	if users == nil {
		t.Error("ListByGroup returned nil, expected empty non-nil slice")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestClusterUserStore_ListByGroup_Filter(t *testing.T) {
	s := openClusterUserStore(t)

	// Insert users in two different groups.
	u1 := sampleUser("alice")
	u1.TargetGroup = "group-a"
	if err := s.Upsert(u1); err != nil {
		t.Fatalf("Upsert alice: %v", err)
	}

	u2 := sampleUser("bob")
	u2.TargetGroup = "group-a"
	if err := s.Upsert(u2); err != nil {
		t.Fatalf("Upsert bob: %v", err)
	}

	u3 := sampleUser("carol")
	u3.TargetGroup = "group-b"
	if err := s.Upsert(u3); err != nil {
		t.Fatalf("Upsert carol: %v", err)
	}

	users, err := s.ListByGroup("group-a")
	if err != nil {
		t.Fatalf("ListByGroup: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users in group-a, got %d", len(users))
	}
	for _, u := range users {
		if u.TargetGroup != "group-a" {
			t.Errorf("unexpected TargetGroup %q, want group-a", u.TargetGroup)
		}
	}

	// group-b should only have carol.
	usersB, err := s.ListByGroup("group-b")
	if err != nil {
		t.Fatalf("ListByGroup group-b: %v", err)
	}
	if len(usersB) != 1 {
		t.Fatalf("expected 1 user in group-b, got %d", len(usersB))
	}
	if usersB[0].Username != "carol" {
		t.Errorf("expected carol in group-b, got %q", usersB[0].Username)
	}
}

func TestClusterUserStore_ListByGroup_IncludesDeleted(t *testing.T) {
	s := openClusterUserStore(t)

	// Insert one active and one deleted user in the same group.
	u1 := sampleUser("eve")
	u1.TargetGroup = "group-c"
	if err := s.Upsert(u1); err != nil {
		t.Fatalf("Upsert eve: %v", err)
	}

	u2 := sampleUser("frank")
	u2.TargetGroup = "group-c"
	u2.Deleted = true
	if err := s.Upsert(u2); err != nil {
		t.Fatalf("Upsert frank: %v", err)
	}

	users, err := s.ListByGroup("group-c")
	if err != nil {
		t.Fatalf("ListByGroup: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users (including deleted) in group-c, got %d", len(users))
	}

	deletedCount := 0
	for _, u := range users {
		if u.Deleted {
			deletedCount++
		}
	}
	if deletedCount != 1 {
		t.Errorf("expected 1 deleted user, got %d", deletedCount)
	}
}
