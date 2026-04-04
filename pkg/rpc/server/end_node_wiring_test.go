package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ---------------------------------------------------------------------------
// Startup wiring: clusterUserEnabled flag propagation
// ---------------------------------------------------------------------------

// TestEndNodeServer_ClusterUserDisabledByDefault:
// A freshly created EndNodeServer (no InitClusterUser call) has cluster_user disabled.
// All cluster-user gRPC handlers should return code=400 immediately.
func TestEndNodeServer_ClusterUserDisabledByDefault(t *testing.T) {
	s := &EndNodeServer{}
	// No InitClusterUser() call — simulates cfg.ClusterUser.Enabled=false wiring path.

	if s.clusterUserEnabled {
		t.Fatal("clusterUserEnabled should be false by default")
	}

	rspList, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{})
	if err != nil || rspList.Code != 400 {
		t.Errorf("ListClusterUsers: expected code=400 when disabled, got code=%d err=%v", rspList.Code, err)
	}

	rspUpsert, err := s.UpsertClusterUsers(context.Background(), &proto.UpsertClusterUsersReq{})
	if err != nil || rspUpsert.Code != 400 {
		t.Errorf("UpsertClusterUsers: expected code=400 when disabled, got code=%d err=%v", rspUpsert.Code, err)
	}

	rspDel, err := s.DeleteClusterUsers(context.Background(), &proto.DeleteClusterUsersReq{})
	if err != nil || rspDel.Code != 400 {
		t.Errorf("DeleteClusterUsers: expected code=400 when disabled, got code=%d err=%v", rspDel.Code, err)
	}

	rspNG, err := s.GetNodeGroups(context.Background(), &proto.GetNodeGroupsReq{})
	if err != nil || rspNG.Code != 400 {
		t.Errorf("GetNodeGroups: expected code=400 when disabled, got code=%d err=%v", rspNG.Code, err)
	}
}

// TestEndNodeServer_InitClusterUser_EnablesFeature:
// After InitClusterUser(true, ...) the flag is set and handlers work.
func TestEndNodeServer_InitClusterUser_EnablesFeature(t *testing.T) {
	cuStore := newTestCUStore()
	ngStore := newTestNGStore("default")
	s := &EndNodeServer{}
	s.Name = "node-wiring-test"

	// Simulate cfg.ClusterUser.Enabled=true wiring: call InitClusterUser
	s.InitClusterUser(true, cuStore, ngStore, nil)

	if !s.clusterUserEnabled {
		t.Fatal("clusterUserEnabled should be true after InitClusterUser(true, ...)")
	}

	// ListClusterUsers should work (no 400)
	rsp, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{})
	if err != nil {
		t.Fatalf("ListClusterUsers error: %v", err)
	}
	if rsp.Code == 400 {
		t.Errorf("ListClusterUsers: got code=400 after feature was enabled")
	}
}

// TestEndNodeServer_InitClusterUser_FlagFalse_DisablesFeature:
// InitClusterUser(false, ...) keeps feature disabled even with stores injected.
func TestEndNodeServer_InitClusterUser_FlagFalse_DisablesFeature(t *testing.T) {
	cuStore := newTestCUStore()
	ngStore := newTestNGStore("default")
	s := &EndNodeServer{}
	s.InitClusterUser(false, cuStore, ngStore, nil)

	if s.clusterUserEnabled {
		t.Fatal("clusterUserEnabled should remain false when InitClusterUser(false,...)")
	}

	rsp, err := s.ListClusterUsers(context.Background(), &proto.ListClusterUsersReq{})
	if err != nil || rsp.Code != 400 {
		t.Errorf("expected code=400 for disabled feature, got code=%d err=%v", rsp.Code, err)
	}
}
