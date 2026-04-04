package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ---------------------------------------------------------------------------
// Startup wiring: cluster sync enabled/disabled propagation via UserManager
// ---------------------------------------------------------------------------

// TestEndNodeServer_ClusterDisabledByDefault:
// A freshly created EndNodeServer with a default UserManager has cluster sync disabled.
// Cluster gRPC handlers should return code=400 immediately.
func TestEndNodeServer_ClusterDisabledByDefault(t *testing.T) {
	s := &EndNodeServer{}
	s.userMgr = usermanager.NewUserManager(nil, "node-test")

	if s.userMgr.IsClusterEnabled() {
		t.Fatal("cluster should be disabled by default")
	}

	rspUpsert, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{})
	if err != nil || rspUpsert.Code != 400 {
		t.Errorf("UpsertClusterUsers: expected code=400 when disabled, got code=%d err=%v", rspUpsert.Code, err)
	}

	rspNG, err := s.GetNodeGroups(context.Background(), &proto.GetNodeGroupsReq{})
	if err != nil || rspNG.Code != 400 {
		t.Errorf("GetNodeGroups: expected code=400 when disabled, got code=%d err=%v", rspNG.Code, err)
	}
}

// TestEndNodeServer_ClusterEnabled:
// After enabling cluster sync via UserManager, handlers should work.
func TestEndNodeServer_ClusterEnabled(t *testing.T) {
	ngStore := newTestNGStore("default")
	mgr := usermanager.NewUserManager(nil, "node-test")
	mgr.EnableClusterSync("default", ngStore)

	s := &EndNodeServer{}
	s.Name = "node-test"
	s.userMgr = mgr

	if !s.userMgr.IsClusterEnabled() {
		t.Fatal("cluster should be enabled after EnableClusterSync")
	}

	// UpsertClusterUsers should not return 400 when enabled.
	rsp, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{})
	if err != nil {
		t.Fatalf("UpsertClusterUsers error: %v", err)
	}
	if rsp.Code == 400 {
		t.Errorf("UpsertClusterUsers: got code=400 after feature was enabled")
	}
}
