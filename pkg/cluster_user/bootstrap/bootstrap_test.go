package bootstrap

import (
	"context"
	"sync"
	"testing"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ---------------------------------------------------------------------------
// In-memory stores
// ---------------------------------------------------------------------------

type memClusterUserStore struct {
	mu    sync.RWMutex
	users map[string]*clusteruser.ClusterUser
}

func newMemCUStore(initial ...*clusteruser.ClusterUser) *memClusterUserStore {
	s := &memClusterUserStore{users: make(map[string]*clusteruser.ClusterUser)}
	for _, u := range initial {
		u2 := *u
		s.users[u.Username] = &u2
	}
	return s
}

func (s *memClusterUserStore) List() ([]*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*clusteruser.ClusterUser, 0, len(s.users))
	for _, u := range s.users {
		u2 := *u
		out = append(out, &u2)
	}
	return out, nil
}

func (s *memClusterUserStore) ListByGroup(group string) ([]*clusteruser.ClusterUser, error) {
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

func (s *memClusterUserStore) Get(username string) (*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return nil, nil
	}
	u2 := *u
	return &u2, nil
}

func (s *memClusterUserStore) Upsert(u *clusteruser.ClusterUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u2 := *u
	s.users[u.Username] = &u2
	return nil
}

func (s *memClusterUserStore) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users), nil
}

func (s *memClusterUserStore) Close() error { return nil }

// ---------------------------------------------------------------------------

type memNodeGroupsStore struct {
	mu     sync.RWMutex
	groups []string
}

func newMemNGStore(groups ...string) *memNodeGroupsStore {
	return &memNodeGroupsStore{groups: groups}
}

func (s *memNodeGroupsStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func (s *memNodeGroupsStore) Set(groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = make([]string, len(groups))
	copy(s.groups, groups)
	return nil
}

func (s *memNodeGroupsStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Mock UserLister
// ---------------------------------------------------------------------------

type mockUserLister struct {
	users []*contracts.User
}

func (m *mockUserLister) ListUsers() []*contracts.User {
	return m.users
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestBootstrap_InitializesDefaultNodeGroup: node groups empty → bootstrap writes default group
func TestBootstrap_InitializesDefaultNodeGroup(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore() // empty
	mgr := &mockUserLister{}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	groups, _ := ngStore.List()
	if len(groups) != 1 || groups[0] != "default" {
		t.Errorf("expected node groups = [default], got %v", groups)
	}
}

// TestBootstrap_NodeGroupsAlreadySet: node groups non-empty → not overwritten
func TestBootstrap_NodeGroupsAlreadySet(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("existing-group")
	mgr := &mockUserLister{}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	groups, _ := ngStore.List()
	if len(groups) != 1 || groups[0] != "existing-group" {
		t.Errorf("expected groups unchanged = [existing-group], got %v", groups)
	}
}

// TestBootstrap_ImportsLocalUsersWhenClusterUsersEmpty:
// cluster_users empty + usermgr has users → all imported
func TestBootstrap_ImportsLocalUsersWhenClusterUsersEmpty(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("default")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "alice", Password: "pa1"},
		{Username: "bob", Password: "pb2"},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	count, _ := cuStore.Count()
	if count != 2 {
		t.Errorf("expected 2 cluster users, got %d", count)
	}
	alice, _ := cuStore.Get("alice")
	if alice == nil || alice.Password != "pa1" {
		t.Error("alice not imported correctly")
	}
}

// TestBootstrap_SkipsImportWhenClusterUsersNotEmpty: cluster_users already populated → no import
func TestBootstrap_SkipsImportWhenClusterUsersNotEmpty(t *testing.T) {
	existing := &clusteruser.ClusterUser{
		Username: "preexisting", Password: "pw",
		TargetGroup: "default", UpdatedAtUs: 1000, OriginNode: "node-0",
	}
	cuStore := newMemCUStore(existing)
	ngStore := newMemNGStore("default")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "alice", Password: "pa1"},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	// alice should NOT have been imported
	alice, _ := cuStore.Get("alice")
	if alice != nil {
		t.Error("alice should not be imported when cluster_users is non-empty")
	}
	count, _ := cuStore.Count()
	if count != 1 {
		t.Errorf("expected count unchanged = 1, got %d", count)
	}
}

// TestBootstrap_FillsDefaultGroupForImportedUsers: imported users get the default group
func TestBootstrap_FillsDefaultGroupForImportedUsers(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("mygroup")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "carol", Password: "pc3"},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "mygroup")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	carol, _ := cuStore.Get("carol")
	if carol == nil {
		t.Fatal("carol not imported")
	}
	if carol.TargetGroup != "mygroup" {
		t.Errorf("expected TargetGroup=mygroup, got %q", carol.TargetGroup)
	}
}

// TestBootstrap_UpdatedAtUs_MonotonicallyIncreasing: imported users have unique timestamps
func TestBootstrap_UpdatedAtUs_MonotonicallyIncreasing(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("default")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "u1", Password: "p1"},
		{Username: "u2", Password: "p2"},
		{Username: "u3", Password: "p3"},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	seen := make(map[int64]string)
	for _, name := range []string{"u1", "u2", "u3"} {
		u, _ := cuStore.Get(name)
		if u == nil {
			t.Fatalf("%s not imported", name)
		}
		if prev, dup := seen[u.UpdatedAtUs]; dup {
			t.Errorf("duplicate UpdatedAtUs %d on %s and %s", u.UpdatedAtUs, prev, name)
		}
		seen[u.UpdatedAtUs] = name
	}
}

// TestBootstrap_NoUserMgr_NoImport: userMgr nil → no panic, no import
func TestBootstrap_NoUserMgr_NoImport(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("default")

	b := NewBootstrapper(cuStore, ngStore, nil, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	count, _ := cuStore.Count()
	if count != 0 {
		t.Errorf("expected no users imported (nil userMgr), got %d", count)
	}
}

// TestBootstrap_SetsOriginNode: imported users have correct OriginNode
func TestBootstrap_SetsOriginNode(t *testing.T) {
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("default")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "dave", Password: "pd4"},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "my-node", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	dave, _ := cuStore.Get("dave")
	if dave == nil {
		t.Fatal("dave not imported")
	}
	if dave.OriginNode != "my-node" {
		t.Errorf("expected OriginNode=my-node, got %q", dave.OriginNode)
	}
}

// TestBootstrap_PreservesExpiryTime: user with expiry → imported Expire matches
func TestBootstrap_PreservesExpiryTime(t *testing.T) {
	expiry := time.Now().Add(7 * 24 * time.Hour)
	cuStore := newMemCUStore()
	ngStore := newMemNGStore("default")
	mgr := &mockUserLister{users: []*contracts.User{
		{Username: "eve", Password: "pe5", ExpiryTime: expiry},
	}}

	b := NewBootstrapper(cuStore, ngStore, mgr, "node-1", "default")
	if err := b.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	eve, _ := cuStore.Get("eve")
	if eve == nil {
		t.Fatal("eve not imported")
	}
	if eve.Expire != expiry.Unix() {
		t.Errorf("expected Expire=%d, got %d", expiry.Unix(), eve.Expire)
	}
}
