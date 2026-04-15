package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func TestResetAuthToken_EmptyUsername(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for empty username, got %d", rsp.Code)
	}
	if rsp.Msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestResetAuthToken_UserNotFound(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for nonexistent user, got %d", rsp.Code)
	}
}

func TestResetAuthToken_AutoGenerate(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	alice, _ := s.userMgr.GetUser("alice")
	oldToken := alice.AuthToken

	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	if rsp.AuthToken == "" {
		t.Fatal("expected non-empty auth_token in response")
	}
	if rsp.AuthToken == oldToken {
		t.Error("expected new token to differ from old token")
	}
}

func TestResetAuthToken_WithSpecificToken(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	specificToken := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "bob",
		NewToken: specificToken,
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Fatalf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	if rsp.AuthToken != specificToken {
		t.Errorf("expected token=%s, got %s", specificToken, rsp.AuthToken)
	}
}

func TestResetAuthToken_InvalidUUID(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "carol", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "carol",
		NewToken: "not-a-valid-uuid",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for invalid UUID, got %d", rsp.Code)
	}
}

func TestResetAuthToken_TokenConflict(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "dave", Password: "pass"}); err != nil {
		t.Fatalf("AddUser dave: %v", err)
	}
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "eve", Password: "pass"}); err != nil {
		t.Fatalf("AddUser eve: %v", err)
	}

	dave, _ := s.userMgr.GetUser("dave")

	rsp, err := s.ResetAuthToken(context.Background(), &proto.ResetAuthTokenReq{
		Username: "eve",
		NewToken: dave.AuthToken,
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for token conflict, got %d", rsp.Code)
	}
}
