package usermanager

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// newTestManagerWithForward creates a UserManager with a real ForwardManager for rotate tests.
func newTestManagerWithForward(t *testing.T) (*UserManager, *forward.DefaultForwardManager) {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 30000,
		MaxPort: 40000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	mgr := NewUserManager(fwdMgr)
	return mgr, fwdMgr
}

func TestRotateUserPort_UserNotFound(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)
	err := mgr.RotateUserPort("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestRotateUserPort_Success(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)

	// Add user
	if err := mgr.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Allocate an initial port
	port1, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    10001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vless-in",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}
	if port1 == 0 {
		t.Fatal("expected non-zero port1")
	}

	// Rotate port
	if err := mgr.RotateUserPort("alice"); err != nil {
		t.Fatalf("RotateUserPort: %v", err)
	}

	// New port should differ from old port
	port2, ok := mgr.GetUserPortByDst("alice", 10001)
	if !ok {
		t.Fatal("expected port mapping after rotation")
	}
	if port2 == 0 {
		t.Fatal("expected non-zero port2 after rotation")
	}
	if port2 == port1 {
		t.Errorf("port should have changed after rotation: port1=%d port2=%d", port1, port2)
	}
}

func TestRotateUserPort_SubscriptionRealtimeUpdate(t *testing.T) {
	// This test verifies that GetUserPortByDst (used by subscription generation)
	// immediately reflects the new port after RotateUserPort.
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Allocate initial port
	port1, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "bob",
		TargetPort:    10002,
		ContainerType: contracts.ContainerSnell,
		InboundTag:    "snell-default",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	// Simulate subscription read before rotation
	pre, ok := mgr.GetUserPortByDst("bob", 10002)
	if !ok || pre != port1 {
		t.Fatalf("pre-rotation subscription port mismatch: got %d want %d", pre, port1)
	}

	// Rotate
	if err := mgr.RotateUserPort("bob"); err != nil {
		t.Fatalf("RotateUserPort: %v", err)
	}

	// Subscription read immediately after rotation should return new port
	post, ok := mgr.GetUserPortByDst("bob", 10002)
	if !ok {
		t.Fatal("subscription: port mapping missing after rotation")
	}
	if post == port1 {
		t.Errorf("subscription not updated: still returns old port %d", post)
	}
	if post == 0 {
		t.Error("subscription returned zero port after rotation")
	}
}

func TestRotateUserPort_MultipleRules(t *testing.T) {
	// User has ports on two different inbounds; both should rotate.
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "carol", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	port1a, err := mgr.GetBindPort(GetBindPortRequest{
		Username: "carol", TargetPort: 10003,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-a",
	})
	if err != nil {
		t.Fatalf("GetBindPort inbound-a: %v", err)
	}

	port1b, err := mgr.GetBindPort(GetBindPortRequest{
		Username: "carol", TargetPort: 10004,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-b",
	})
	if err != nil {
		t.Fatalf("GetBindPort inbound-b: %v", err)
	}

	// Rotate all ports
	if err := mgr.RotateUserPort("carol"); err != nil {
		t.Fatalf("RotateUserPort: %v", err)
	}

	port2a, okA := mgr.GetUserPortByDst("carol", 10003)
	port2b, okB := mgr.GetUserPortByDst("carol", 10004)

	if !okA || !okB {
		t.Fatal("port mapping missing after rotation")
	}
	if port2a == port1a {
		t.Errorf("inbound-a port unchanged: %d", port1a)
	}
	if port2b == port1b {
		t.Errorf("inbound-b port unchanged: %d", port1b)
	}
}
