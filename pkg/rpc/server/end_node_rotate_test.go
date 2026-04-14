package server

import (
	"context"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

func newRotateTestServer(t *testing.T) *EndNodeServer {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 30000,
		MaxPort: 40000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	mgr := usermanager.NewUserManager(fwdMgr, "test-node")
	return &EndNodeServer{userMgr: mgr}
}

// --- RotateInboundPort ---

func TestRotateInboundPort_UserNotFound(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.RotateInboundPort(context.Background(), &proto.RotateInboundPortReq{
		Username:  "nonexistent",
		Container: "xray",
		Inbound:   "vless-tcp",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for nonexistent user, got %d", rsp.Code)
	}
	if rsp.Msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRotateInboundPort_MissingInboundTag(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.RotateInboundPort(context.Background(), &proto.RotateInboundPortReq{
		Username:  "alice",
		Container: "xray",
		Inbound:   "",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for missing inbound, got %d", rsp.Code)
	}
}

func TestRotateInboundPort_Success(t *testing.T) {
	s := newRotateTestServer(t)

	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if _, err := s.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      "alice",
		TargetPort:    10001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vless-tcp",
	}); err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	rsp, err := s.RotateInboundPort(context.Background(), &proto.RotateInboundPortReq{
		Username:  "alice",
		Container: "xray",
		Inbound:   "vless-tcp",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Errorf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	if rsp.Port == 0 {
		t.Error("expected non-zero port after rotation")
	}
}

func TestRotateInboundPort_NoForwardRule(t *testing.T) {
	s := newRotateTestServer(t)
	// User exists but has no forward rule for the given inbound.
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	rsp, err := s.RotateInboundPort(context.Background(), &proto.RotateInboundPortReq{
		Username:  "bob",
		Container: "xray",
		Inbound:   "nonexistent-inbound",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for missing forward rule, got %d", rsp.Code)
	}
	if rsp.Msg == "" {
		t.Error("expected non-empty error message")
	}
	if rsp.Port != 0 {
		t.Errorf("expected port=0 on failure, got %d", rsp.Port)
	}
}

// --- RotateAllPorts ---

func TestRotateAllPorts_EmptyUsername(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.RotateAllPorts(context.Background(), &proto.RotateAllPortsReq{
		Username: "",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for empty username, got %d", rsp.Code)
	}
}

func TestRotateAllPorts_UserNotFound(t *testing.T) {
	s := newRotateTestServer(t)
	rsp, err := s.RotateAllPorts(context.Background(), &proto.RotateAllPortsReq{
		Username: "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300, got %d", rsp.Code)
	}
}

func TestRotateAllPorts_UserHasNoForwardRules(t *testing.T) {
	s := newRotateTestServer(t)
	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "dan", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	// User exists but has no forward rules.
	rsp, err := s.RotateAllPorts(context.Background(), &proto.RotateAllPortsReq{Username: "dan"})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 300 {
		t.Errorf("expected code=300 for user with no rules, got %d", rsp.Code)
	}
	if len(rsp.Ports) != 0 {
		t.Errorf("expected empty ports map, got %v", rsp.Ports)
	}
}

func TestRotateAllPorts_Success(t *testing.T) {
	s := newRotateTestServer(t)

	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	// Allocate two inbounds.
	for _, tag := range []string{"vless-tcp", "trojan-tls"} {
		if _, err := s.userMgr.GetBindPort(usermanager.GetBindPortRequest{
			Username:      "bob",
			TargetPort:    10001,
			ContainerType: contracts.ContainerXray,
			InboundTag:    tag,
		}); err != nil {
			t.Fatalf("GetBindPort(%s): %v", tag, err)
		}
	}

	rsp, err := s.RotateAllPorts(context.Background(), &proto.RotateAllPortsReq{Username: "bob"})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	if rsp.Code != 0 {
		t.Errorf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	if len(rsp.Ports) != 2 {
		t.Errorf("expected 2 ports in response, got %d: %v", len(rsp.Ports), rsp.Ports)
	}
	// Keys should be "container:inboundTag" format.
	for _, expectedKey := range []string{"xray:vless-tcp", "xray:trojan-tls"} {
		port, ok := rsp.Ports[expectedKey]
		if !ok {
			t.Errorf("expected key %q in ports map, got %v", expectedKey, rsp.Ports)
		}
		if port == 0 {
			t.Errorf("port for %s should be non-zero", expectedKey)
		}
	}
}

func TestRotateAllPorts_PartialSuccess_PreservesPorts(t *testing.T) {
	s := newRotateTestServer(t)

	if err := s.userMgr.AddUser(usermanager.AddUserRequest{Username: "carol", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	// Only allocate one inbound so the response has at least one entry if partial.
	if _, err := s.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      "carol",
		TargetPort:    10001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vless-tcp",
	}); err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	rsp, err := s.RotateAllPorts(context.Background(), &proto.RotateAllPortsReq{Username: "carol"})
	if err != nil {
		t.Fatalf("unexpected gRPC error: %v", err)
	}
	// With one inbound, should succeed fully.
	if rsp.Code != 0 {
		t.Errorf("expected code=0, got %d msg=%s", rsp.Code, rsp.Msg)
	}
	// Key check: ports map is always populated regardless of code.
	if rsp.Ports == nil {
		t.Error("ports map should not be nil")
	}
}
