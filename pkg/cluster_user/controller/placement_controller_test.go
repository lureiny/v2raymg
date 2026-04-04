package controller

import (
	"sync"
	"testing"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// ---------------------------------------------------------------------------
// In-memory mock stores
// ---------------------------------------------------------------------------

type memClusterUserStore struct {
	mu    sync.RWMutex
	users map[string]*clusteruser.ClusterUser
}

func newMemClusterUserStore(initial ...*clusteruser.ClusterUser) *memClusterUserStore {
	s := &memClusterUserStore{users: make(map[string]*clusteruser.ClusterUser)}
	for _, u := range initial {
		s.users[u.Username] = u
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

func newMemNodeGroupsStore(groups ...string) *memNodeGroupsStore {
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
// Mock UserManager
// ---------------------------------------------------------------------------

type mockUserMgr struct {
	mu      sync.Mutex
	users   map[string]*contracts.User
	added   []string
	removed []string
	updated []string
}

func newMockUserMgr(initial ...*contracts.User) *mockUserMgr {
	m := &mockUserMgr{users: make(map[string]*contracts.User)}
	for _, u := range initial {
		u2 := *u
		m.users[u.Username] = &u2
	}
	return m
}

func (m *mockUserMgr) ListUsers() []*contracts.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*contracts.User, 0, len(m.users))
	for _, u := range m.users {
		u2 := *u
		out = append(out, &u2)
	}
	return out
}

func (m *mockUserMgr) AddUser(req usermanager.AddUserRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := &contracts.User{Username: req.Username, Password: req.Password}
	if req.TTL > 0 {
		u.ExpiryTime = time.Now().Add(req.TTL)
	}
	m.users[req.Username] = u
	m.added = append(m.added, req.Username)
	return nil
}

func (m *mockUserMgr) UpdateUser(username, password string, expireTime int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[username]
	if !ok {
		return nil
	}
	if password != "" {
		u.Password = password
	}
	if expireTime > 0 {
		u.ExpiryTime = time.Unix(expireTime, 0)
	}
	m.updated = append(m.updated, username)
	return nil
}

func (m *mockUserMgr) RemoveUser(req usermanager.RemoveUserRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, req.Username)
	m.removed = append(m.removed, req.Username)
	return nil
}

func (m *mockUserMgr) hasAdded(username string) bool {
	for _, v := range m.added {
		if v == username {
			return true
		}
	}
	return false
}

func (m *mockUserMgr) hasRemoved(username string) bool {
	for _, v := range m.removed {
		if v == username {
			return true
		}
	}
	return false
}

func (m *mockUserMgr) hasUpdated(username string) bool {
	for _, v := range m.updated {
		if v == username {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newTestController(
	cuStore *memClusterUserStore,
	ngStore *memNodeGroupsStore,
	mgr *mockUserMgr,
	defaultGroup string,
) *PlacementController {
	cfg := appconfig.ClusterUserConfig{
		SyncIntervalSec: 5,
		DefaultGroup:    defaultGroup,
	}
	return New(cuStore, ngStore, mgr, cfg)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestReconcile_AddUser: ClusterUser present, group matches, not in usermgr → Add
func TestReconcile_AddUser(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "alice",
		Password:    "pass1",
		TargetGroup: "default",
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("default")
	mgr := newMockUserMgr()

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasAdded("alice") {
		t.Error("expected alice to be added")
	}
	if mgr.hasRemoved("alice") {
		t.Error("alice should not be removed")
	}
}

// TestReconcile_RemoveUser_Deleted: deleted=true, user in usermgr → Remove
func TestReconcile_RemoveUser_Deleted(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "bob",
		Password:    "pass2",
		TargetGroup: "default",
		Deleted:     true,
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("default")
	mgr := newMockUserMgr(&contracts.User{Username: "bob", Password: "pass2"})

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasRemoved("bob") {
		t.Error("expected bob to be removed (deleted=true)")
	}
	if mgr.hasAdded("bob") {
		t.Error("bob should not be added")
	}
}

// TestReconcile_RemoveUser_GroupMismatch: group not assigned to node, user in usermgr → Remove
func TestReconcile_RemoveUser_GroupMismatch(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "carol",
		Password:    "pass3",
		TargetGroup: "group-B", // not in nodeGroups
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("group-A") // only group-A
	mgr := newMockUserMgr(&contracts.User{Username: "carol", Password: "pass3"})

	ctrl := newTestController(cuStore, ngStore, mgr, "group-A")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasRemoved("carol") {
		t.Error("expected carol to be removed (group mismatch)")
	}
}

// TestReconcile_UpdateUser: group matches, usermgr user exists but password differs → Update
func TestReconcile_UpdateUser(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "dave",
		Password:    "newpass",
		TargetGroup: "default",
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("default")
	// local user has old password
	mgr := newMockUserMgr(&contracts.User{Username: "dave", Password: "oldpass"})

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasUpdated("dave") {
		t.Error("expected dave to be updated (password mismatch)")
	}
	if mgr.hasRemoved("dave") {
		t.Error("dave should not be removed")
	}
}

// TestReconcile_NoOp_Consistent: group matches, user consistent → no Add/Update/Remove
func TestReconcile_NoOp_Consistent(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "eve",
		Password:    "samepass",
		Expire:      0,
		TargetGroup: "default",
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("default")
	// local user matches exactly
	mgr := newMockUserMgr(&contracts.User{Username: "eve", Password: "samepass"})

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if mgr.hasAdded("eve") {
		t.Error("eve should not be added (already exists)")
	}
	if mgr.hasUpdated("eve") {
		t.Error("eve should not be updated (consistent)")
	}
	if mgr.hasRemoved("eve") {
		t.Error("eve should not be removed")
	}
}

// TestReconcile_EmptyClusterUsers_DoesNotRemoveLocalUsers: cluster_users empty, local users exist → no Remove
func TestReconcile_EmptyClusterUsers_DoesNotRemoveLocalUsers(t *testing.T) {
	cuStore := newMemClusterUserStore() // empty
	ngStore := newMemNodeGroupsStore("default")
	mgr := newMockUserMgr(
		&contracts.User{Username: "alice", Password: "pass1"},
		&contracts.User{Username: "bob", Password: "pass2"},
	)

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if mgr.hasRemoved("alice") || mgr.hasRemoved("bob") {
		t.Error("cluster_users is empty: no local user should be removed")
	}
	if mgr.hasAdded("alice") || mgr.hasAdded("bob") {
		t.Error("cluster_users is empty: no add should happen either")
	}
}

// TestReconcile_EmptyClusterUsers_IsFullNoOp: cluster_users empty → no add/update/remove at all
func TestReconcile_EmptyClusterUsers_IsFullNoOp(t *testing.T) {
	cuStore := newMemClusterUserStore() // empty
	ngStore := newMemNodeGroupsStore("default")
	mgr := newMockUserMgr()

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if len(mgr.added) != 0 || len(mgr.removed) != 0 || len(mgr.updated) != 0 {
		t.Errorf("expected full no-op, got added=%v removed=%v updated=%v",
			mgr.added, mgr.removed, mgr.updated)
	}
}

// TestReconcile_ClusterUsersBecomeAvailable_AppliesDesiredState:
// First call empty → no-op; after adding cluster users second call applies desired state.
func TestReconcile_ClusterUsersBecomeAvailable_AppliesDesiredState(t *testing.T) {
	cuStore := newMemClusterUserStore() // initially empty
	ngStore := newMemNodeGroupsStore("default")
	mgr := newMockUserMgr()

	ctrl := newTestController(cuStore, ngStore, mgr, "default")

	// First reconcile: cluster_users empty → no-op
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("first Reconcile() error: %v", err)
	}
	if len(mgr.added) != 0 {
		t.Errorf("first reconcile should be no-op, got adds: %v", mgr.added)
	}

	// Now populate cluster_users
	_ = cuStore.Upsert(&clusteruser.ClusterUser{
		Username:    "grace",
		Password:    "pass7",
		TargetGroup: "default",
	})

	// Second reconcile: cluster_users non-empty → add grace
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("second Reconcile() error: %v", err)
	}
	if !mgr.hasAdded("grace") {
		t.Error("expected grace to be added after cluster_users became non-empty")
	}
}

// TestReconcile_UpdateUser_ExpireChange: local user has no expiry but cluster user has expiry → update
func TestReconcile_UpdateUser_ExpireChange(t *testing.T) {
	expire := time.Now().Add(24 * time.Hour).Unix()
	cu := &clusteruser.ClusterUser{
		Username:    "henry",
		Password:    "pass",
		Expire:      expire,
		TargetGroup: "default",
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("default")
	// local user exists with same password but no expiry
	mgr := newMockUserMgr(&contracts.User{Username: "henry", Password: "pass"})

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasUpdated("henry") {
		t.Error("expected henry to be updated (expire changed)")
	}
	if mgr.hasRemoved("henry") {
		t.Error("henry should not be removed")
	}
}

// TestReconcile_RemoveUser_DeletedAndGroupMismatch_Priority:
// user satisfies both deleted=true and group mismatch → removed exactly once
func TestReconcile_RemoveUser_DeletedAndGroupMismatch_Priority(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "ivan",
		Password:    "pass",
		TargetGroup: "group-B", // not assigned to this node
		Deleted:     true,
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore("group-A")
	mgr := newMockUserMgr(&contracts.User{Username: "ivan", Password: "pass"})

	ctrl := newTestController(cuStore, ngStore, mgr, "group-A")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	count := 0
	for _, v := range mgr.removed {
		if v == "ivan" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected ivan removed exactly once, got %d removals", count)
	}
}

// TestReconcile_EmptyNodeGroups: nodeGroups empty → treated as [defaultGroup]
func TestReconcile_EmptyNodeGroups(t *testing.T) {
	cu := &clusteruser.ClusterUser{
		Username:    "frank",
		Password:    "pass6",
		TargetGroup: "default", // matches defaultGroup
	}
	cuStore := newMemClusterUserStore(cu)
	ngStore := newMemNodeGroupsStore() // empty
	mgr := newMockUserMgr()

	ctrl := newTestController(cuStore, ngStore, mgr, "default")
	if err := ctrl.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}
	if !mgr.hasAdded("frank") {
		t.Error("expected frank to be added (empty nodeGroups falls back to [defaultGroup])")
	}
}
