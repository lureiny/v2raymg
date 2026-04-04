package syncer

import (
	"fmt"
	"sync"
	"testing"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
)

// ---------------------------------------------------------------------------
// In-memory store
// ---------------------------------------------------------------------------

type memStore struct {
	mu    sync.RWMutex
	users map[string]*clusteruser.ClusterUser
}

func newMemStore(initial ...*clusteruser.ClusterUser) *memStore {
	s := &memStore{users: make(map[string]*clusteruser.ClusterUser)}
	for _, u := range initial {
		u2 := *u
		s.users[u.Username] = &u2
	}
	return s
}

func (s *memStore) List() ([]*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*clusteruser.ClusterUser, 0, len(s.users))
	for _, u := range s.users {
		u2 := *u
		out = append(out, &u2)
	}
	return out, nil
}

func (s *memStore) ListByGroup(group string) ([]*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*clusteruser.ClusterUser
	for _, u := range s.users {
		if u.TargetGroup == group {
			u2 := *u
			out = append(out, &u2)
		}
	}
	return out, nil
}

func (s *memStore) Get(username string) (*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return nil, nil
	}
	u2 := *u
	return &u2, nil
}

func (s *memStore) Upsert(u *clusteruser.ClusterUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u2 := *u
	s.users[u.Username] = &u2
	return nil
}

func (s *memStore) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users), nil
}

func (s *memStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func localUser(username string, updatedAtUs int64, originNode, hash string) *clusteruser.ClusterUser {
	return &clusteruser.ClusterUser{
		Username:    username,
		Password:    "pass",
		TargetGroup: "default",
		UpdatedAtUs: updatedAtUs,
		OriginNode:  originNode,
		Hash:        hash,
	}
}

func digest(username string, updatedAtUs int64, originNode, hash string) clusteruser.UserDigest {
	return clusteruser.UserDigest{
		Username:    username,
		UpdatedAtUs: updatedAtUs,
		OriginNode:  originNode,
		Hash:        hash,
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Error-injecting store wrapper
// ---------------------------------------------------------------------------

type errorStore struct {
	*memStore
	failGet map[string]bool // usernames whose Get should fail
}

func newErrorStore(failUsernames []string, initial ...*clusteruser.ClusterUser) *errorStore {
	fail := make(map[string]bool, len(failUsernames))
	for _, u := range failUsernames {
		fail[u] = true
	}
	return &errorStore{memStore: newMemStore(initial...), failGet: fail}
}

func (s *errorStore) Get(username string) (*clusteruser.ClusterUser, error) {
	if s.failGet[username] {
		return nil, fmt.Errorf("simulated DB error for %s", username)
	}
	return s.memStore.Get(username)
}

// ---------------------------------------------------------------------------
// CompareDigests tests
// ---------------------------------------------------------------------------

// TestCompareDigests_RequestWhenLocalMissing: remote has user, local does not → request full payload
func TestCompareDigests_RequestWhenLocalMissing(t *testing.T) {
	store := newMemStore() // empty
	s := NewSyncer(store, "node-local")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("alice", 1000, "node-remote", "hash-a"),
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "alice") {
		t.Error("expected alice in NeedFull list (local missing)")
	}
}

// TestCompareDigests_RequestWhenRemoteNewer: remote version newer → request full payload
func TestCompareDigests_RequestWhenRemoteNewer(t *testing.T) {
	store := newMemStore(localUser("bob", 500, "node-local", "hash-old"))
	s := NewSyncer(store, "node-local")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("bob", 1000, "node-remote", "hash-new"), // newer timestamp
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "bob") {
		t.Error("expected bob in NeedFull list (remote newer)")
	}
}

// TestCompareDigests_NoRequestWhenLocalNewer: local version newer → do not request
func TestCompareDigests_NoRequestWhenLocalNewer(t *testing.T) {
	store := newMemStore(localUser("carol", 2000, "node-local", "hash-local"))
	s := NewSyncer(store, "node-local")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("carol", 1000, "node-remote", "hash-remote"), // older timestamp
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if contains(need, "carol") {
		t.Error("carol should NOT be in NeedFull list (local is newer)")
	}
}

// TestCompareDigests_NoRequestWhenIdentical: same version and hash → no request
func TestCompareDigests_NoRequestWhenIdentical(t *testing.T) {
	store := newMemStore(localUser("dave", 1000, "node-x", "hash-same"))
	s := NewSyncer(store, "node-x")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("dave", 1000, "node-x", "hash-same"),
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if contains(need, "dave") {
		t.Error("dave should NOT be requested (identical version and hash)")
	}
}

// TestCompareDigests_RequestWhenHashMismatchAtSameVersion: same version but different hash → request
func TestCompareDigests_RequestWhenHashMismatchAtSameVersion(t *testing.T) {
	store := newMemStore(localUser("eve", 1000, "node-x", "hash-local"))
	s := NewSyncer(store, "node-x")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("eve", 1000, "node-x", "hash-different"), // same version, different hash
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "eve") {
		t.Error("expected eve in NeedFull list (hash mismatch at same version)")
	}
}

// TestCompareDigests_MultipleUsers: mix of need/no-need
func TestCompareDigests_MultipleUsers(t *testing.T) {
	store := newMemStore(
		localUser("existing-current", 1000, "node-x", "hash-c"),
		localUser("existing-older", 500, "node-x", "hash-o"),
	)
	s := NewSyncer(store, "node-x")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("new-user", 1000, "node-remote", "hash-n"),         // missing → request
		digest("existing-current", 1000, "node-x", "hash-c"),     // identical → skip
		digest("existing-older", 1000, "node-remote", "hash-new"), // remote newer → request
	})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "new-user") {
		t.Error("new-user should be requested")
	}
	if contains(need, "existing-current") {
		t.Error("existing-current should NOT be requested")
	}
	if !contains(need, "existing-older") {
		t.Error("existing-older should be requested (remote is newer)")
	}
}

// TestCompareDigests_DBError_ReturnsErrorAndPartialResult: DB failure → error returned, affected user still in needFull
func TestCompareDigests_DBError_ReturnsErrorAndPartialResult(t *testing.T) {
	// "bob" exists locally, "alice" will hit a DB error
	store := newErrorStore(
		[]string{"alice"},
		localUser("bob", 500, "node-local", "hash-b"),
	)
	s := NewSyncer(store, "node-local")

	need, err := s.CompareDigests([]clusteruser.UserDigest{
		digest("alice", 1000, "node-remote", "hash-a"), // DB error → still in needFull
		digest("bob", 1000, "node-remote", "hash-new"), // remote newer → in needFull
	})

	// Error should be non-nil (DB failures occurred)
	if err == nil {
		t.Fatal("expected non-nil error when DB read fails")
	}

	// Both users should still be in needFull (self-healing behavior)
	if !contains(need, "alice") {
		t.Error("expected alice in needFull despite DB error")
	}
	if !contains(need, "bob") {
		t.Error("expected bob in needFull (remote is newer)")
	}
}

// ---------------------------------------------------------------------------
// UpsertFromRemote tests
// ---------------------------------------------------------------------------

// TestUpsertFromRemote_AppliesNewerVersion: remote is newer → stored
func TestUpsertFromRemote_AppliesNewerVersion(t *testing.T) {
	local := localUser("frank", 500, "node-local", "hash-old")
	local.Password = "old-pass"
	store := newMemStore(local)
	s := NewSyncer(store, "node-local")

	remote := &clusteruser.ClusterUser{
		Username:    "frank",
		Password:    "new-pass",
		UpdatedAtUs: 1000, // newer
		OriginNode:  "node-remote",
		Hash:        "hash-new",
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{remote}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	updated, _ := store.Get("frank")
	if updated == nil || updated.Password != "new-pass" {
		t.Error("expected frank's password to be updated to new-pass")
	}
}

// TestUpsertFromRemote_SkipsOlderVersion: remote is older → local preserved
func TestUpsertFromRemote_SkipsOlderVersion(t *testing.T) {
	local := localUser("grace", 2000, "node-local", "hash-current")
	local.Password = "current-pass"
	store := newMemStore(local)
	s := NewSyncer(store, "node-local")

	remote := &clusteruser.ClusterUser{
		Username:    "grace",
		Password:    "old-pass",
		UpdatedAtUs: 1000, // older
		OriginNode:  "node-remote",
		Hash:        "hash-old",
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{remote}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	unchanged, _ := store.Get("grace")
	if unchanged == nil || unchanged.Password != "current-pass" {
		t.Error("grace's local copy should be preserved (remote is older)")
	}
}

// TestUpsertFromRemote_InsertsNewUser: user not known locally → inserted
func TestUpsertFromRemote_InsertsNewUser(t *testing.T) {
	store := newMemStore()
	s := NewSyncer(store, "node-local")

	remote := &clusteruser.ClusterUser{
		Username:    "henry",
		Password:    "pass-h",
		UpdatedAtUs: 1000,
		OriginNode:  "node-remote",
		Hash:        "hash-h",
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{remote}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	henry, _ := store.Get("henry")
	if henry == nil || henry.Password != "pass-h" {
		t.Error("henry should be inserted from remote")
	}
}

// TestUpsertFromRemote_SkipsNilAndEmptyUsername: nil/empty entries → ignored, no panic
func TestUpsertFromRemote_SkipsNilAndEmptyUsername(t *testing.T) {
	store := newMemStore()
	s := NewSyncer(store, "node-local")

	err := s.UpsertFromRemote([]*clusteruser.ClusterUser{
		nil,
		{Username: "", Password: "x"},
	})
	if err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}
	count, _ := store.Count()
	if count != 0 {
		t.Errorf("expected 0 users stored, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Tombstone / deletion propagation tests
// ---------------------------------------------------------------------------

// TestCompareDigests_RequestWhenRemoteTombstoneNewer: remote digest has Deleted=true and newer version → request full payload
func TestCompareDigests_RequestWhenRemoteTombstoneNewer(t *testing.T) {
	// Local has a live record
	local := localUser("alice", 500, "node-local", "hash-live")
	local.Deleted = false
	store := newMemStore(local)
	s := NewSyncer(store, "node-local")

	d := digest("alice", 1000, "node-remote", "hash-tombstone")
	d.Deleted = true

	need, err := s.CompareDigests([]clusteruser.UserDigest{d})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "alice") {
		t.Error("expected alice in NeedFull list (remote tombstone is newer)")
	}
}

// TestCompareDigests_NoRequestWhenRemoteTombstoneOlder: remote tombstone is older → do not request
func TestCompareDigests_NoRequestWhenRemoteTombstoneOlder(t *testing.T) {
	local := localUser("bob", 2000, "node-local", "hash-current")
	store := newMemStore(local)
	s := NewSyncer(store, "node-local")

	d := digest("bob", 1000, "node-remote", "hash-tombstone")
	d.Deleted = true

	need, err := s.CompareDigests([]clusteruser.UserDigest{d})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if contains(need, "bob") {
		t.Error("bob should NOT be requested (remote tombstone is older)")
	}
}

// TestCompareDigests_RequestWhenSameVersionTombstoneHashMismatch: same version, tombstone, hash mismatch → request
func TestCompareDigests_RequestWhenSameVersionTombstoneHashMismatch(t *testing.T) {
	local := localUser("carol", 1000, "node-x", "hash-live")
	local.Deleted = false
	store := newMemStore(local)
	s := NewSyncer(store, "node-x")

	d := digest("carol", 1000, "node-x", "hash-tombstone-different")
	d.Deleted = true

	need, err := s.CompareDigests([]clusteruser.UserDigest{d})
	if err != nil {
		t.Fatalf("CompareDigests error: %v", err)
	}
	if !contains(need, "carol") {
		t.Error("expected carol in NeedFull list (same version, tombstone hash mismatch)")
	}
}

// TestUpsertFromRemote_PersistsTombstone: remote sends tombstone → local Deleted=true persisted
func TestUpsertFromRemote_PersistsTombstone(t *testing.T) {
	// Local has live record
	live := localUser("dave", 500, "node-local", "hash-live")
	live.Deleted = false
	store := newMemStore(live)
	s := NewSyncer(store, "node-local")

	tombstone := &clusteruser.ClusterUser{
		Username:    "dave",
		Password:    "pass",
		UpdatedAtUs: 1000, // newer
		OriginNode:  "node-remote",
		Hash:        "hash-tombstone",
		Deleted:     true,
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{tombstone}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	stored, _ := store.Get("dave")
	if stored == nil {
		t.Fatal("dave should still exist in store (as tombstone)")
	}
	if !stored.Deleted {
		t.Error("expected stored record to have Deleted=true after tombstone upsert")
	}
}

// TestUpsertFromRemote_TombstoneOlderThanLocal_NotApplied: remote tombstone older than live local → not applied
func TestUpsertFromRemote_TombstoneOlderThanLocal_NotApplied(t *testing.T) {
	live := localUser("eve", 2000, "node-local", "hash-current")
	live.Deleted = false
	live.Password = "current-pass"
	store := newMemStore(live)
	s := NewSyncer(store, "node-local")

	tombstone := &clusteruser.ClusterUser{
		Username:    "eve",
		Password:    "old-pass",
		UpdatedAtUs: 1000, // older
		OriginNode:  "node-remote",
		Hash:        "hash-tombstone",
		Deleted:     true,
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{tombstone}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	stored, _ := store.Get("eve")
	if stored == nil {
		t.Fatal("eve should still exist")
	}
	if stored.Deleted {
		t.Error("eve's local live record should NOT be overwritten by older tombstone")
	}
	if stored.Password != "current-pass" {
		t.Errorf("expected current-pass, got %q", stored.Password)
	}
}

// TestUpsertFromRemote_SameVersionHigherOriginNode: tie-break by OriginNode lex → higher wins
func TestUpsertFromRemote_SameVersionHigherOriginNode(t *testing.T) {
	local := localUser("ivan", 1000, "node-a", "hash-a") // lower lex
	local.Password = "pass-a"
	store := newMemStore(local)
	s := NewSyncer(store, "node-a")

	remote := &clusteruser.ClusterUser{
		Username:    "ivan",
		Password:    "pass-z",
		UpdatedAtUs: 1000,
		OriginNode:  "node-z", // higher lex → wins
		Hash:        "hash-z",
	}
	if err := s.UpsertFromRemote([]*clusteruser.ClusterUser{remote}); err != nil {
		t.Fatalf("UpsertFromRemote error: %v", err)
	}

	updated, _ := store.Get("ivan")
	if updated == nil || updated.Password != "pass-z" {
		t.Errorf("expected pass-z (node-z wins tie-break), got %v", updated)
	}
}
