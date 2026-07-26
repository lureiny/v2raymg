package usermanager

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestRotateUserPortForInbound_UserNotFound(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)
	_, err := mgr.RotateUserPortForInbound("ghost", contracts.ContainerXray, "vless-in", 0)
	if err == nil {
		t.Fatal("expected error for nonexistent user")
	}
}

func TestRotateUserPortForInbound_NoRule(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)
	if err := mgr.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	// No forward rule created yet — should error
	_, err := mgr.RotateUserPortForInbound("alice", contracts.ContainerXray, "vless-in", 0)
	if err == nil {
		t.Fatal("expected error when no forward rule exists")
	}
}

func TestRotateUserPortForInbound_Success(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "alice", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Allocate initial port
	port1, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "alice",
		TargetPort:    10001,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vless-in",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	// Rotate only this inbound
	newPort, err := mgr.RotateUserPortForInbound("alice", contracts.ContainerXray, "vless-in", 0)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound: %v", err)
	}
	if newPort == 0 {
		t.Fatal("expected non-zero new port")
	}
	if newPort == port1 {
		t.Errorf("port should have changed: was %d, still %d", port1, newPort)
	}

	// PortMappings must reflect the new port
	got, ok := mgr.GetUserPortByDst("alice", 10001)
	if !ok {
		t.Fatal("port mapping missing after rotation")
	}
	if got != newPort {
		t.Errorf("PortMappings: got %d, want %d", got, newPort)
	}
}

func TestRotateUserPortForInbound_OldPortReleased(t *testing.T) {
	mgr, fwdMgr := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "bob", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	port1, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "bob",
		TargetPort:    10002,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "trojan-in",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	newPort, err := mgr.RotateUserPortForInbound("bob", contracts.ContainerXray, "trojan-in", 0)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound: %v", err)
	}

	// Old rule must no longer exist in ForwardManager
	oldRuleKey := "xray:trojan-in:bob"
	rule := fwdMgr.GetRule(oldRuleKey)
	if rule != nil && rule.ListenPort == port1 {
		t.Errorf("old rule with port %d still exists after rotation", port1)
	}

	// New rule must exist with the new port
	if rule == nil {
		t.Fatal("new rule not found after rotation")
	}
	if rule.ListenPort != newPort {
		t.Errorf("new rule port: got %d, want %d", rule.ListenPort, newPort)
	}
}

func TestRotateUserPortForInbound_SubscriptionUpdate(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "carol", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	port1, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "carol",
		TargetPort:    10003,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vmess-in",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	pre, ok := mgr.GetUserPortByDst("carol", 10003)
	if !ok || pre != port1 {
		t.Fatalf("pre-rotation subscription mismatch: got %d want %d", pre, port1)
	}

	newPort, err := mgr.RotateUserPortForInbound("carol", contracts.ContainerXray, "vmess-in", 0)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound: %v", err)
	}

	post, ok := mgr.GetUserPortByDst("carol", 10003)
	if !ok {
		t.Fatal("port mapping missing after rotation")
	}
	if post != newPort {
		t.Errorf("subscription not updated: got %d want %d", post, newPort)
	}
	if post == port1 {
		t.Errorf("subscription still returns old port %d", port1)
	}
}

func TestRotateUserPortForInbound_OtherInboundsUnaffected(t *testing.T) {
	// User has two inbounds; only the targeted one should change
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "dave", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	portA, err := mgr.GetBindPort(GetBindPortRequest{
		Username: "dave", TargetPort: 10004,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-a",
	})
	if err != nil {
		t.Fatalf("GetBindPort inbound-a: %v", err)
	}
	portB, err := mgr.GetBindPort(GetBindPortRequest{
		Username: "dave", TargetPort: 10005,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-b",
	})
	if err != nil {
		t.Fatalf("GetBindPort inbound-b: %v", err)
	}

	// Only rotate inbound-a
	_, err = mgr.RotateUserPortForInbound("dave", contracts.ContainerXray, "inbound-a", 0)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound: %v", err)
	}

	// inbound-a port must have changed
	newA, okA := mgr.GetUserPortByDst("dave", 10004)
	if !okA {
		t.Fatal("inbound-a mapping missing after rotation")
	}
	if newA == portA {
		t.Errorf("inbound-a port unchanged: %d", portA)
	}

	// inbound-b port must be untouched
	gotB, okB := mgr.GetUserPortByDst("dave", 10005)
	if !okB {
		t.Fatal("inbound-b mapping missing (should be untouched)")
	}
	if gotB != portB {
		t.Errorf("inbound-b port changed unexpectedly: was %d, got %d", portB, gotB)
	}
}

func TestRotateUserPortForInbound_PreferredPort(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)

	if err := mgr.AddUser(AddUserRequest{Username: "eve", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	_, err := mgr.GetBindPort(GetBindPortRequest{
		Username:      "eve",
		TargetPort:    10006,
		ContainerType: contracts.ContainerXray,
		InboundTag:    "vless-in",
	})
	if err != nil {
		t.Fatalf("GetBindPort: %v", err)
	}

	// Must sit inside the test forward pool (22000-22999), which is deliberately
	// kept below the Linux ephemeral range — see the pool definition in
	// testhelper_test.go.
	const wantPort uint32 = 22500
	newPort, err := mgr.RotateUserPortForInbound("eve", contracts.ContainerXray, "vless-in", wantPort)
	if err != nil {
		t.Fatalf("RotateUserPortForInbound with preferred port: %v", err)
	}
	if newPort != wantPort {
		t.Errorf("preferred port not honoured: got %d, want %d", newPort, wantPort)
	}

	got, ok := mgr.GetUserPortByDst("eve", 10006)
	if !ok || got != wantPort {
		t.Errorf("PortMappings: got %d, want %d", got, wantPort)
	}
}

func TestRotateAllUserPorts_NoRules(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)
	if err := mgr.AddUser(AddUserRequest{Username: "frank", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	_, err := mgr.RotateAllUserPorts("frank")
	if err == nil {
		t.Fatal("expected error when user has no forward rules")
	}
}

func TestRotateAllUserPorts_Success(t *testing.T) {
	mgr, _ := newTestManagerWithForward(t)
	if err := mgr.AddUser(AddUserRequest{Username: "grace", Password: "pass"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	portA, _ := mgr.GetBindPort(GetBindPortRequest{
		Username: "grace", TargetPort: 10010,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-a",
	})
	portB, _ := mgr.GetBindPort(GetBindPortRequest{
		Username: "grace", TargetPort: 10011,
		ContainerType: contracts.ContainerXray, InboundTag: "inbound-b",
	})

	result, err := mgr.RotateAllUserPorts("grace")
	if err != nil {
		t.Fatalf("RotateAllUserPorts: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 rotated inbounds, got %d", len(result))
	}

	// Both ports should have changed
	newA, okA := mgr.GetUserPortByDst("grace", 10010)
	newB, okB := mgr.GetUserPortByDst("grace", 10011)
	if !okA || !okB {
		t.Fatal("port mappings missing after rotation")
	}
	if newA == portA {
		t.Errorf("inbound-a port unchanged: %d", portA)
	}
	if newB == portB {
		t.Errorf("inbound-b port unchanged: %d", portB)
	}

	// result map keys are "container:inboundTag"
	if result["xray:inbound-a"] != newA {
		t.Errorf("result[xray:inbound-a]=%d, GetUserPortByDst=%d", result["xray:inbound-a"], newA)
	}
	if result["xray:inbound-b"] != newB {
		t.Errorf("result[xray:inbound-b]=%d, GetUserPortByDst=%d", result["xray:inbound-b"], newB)
	}
}
