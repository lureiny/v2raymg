package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func TestResetUserTraffic_EmptyUsers(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.ResetUserTraffic(context.Background(), &proto.UserOpReq{})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Errorf("expected code=0 for empty users, got %d msg=%s", rsp.Code, rsp.Msg)
	}
}

func TestResetUserTraffic_EmptyUsername(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.ResetUserTraffic(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{{Name: ""}},
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for empty username, got %d", rsp.Code)
	}
}

func TestResetUserTraffic_UserNotFound(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.ResetUserTraffic(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{{Name: "nonexistent"}},
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for unknown user, got %d", rsp.Code)
	}
}

func TestResetUserTraffic_Success(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := s.userMgr.SetUserTrafficTotalForTest("alice", 1000, 2000); err != nil {
		t.Fatalf("SetUserTrafficTotalForTest: %v", err)
	}

	rsp, err := s.ResetUserTraffic(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{{Name: "alice"}},
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}

	got, _ := s.userMgr.GetUser("alice")
	if got.TrafficTotalUplink != 0 || got.TrafficTotalDownlink != 0 {
		t.Errorf("expected zero totals after reset, got uplink=%d downlink=%d",
			got.TrafficTotalUplink, got.TrafficTotalDownlink)
	}
}

// TestResetUserTraffic_StopsOnFirstFailure documents the batch semantics the
// RPC handler inherited from ResetUser: on the first failure the loop returns
// immediately, leaving any unprocessed users untouched — and any earlier
// already-reset users stay reset (no rollback). Callers that fan out to
// multiple nodes or need per-user success granularity should issue one request
// per user.
func TestResetUserTraffic_StopsOnFirstFailure(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := s.userMgr.SetUserTrafficTotalForTest("alice", 500, 0); err != nil {
		t.Fatalf("SetUserTrafficTotalForTest: %v", err)
	}

	// First user is unknown → request short-circuits and leaves alice alone.
	rsp, err := s.ResetUserTraffic(context.Background(), &proto.UserOpReq{
		Users: []*proto.User{{Name: "ghost"}, {Name: "alice"}},
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 when first user fails, got %d", rsp.Code)
	}

	got, _ := s.userMgr.GetUser("alice")
	if got.TrafficTotalUplink != 500 {
		t.Errorf("alice's counter should be untouched when earlier user errored, got %d", got.TrafficTotalUplink)
	}
}
