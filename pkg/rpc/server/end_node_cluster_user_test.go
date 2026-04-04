package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	usync "github.com/lureiny/v2raymg/pkg/proxy/usermanager/sync"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ---------------------------------------------------------------------------
// In-memory NodeGroupsStore mock
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
// Helper: build a cluster-enabled server backed by UserManager
// ---------------------------------------------------------------------------

func newClusterEnabledServer(ngStore *testNGStore) *EndNodeServer {
	mgr := usermanager.NewUserManager(nil, "test-node")
	mgr.EnableClusterSync("default", ngStore)

	s := &EndNodeServer{}
	s.Name = "test-node"
	s.userMgr = mgr
	return s
}

func newClusterDisabledServer() *EndNodeServer {
	mgr := usermanager.NewUserManager(nil, "test-node")
	s := &EndNodeServer{}
	s.Name = "test-node"
	s.userMgr = mgr
	return s
}

// ---------------------------------------------------------------------------
// Disabled-mode tests
// ---------------------------------------------------------------------------

func TestUpsertClusterUsers_Disabled_Returns400(t *testing.T) {
	s := newClusterDisabledServer()
	rsp, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rsp.Code != 400 {
		t.Errorf("expected code=400, got %d", rsp.Code)
	}
}

// ---------------------------------------------------------------------------
// UpsertClusterUsers (peer sync via UserManager)
// ---------------------------------------------------------------------------

func TestUpsertClusterUsers_PeerSync_NewUser(t *testing.T) {
	s := newClusterEnabledServer(newTestNGStore("default"))

	now := time.Now().UnixMicro()
	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		Users: []*proto.ClusterUserSync{
			{User: &proto.User{Name: "alice", Passwd: "pw"}, UpdatedAtUs: now, OriginNode: "remote-node", Hash: "abc"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	u := s.userMgr.GetUserForSync("alice")
	if u == nil {
		t.Fatal("alice should exist after sync upsert")
	}
	if u.AuthToken != "pw" {
		t.Errorf("expected pw, got %q", u.AuthToken)
	}
}

func TestUpsertClusterUsers_PeerSync_OlderVersion_Skipped(t *testing.T) {
	s := newClusterEnabledServer(newTestNGStore("default"))

	// Add a user locally with a newer timestamp
	_ = s.userMgr.AddUser(usermanager.AddUserRequest{Username: "bob", Password: "local-pw"})
	local := s.userMgr.GetUserForSync("bob")
	if local == nil {
		t.Fatal("bob should exist")
	}

	// Try to sync an older version
	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		Users: []*proto.ClusterUserSync{
			{User: &proto.User{Name: "bob", Passwd: "old-pw"}, UpdatedAtUs: local.UpdatedAtUs - 1, OriginNode: "remote"},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	// Password should remain unchanged
	u := s.userMgr.GetUserForSync("bob")
	if u.AuthToken == "old-pw" {
		t.Errorf("older sync should have been skipped, but auth_token was overwritten to old-pw")
	}
}

func TestUpsertClusterUsers_PeerSync_Tombstone(t *testing.T) {
	s := newClusterEnabledServer(newTestNGStore("default"))

	// Add a user locally
	_ = s.userMgr.AddUser(usermanager.AddUserRequest{Username: "carol", Password: "pw"})

	now := time.Now().UnixMicro() + 1000 // newer than local
	incoming := &contracts.User{}
	incoming.Username = "carol"
	incoming.MarkDeleting()
	incoming.UpdatedAtUs = now
	incoming.OriginNode = "remote"
	incoming.Hash = usync.ComputeHash(incoming)

	_, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{
		Users: []*proto.ClusterUserSync{
			{User: &proto.User{Name: "carol"}, Deleted: true, UpdatedAtUs: now, OriginNode: "remote", Hash: incoming.Hash},
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	u := s.userMgr.GetUserForSync("carol")
	if u == nil {
		t.Fatal("carol should still exist as tombstone")
	}
	if !u.IsDeleting() {
		t.Error("expected carol to be in deleting state")
	}
}

// ---------------------------------------------------------------------------
// GetNodeGroups / SetNodeGroups
// ---------------------------------------------------------------------------

func TestGetNodeGroups_EmptyStore_ReturnDefault(t *testing.T) {
	s := newClusterEnabledServer(newTestNGStore()) // empty groups

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

func TestSetNodeGroups_StoresGroups(t *testing.T) {
	ngStore := newTestNGStore()
	s := newClusterEnabledServer(ngStore)

	_, err := s.SetNodeGroups(context.Background(), &proto.SetNodeGroupsReq{Groups: []string{"hk", "sg"}})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	groups, _ := ngStore.List()
	if len(groups) != 2 {
		t.Errorf("expected 2 groups stored, got %v", groups)
	}
}
