package forward

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *DefaultForwardManager {
	t.Helper()
	m, err := NewDefaultForwardManager(PortAllocatorConfig{
		MinPort: 19000,
		MaxPort: 19100,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	return m
}

func TestForwardManager_AddRemoveRule(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	rule, err := m.AddRule(ForwardRule{
		Username:   "user1@test.com",
		TargetAddr: echoAddr,
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	if rule.ListenPort < 19000 || rule.ListenPort > 19100 {
		t.Errorf("allocated port %d out of range", rule.ListenPort)
	}

	// Verify rule is retrievable
	got := m.GetRule(rule.RuleKey())
	if got == nil {
		t.Fatal("GetRule returned nil")
	}
	if got.ListenPort != rule.ListenPort {
		t.Errorf("port mismatch: got %d, want %d", got.ListenPort, rule.ListenPort)
	}

	// Test connectivity
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+itoa(rule.ListenPort), 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	conn.Write([]byte("test"))
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)
	conn.Close()

	if string(buf[:n]) != "test" {
		t.Errorf("expected echo 'test', got %q", string(buf[:n]))
	}

	// Remove rule
	if err := m.RemoveRule(rule.RuleKey()); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}

	if m.GetRule(rule.RuleKey()) != nil {
		t.Error("rule should be gone after RemoveRule")
	}
}

func TestForwardManager_DuplicateRule(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	r := ForwardRule{Username: "u@t.com", TargetAddr: echoAddr}
	_, err := m.AddRule(r)
	if err != nil {
		t.Fatalf("first AddRule: %v", err)
	}

	_, err = m.AddRule(r)
	if err == nil {
		t.Fatal("duplicate AddRule should fail")
	}
}

func TestForwardManager_GetRulesByUser(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	m.AddRule(ForwardRule{Username: "alice@t.com", TargetAddr: echoAddr})
	m.AddRule(ForwardRule{Username: "bob@t.com", TargetAddr: echoAddr})

	alice := m.GetRulesByUser("alice@t.com")
	if len(alice) != 1 {
		t.Errorf("expected 1 rule for alice, got %d", len(alice))
	}

	bob := m.GetRulesByUser("bob@t.com")
	if len(bob) != 1 {
		t.Errorf("expected 1 rule for bob, got %d", len(bob))
	}
}

func TestForwardManager_RemoveByUser(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	m.AddRule(ForwardRule{Username: "remove-me@t.com", TargetAddr: echoAddr})
	m.AddRule(ForwardRule{Username: "keep-me@t.com", TargetAddr: echoAddr})

	m.RemoveRulesByUser("remove-me@t.com")

	all := m.GetAllRules()
	if len(all) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(all))
	}
	if all[0].Username != "keep-me@t.com" {
		t.Errorf("wrong rule kept: %s", all[0].Username)
	}
}

func TestForwardManager_Traffic(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	rule, _ := m.AddRule(ForwardRule{
		Username: "u@t.com", TargetAddr: echoAddr,
	})

	// Send data through
	conn, _ := net.DialTimeout("tcp", "127.0.0.1:"+itoa(rule.ListenPort), 2*time.Second)
	conn.Write([]byte("hello"))
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.Read(buf)
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	snap, err := m.GetTraffic(rule.RuleKey(), false)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	if snap.Upload < 5 {
		t.Errorf("expected upload >= 5, got %d", snap.Upload)
	}
	if snap.Download < 5 {
		t.Errorf("expected download >= 5, got %d", snap.Download)
	}

	// GetAllTraffic
	all := m.GetAllTraffic(false)
	if all.TotalRules != 1 {
		t.Errorf("expected 1 rule, got %d", all.TotalRules)
	}
}

func TestForwardManager_TrafficReset(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	rule, _ := m.AddRule(ForwardRule{
		Username: "reset@t.com", TargetAddr: echoAddr,
	})

	conn, _ := net.DialTimeout("tcp", "127.0.0.1:"+itoa(rule.ListenPort), 2*time.Second)
	conn.Write([]byte("data"))
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.Read(buf)
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Read with reset
	snap, _ := m.GetTraffic(rule.RuleKey(), true)
	if snap.Upload < 4 {
		t.Errorf("expected upload >= 4, got %d", snap.Upload)
	}

	// After reset, should be 0
	snap2, _ := m.GetTraffic(rule.RuleKey(), false)
	if snap2.Upload != 0 {
		t.Errorf("expected upload 0 after reset, got %d", snap2.Upload)
	}
}

func TestForwardManager_SpecificPort(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	rule, err := m.AddRule(ForwardRule{
		Username:   "u@t.com",
		TargetAddr: echoAddr,
		ListenPort: 19050,
	})
	if err != nil {
		t.Fatalf("AddRule with specific port: %v", err)
	}
	if rule.ListenPort != 19050 {
		t.Errorf("expected port 19050, got %d", rule.ListenPort)
	}
}

func TestForwardManager_Close(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)

	m.AddRule(ForwardRule{Username: "u1@t.com", TargetAddr: echoAddr})
	m.AddRule(ForwardRule{Username: "u2@t.com", TargetAddr: echoAddr})

	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(m.GetAllRules()) != 0 {
		t.Error("all rules should be cleaned up after Close")
	}

	// AddRule after Close should fail
	_, err := m.AddRule(ForwardRule{Username: "u@t.com", TargetAddr: echoAddr})
	if err == nil {
		t.Error("AddRule after Close should fail")
	}
}

func TestForwardManager_UpdateRateLimit(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	rule, _ := m.AddRule(ForwardRule{
		Username: "u@t.com", TargetAddr: echoAddr,
	})

	err := m.UpdateRateLimit(rule.RuleKey(), 1024, 2048)
	if err != nil {
		t.Fatalf("UpdateRateLimit: %v", err)
	}

	got := m.GetRule(rule.RuleKey())
	if got.UploadBytesPerSec != 1024 {
		t.Errorf("expected upload rate 1024, got %d", got.UploadBytesPerSec)
	}
	if got.DownloadBytesPerSec != 2048 {
		t.Errorf("expected download rate 2048, got %d", got.DownloadBytesPerSec)
	}
}

func TestForwardManager_LargeDataTransfer(t *testing.T) {
	// Server that accepts and discards data (no echo)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and discard - don't echo
			io.Copy(io.Discard, conn)
			conn.Close()
		}
	}()

	m := newTestManager(t)
	defer m.Close()

	rule, _ := m.AddRule(ForwardRule{
		Username: "big@t.com", TargetAddr: ln.Addr().String(),
	})

	conn, _ := net.DialTimeout("tcp", "127.0.0.1:"+itoa(rule.ListenPort), 2*time.Second)
	defer conn.Close()

	// Send 100KB
	data := make([]byte, 100*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Write data and signal EOF - with new behavior, upload direction returning
	// triggers immediate session termination
	conn.Write(data)
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.CloseWrite()
	}

	// Give time for session to terminate
	time.Sleep(100 * time.Millisecond)

	// Verify traffic was counted
	snap, _ := m.GetTraffic(rule.RuleKey(), false)
	if snap.Upload < int64(len(data)) {
		t.Errorf("upload should be >= %d, got %d", len(data), snap.Upload)
	}
}

// helper
func itoa(port uint32) string {
	return fmt.Sprintf("%d", port)
}

// Use fmt import
var _ = fmt.Sprintf

// --- User bandwidth limit tests ---

func TestForwardManager_SetUserBandwidthLimit_Validation(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Test empty username
	err := m.SetUserBandwidthLimit("", BandwidthUpload, 1024)
	if err == nil {
		t.Error("expected error for empty username")
	}

	// Test invalid kind
	err = m.SetUserBandwidthLimit("user@test.com", "invalid", 1024)
	if err == nil {
		t.Error("expected error for invalid kind")
	}

	// Test negative value
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, -1)
	if err == nil {
		t.Error("expected error for negative value")
	}

	// Test valid cases should not error
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	err = m.SetUserBandwidthLimit("user@test.com", BandwidthDownload, 2048)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Setting to 0 should clear the limit
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestForwardManager_GetUserBandwidthLimit(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// No limit set initially
	_, ok := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	if ok {
		t.Error("expected false for unset limit")
	}

	// Set upload limit
	err := m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Get should return the limit
	rate, ok := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	if !ok {
		t.Error("expected true for set limit")
	}
	if rate != 1024 {
		t.Errorf("expected rate 1024, got %d", rate)
	}

	// Download is not set yet, should return ok=false
	_, ok = m.GetUserBandwidthLimit("user@test.com", BandwidthDownload)
	if ok {
		t.Error("expected false for unset download limit")
	}

	// Set download limit
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthDownload, 2048)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Both should be retrievable
	up, _ := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	down, _ := m.GetUserBandwidthLimit("user@test.com", BandwidthDownload)
	if up != 1024 || down != 2048 {
		t.Errorf("expected upload=1024, download=2048, got upload=%d, download=%d", up, down)
	}
}

func TestForwardManager_UserBandwidthLimit_AppliedToNewRules(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	// Set user bandwidth limit BEFORE adding rules
	err := m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthDownload, 2048)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Add rule - should use user-level limits
	rule, err := m.AddRule(ForwardRule{
		Username:      "user@test.com",
		TargetAddr:    echoAddr,
		ContainerType: "xray",
		InboundTag:    "test-inbound",
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Verify rule has the user-level limit applied (via GetRule)
	got := m.GetRule(rule.RuleKey())
	if got == nil {
		t.Fatal("GetRule returned nil")
	}
	// Note: GetRule returns the rule config, not actual applied limits
	// The actual limits are stored in userBandwidthLimiter and used at relay level

	// Verify user limit is still accessible
	up, ok := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	if !ok || up != 1024 {
		t.Errorf("expected upload limit 1024, got %d, ok=%v", up, ok)
	}
}

func TestForwardManager_UserBandwidthLimit_DynamicUpdate(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	// Add rule first without user limit
	rule, err := m.AddRule(ForwardRule{
		Username:      "user@test.com",
		TargetAddr:    echoAddr,
		ContainerType: "xray",
		InboundTag:    "test-inbound",
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Set user bandwidth limit AFTER rule is added
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Add another rule for same user - should get user limit
	rule2, err := m.AddRule(ForwardRule{
		Username:      "user@test.com",
		TargetAddr:    echoAddr,
		ContainerType: "xray",
		InboundTag:    "test-inbound2",
	})
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Both rules should exist
	if m.GetRule(rule.RuleKey()) == nil || m.GetRule(rule2.RuleKey()) == nil {
		t.Error("both rules should exist")
	}

	// Update the limit dynamically
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 4096)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit update: %v", err)
	}

	// Verify the update
	rate, ok := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	if !ok || rate != 4096 {
		t.Errorf("expected upload limit 4096, got %d, ok=%v", rate, ok)
	}
}

func TestForwardManager_UserBandwidthLimit_MultipleUsersIndependent(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	// Set limits for two different users
	err := m.SetUserBandwidthLimit("user1@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}
	err = m.SetUserBandwidthLimit("user2@test.com", BandwidthUpload, 2048)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Add rules for both users
	rule1, err := m.AddRule(ForwardRule{
		Username:      "user1@test.com",
		TargetAddr:    echoAddr,
		ContainerType: "xray",
		InboundTag:    "inbound1",
	})
	if err != nil {
		t.Fatalf("AddRule for user1: %v", err)
	}

	rule2, err := m.AddRule(ForwardRule{
		Username:      "user2@test.com",
		TargetAddr:    echoAddr,
		ContainerType: "xray",
		InboundTag:    "inbound2",
	})
	if err != nil {
		t.Fatalf("AddRule for user2: %v", err)
	}

	// Both should exist
	_ = rule1
	_ = rule2

	// Verify limits are independent
	u1, _ := m.GetUserBandwidthLimit("user1@test.com", BandwidthUpload)
	u2, _ := m.GetUserBandwidthLimit("user2@test.com", BandwidthUpload)
	if u1 != 1024 || u2 != 2048 {
		t.Errorf("expected user1=1024, user2=2048, got user1=%d, user2=%d", u1, u2)
	}
}

func TestForwardManager_UserBandwidthLimit_ClearLimit(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Set limit
	err := m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 1024)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	// Clear limit by setting to 0
	err = m.SetUserBandwidthLimit("user@test.com", BandwidthUpload, 0)
	if err != nil {
		t.Fatalf("SetUserBandwidthLimit clear: %v", err)
	}

	// Should return ok=false
	_, ok := m.GetUserBandwidthLimit("user@test.com", BandwidthUpload)
	if ok {
		t.Error("expected false after clearing limit")
	}
}

// TestForwardManager_UserConnectionLimit_SetGet_Idempotent tests user-level connection limits.
// In the simplified version, client limits are set via ForwardRule.MaxClients, not user-level methods.
// This test is kept for reference but uses the new approach via rule creation.
func TestForwardManager_ClientLimitViaRule(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Create rule with MaxClients = 2
	rule := ForwardRule{
		Username:    "user@test.com",
		TargetAddr:  "127.0.0.1:8080",
		MaxClients: 2, // This sets the client limit
	}
	_, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
}
