package server

import (
	"context"
	"sync"
	"testing"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ---------------------------------------------------------------------------
// In-memory store mocks
// ---------------------------------------------------------------------------

type testCUStore struct {
	mu    sync.RWMutex
	users map[string]*clusteruser.ClusterUser
}

func newTestCUStore(initial ...*clusteruser.ClusterUser) *testCUStore {
	s := &testCUStore{users: make(map[string]*clusteruser.ClusterUser)}
	for _, u := range initial {
		u2 := *u
		s.users[u.Username] = &u2
	}
	return s
}

func (s *testCUStore) List() ([]*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*clusteruser.ClusterUser, 0, len(s.users))
	for _, u := range s.users {
		u2 := *u
		out = append(out, &u2)
	}
	return out, nil
}

func (s *testCUStore) ListByGroup(group string) ([]*clusteruser.ClusterUser, error) {
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

func (s *testCUStore) Get(username string) (*clusteruser.ClusterUser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[username]
	if !ok {
		return nil, nil
	}
	u2 := *u
	return &u2, nil
}

func (s *testCUStore) Upsert(u *clusteruser.ClusterUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u2 := *u
	s.users[u.Username] = &u2
	return nil
}

func (s *testCUStore) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users), nil
}

func (s *testCUStore) Close() error { return nil }

// ---------------------------------------------------------------------------

type testNGStore struct {
	mu     sync.RWMutex
	groups []string
}

func newTestNGStore(groups ...string) *testNGStore {
	return &testNGStore{groups: groups}
}

func (s *testNGStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.groups))
	copy(out, s.groups)
	return out, nil
}

func (s *testNGStore) Set(groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.groups = make([]string, len(groups))
	copy(s.groups, groups)
	return nil
}

func (s *testNGStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// Helper: build a minimal server with cluster user enabled
// ---------------------------------------------------------------------------

func newEnabledServer(cuStore *testCUStore, ngStore *testNGStore) *EndNodeServer {
	s := &EndNodeServer{}
	s.Name = "test-node"
	s.InitClusterUser(true, cuStore, ngStore, nil)
	return s
}

func newDisabledServer() *EndNodeServer {
	s := &EndNodeServer{}
	s.Name = "test-node"
	// clusterUserEnabled is false by default — no InitClusterUser call
	return s
}

// ---------------------------------------------------------------------------
// Disabled-mode tests
// ---------------------------------------------------------------------------

func TestListClusterUsers_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

func TestGetClusterUsersByName_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.GetClusterUsersByName(context.Background(), &proto.GetClusterUsersByNameReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

func TestUpsertClusterUsers_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

func TestDeleteClusterUsers_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.DeleteClusterUsers(context.Background(), &proto.DeleteClusterUsersReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

func TestGetNodeGroups_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.GetNodeGroups(context.Background(), &proto.GetNodeGroupsReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

func TestSetNodeGroups_Disabled_Returns400(t *testing.T) {
	s := newDisabledServer()
	rsp, err := s.SetNodeGroups(context.Background(), &proto.SetNodeGroupsReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

// ---------------------------------------------------------------------------
// ListClusterUsers
// ---------------------------------------------------------------------------

// TestListClusterUsers_FiltersTombstones: deleted records are not returned to API callers
func TestListClusterUsers_FiltersTombstones(t *testing.T) {
	live := &clusteruser.ClusterUser{Username: "alice", Password: "pw", TargetGroup: "default", Deleted: false}
	dead := &clusteruser.ClusterUser{Username: "bob", Password: "pw", TargetGroup: "default", Deleted: true}
	s := newEnabledServer(newTestCUStore(live, dead), newTestNGStore("default"))

	rsp, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	if len(rsp.Users) != 1 || rsp.Users[0].Username != "alice" {
		t.Errorf("expected only alice (non-deleted), got %v", rsp.Users)
	}
}

// TestListClusterUsers_GroupFilter: group param filters by TargetGroup
func TestListClusterUsers_GroupFilter(t *testing.T) {
	a := &clusteruser.ClusterUser{Username: "alice", TargetGroup: "group-a"}
	b := &clusteruser.ClusterUser{Username: "bob", TargetGroup: "group-b"}
	s := newEnabledServer(newTestCUStore(a, b), newTestNGStore("group-a", "group-b"))

	rsp, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{Group: "group-a"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(rsp.Users) != 1 || rsp.Users[0].Username != "alice" {
		t.Errorf("expected only alice in group-a, got %v", rsp.Users)
	}
}

// ---------------------------------------------------------------------------
// GetClusterUsersByName
// ---------------------------------------------------------------------------

// TestGetClusterUsersByName_IncludesTombstones: tombstones are returned (for sync)
func TestGetClusterUsersByName_IncludesTombstones(t *testing.T) {
	dead := &clusteruser.ClusterUser{Username: "carol", Deleted: true, TargetGroup: "default"}
	s := newEnabledServer(newTestCUStore(dead), newTestNGStore())

	rsp, err := s.GetClusterUsersByName(context.Background(), &proto.GetClusterUsersByNameReq{Usernames: []string{"carol"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(rsp.Users) != 1 || !rsp.Users[0].Deleted {
		t.Errorf("expected tombstone carol to be returned, got %v", rsp.Users)
	}
}

// TestGetClusterUsersByName_SkipsMissingUser: unknown username → not included, no error
func TestGetClusterUsersByName_SkipsMissingUser(t *testing.T) {
	s := newEnabledServer(newTestCUStore(), newTestNGStore())

	rsp, err := s.GetClusterUsersByName(context.Background(), &proto.GetClusterUsersByNameReq{Usernames: []string{"nobody"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 0 {
		t.Errorf("expected code=0, got %d", rsp.Code)
	}
	if len(rsp.Users) != 0 {
		t.Errorf("expected empty result for unknown user, got %v", rsp.Users)
	}
}

// ---------------------------------------------------------------------------
// UpsertClusterUsers
// ---------------------------------------------------------------------------

// TestUpsertClusterUsers_AdminWrite_RMW: FromAdmin=true → admin write, preserves unset fields
func TestUpsertClusterUsers_AdminWrite_RMW(t *testing.T) {
	prior := &clusteruser.ClusterUser{
		Username: "dave", Password: "old-pass", Role: "normal",
		TargetGroup: "group-x", Expire: 9999,
		UpdatedAtUs: 1000, OriginNode: "prior-node",
	}
	cuStore := newTestCUStore(prior)
	s := newEnabledServer(cuStore, newTestNGStore())

	// Admin write: only update password, leave others unset
	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		FromAdmin: true,
		Users: []*proto.ClusterUserInfo{
			{Username: "dave", Password: "new-pass"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	stored, _ := cuStore.Get("dave")
	if stored == nil {
		t.Fatal("dave should exist after upsert")
	}
	if stored.Password != "new-pass" {
		t.Errorf("expected new-pass, got %q", stored.Password)
	}
	// Unset fields should be preserved from prior
	if stored.Role != "normal" {
		t.Errorf("expected preserved role=normal, got %q", stored.Role)
	}
	if stored.TargetGroup != "group-x" {
		t.Errorf("expected preserved target_group=group-x, got %q", stored.TargetGroup)
	}
	if stored.Expire != 9999 {
		t.Errorf("expected preserved expire=9999, got %d", stored.Expire)
	}
	// Version fields should be filled
	if stored.UpdatedAtUs == 0 {
		t.Error("expected UpdatedAtUs to be set by admin write")
	}
	if stored.OriginNode != "test-node" {
		t.Errorf("expected OriginNode=test-node, got %q", stored.OriginNode)
	}
}

// TestUpsertClusterUsers_AdminWrite_DefaultGroup: no prior, no group → defaults to "default"
func TestUpsertClusterUsers_AdminWrite_DefaultGroup(t *testing.T) {
	cuStore := newTestCUStore()
	s := newEnabledServer(cuStore, newTestNGStore())

	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		FromAdmin: true,
		Users: []*proto.ClusterUserInfo{
			{Username: "new-user", Password: "pw"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	stored, _ := cuStore.Get("new-user")
	if stored == nil {
		t.Fatal("new-user should exist")
	}
	if stored.TargetGroup != "default" {
		t.Errorf("expected default target_group, got %q", stored.TargetGroup)
	}
}

// TestUpsertClusterUsers_AdminWrite_NewUserMissingPassword: admin write, no prior, empty password → 400
func TestUpsertClusterUsers_AdminWrite_NewUserMissingPassword(t *testing.T) {
	cuStore := newTestCUStore() // empty — no prior user
	s := newEnabledServer(cuStore, newTestNGStore())

	rsp, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		FromAdmin: true,
		Users: []*proto.ClusterUserInfo{
			{Username: "no-pass-user"}, // Password=""
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400 for new user without password, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	// Verify user was not stored
	stored, _ := cuStore.Get("no-pass-user")
	if stored != nil {
		t.Error("expected user NOT to be stored when password missing")
	}
}

// TestUpsertClusterUsers_PeerWrite_VersionArbitration_Newer: peer write, newer version → stored
func TestUpsertClusterUsers_PeerWrite_VersionArbitration_Newer(t *testing.T) {
	prior := &clusteruser.ClusterUser{
		Username: "eve", Password: "old", UpdatedAtUs: 500, OriginNode: "node-a",
	}
	cuStore := newTestCUStore(prior)
	s := newEnabledServer(cuStore, newTestNGStore())

	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		Users: []*proto.ClusterUserInfo{
			{Username: "eve", Password: "new", UpdatedAtUs: 1000, OriginNode: "node-b"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	stored, _ := cuStore.Get("eve")
	if stored.Password != "new" {
		t.Errorf("expected new password after newer peer write, got %q", stored.Password)
	}
}

// TestUpsertClusterUsers_PeerWrite_VersionArbitration_Older: peer write, older version → skipped
func TestUpsertClusterUsers_PeerWrite_VersionArbitration_Older(t *testing.T) {
	prior := &clusteruser.ClusterUser{
		Username: "frank", Password: "current", UpdatedAtUs: 2000, OriginNode: "node-a",
	}
	cuStore := newTestCUStore(prior)
	s := newEnabledServer(cuStore, newTestNGStore())

	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		Users: []*proto.ClusterUserInfo{
			{Username: "frank", Password: "old", UpdatedAtUs: 1000, OriginNode: "node-b"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	stored, _ := cuStore.Get("frank")
	if stored.Password != "current" {
		t.Errorf("expected current password preserved (older peer write skipped), got %q", stored.Password)
	}
}

// ---------------------------------------------------------------------------
// DeleteClusterUsers
// ---------------------------------------------------------------------------

// TestDeleteClusterUsers_ExistingUser_CreatesTombstone: existing user → Deleted=true
func TestDeleteClusterUsers_ExistingUser_CreatesTombstone(t *testing.T) {
	live := &clusteruser.ClusterUser{
		Username: "grace", Password: "pw", TargetGroup: "default",
		UpdatedAtUs: 500, OriginNode: "node-a",
	}
	cuStore := newTestCUStore(live)
	s := newEnabledServer(cuStore, newTestNGStore())

	rsp, err := s.DeleteClusterUsers(context.Background(), &proto.DeleteClusterUsersReq{Usernames: []string{"grace"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}

	stored, _ := cuStore.Get("grace")
	if stored == nil {
		t.Fatal("grace should still exist as tombstone")
	}
	if !stored.Deleted {
		t.Error("expected Deleted=true after delete")
	}
	if stored.UpdatedAtUs <= 500 {
		t.Error("expected UpdatedAtUs to be updated after delete")
	}
}

// TestDeleteClusterUsers_UnknownUser_CreatesTombstone: user not stored locally → tombstone still written
func TestDeleteClusterUsers_UnknownUser_CreatesTombstone(t *testing.T) {
	cuStore := newTestCUStore() // empty
	s := newEnabledServer(cuStore, newTestNGStore())

	rsp, err := s.DeleteClusterUsers(context.Background(), &proto.DeleteClusterUsersReq{Usernames: []string{"henry"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}

	stored, _ := cuStore.Get("henry")
	if stored == nil {
		t.Fatal("henry should be created as tombstone even if previously unknown")
	}
	if !stored.Deleted {
		t.Error("expected Deleted=true")
	}
}

// TestDeleteClusterUsers_AlreadyDeleted_Idempotent: already tombstoned → no-op (UpdatedAtUs unchanged)
func TestDeleteClusterUsers_AlreadyDeleted_Idempotent(t *testing.T) {
	dead := &clusteruser.ClusterUser{
		Username: "ivan", Deleted: true, UpdatedAtUs: 1000, OriginNode: "node-a",
	}
	cuStore := newTestCUStore(dead)
	s := newEnabledServer(cuStore, newTestNGStore())

	_, err := s.DeleteClusterUsers(context.Background(), &proto.DeleteClusterUsersReq{Usernames: []string{"ivan"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	stored, _ := cuStore.Get("ivan")
	// Should not re-write (UpdatedAtUs should remain 1000 since the `continue` skips already-deleted)
	if stored.UpdatedAtUs != 1000 {
		t.Errorf("already-deleted user should not be re-written; expected UpdatedAtUs=1000, got %d", stored.UpdatedAtUs)
	}
}

// ---------------------------------------------------------------------------
// GetNodeGroups / SetNodeGroups
// ---------------------------------------------------------------------------

// TestGetNodeGroups_EmptyStore_ReturnDefault: no groups stored → ["default"]
func TestGetNodeGroups_EmptyStore_ReturnDefault(t *testing.T) {
	s := newEnabledServer(newTestCUStore(), newTestNGStore()) // empty groups

	rsp, err := s.GetNodeGroups(context.Background(), &proto.GetNodeGroupsReq{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d", rsp.Code)
	}
	if len(rsp.Groups) != 1 || rsp.Groups[0] != "default" {
		t.Errorf("expected [default], got %v", rsp.Groups)
	}
}

// TestGetNodeGroups_ReturnsStored: stored groups returned as-is
func TestGetNodeGroups_ReturnsStored(t *testing.T) {
	s := newEnabledServer(newTestCUStore(), newTestNGStore("group-a", "group-b"))

	rsp, err := s.GetNodeGroups(context.Background(), &proto.GetNodeGroupsReq{})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(rsp.Groups) != 2 {
		t.Errorf("expected 2 groups, got %v", rsp.Groups)
	}
}

// TestSetNodeGroups_StoresGroups: groups persisted and retrievable
func TestSetNodeGroups_StoresGroups(t *testing.T) {
	ngStore := newTestNGStore()
	s := newEnabledServer(newTestCUStore(), ngStore)

	_, err := s.SetNodeGroups(context.Background(), &proto.SetNodeGroupsReq{Groups: []string{"hk", "sg"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	groups, _ := ngStore.List()
	if len(groups) != 2 {
		t.Errorf("expected 2 groups stored, got %v", groups)
	}
}

// TestSetNodeGroups_EmptyInput_WritesDefault: empty groups input → ["default"] written
func TestSetNodeGroups_EmptyInput_WritesDefault(t *testing.T) {
	ngStore := newTestNGStore("existing")
	s := newEnabledServer(newTestCUStore(), ngStore)

	_, err := s.SetNodeGroups(context.Background(), &proto.SetNodeGroupsReq{Groups: []string{}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	groups, _ := ngStore.List()
	if len(groups) != 1 || groups[0] != "default" {
		t.Errorf("expected [default] for empty input, got %v", groups)
	}
}
