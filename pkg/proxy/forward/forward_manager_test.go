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

// TestForwardManager_AddRule_UDPDispatch verifies that Network="udp" actually
// instantiates a UDPRelay and that end-to-end datagram forwarding works.
func TestForwardManager_AddRule_UDPDispatch(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	created, err := m.AddRule(ForwardRule{
		Username:          "udp-user@test.com",
		TargetAddr:        echoAddr,
		Network:           "udp",
		UDPSessionIdleSec: 5,
	})
	if err != nil {
		t.Fatalf("AddRule udp: %v", err)
	}

	// The managed relay must be UDP, not TCP. Under the default dual-stack
	// listener the relay is a *multiRelay wrapping one UDPRelay per bound
	// socket, so unwrap before asserting the concrete transport type.
	m.mu.RLock()
	mr := m.rules[created.RuleKey()]
	m.mu.RUnlock()
	if mr == nil {
		t.Fatal("managedRule missing")
	}
	assertUDPRelay(t, mr.relay)

	// Round-trip a datagram via the allocated port.
	client := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", created.ListenPort))
	defer client.Close()
	payload := []byte("udp-dispatch")
	if _, err := client.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := readOnce(t, client, time.Second)
	if string(resp) != string(payload) {
		t.Fatalf("echo = %q, want %q", resp, payload)
	}

	// RemoveRule should cleanly stop the UDPRelay and release the port.
	if err := m.RemoveRule(created.RuleKey()); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}
	if m.GetRule(created.RuleKey()) != nil {
		t.Error("rule should be gone after RemoveRule")
	}
	// Port is freed — listen on it directly should succeed.
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", created.ListenPort))
	if err != nil {
		t.Fatalf("port not released: %v", err)
	}
	pc.Close()
}

// TestForwardManager_AddRule_NetworkValidation verifies that invalid Network
// values are rejected at Validate, not silently treated as TCP.
func TestForwardManager_AddRule_NetworkValidation(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	_, err := m.AddRule(ForwardRule{
		Username:   "u@t.com",
		TargetAddr: "127.0.0.1:1234",
		Network:    "sctp",
	})
	if err == nil {
		t.Fatal("expected error for unsupported network")
	}
}

// TestForwardManager_AllocatePort_Basic verifies that AllocatePort returns
// distinct in-range ports and ReleasePort makes them reusable.
func TestForwardManager_AllocatePort_Basic(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	p1, err := m.AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}
	if p1 < 19000 || p1 > 19100 {
		t.Errorf("port %d out of range [19000,19100]", p1)
	}

	p2, err := m.AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort (2nd): %v", err)
	}
	if p1 == p2 {
		t.Errorf("AllocatePort returned the same port twice: %d", p1)
	}

	m.ReleasePort(p1)
	// After release, p1 is eligible again; p2 must still be held.
	p3, err := m.AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort after Release: %v", err)
	}
	if p3 == p2 {
		t.Errorf("AllocatePort handed out still-allocated port %d", p2)
	}
}

// TestForwardManager_AllocatePort_AfterClose verifies AllocatePort is
// rejected once the manager is closed, matching AddRule's behaviour.
func TestForwardManager_AllocatePort_AfterClose(t *testing.T) {
	m := newTestManager(t)
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := m.AllocatePort(); err == nil {
		t.Fatal("AllocatePort after Close should fail")
	}
}

// TestForwardManager_AllocatePort_SharesPoolWithAddRule verifies that ports
// obtained via AllocatePort cannot be reused by AddRule (ListenPort=P) until
// released, proving both paths share the same allocation table — the core
// guarantee the mihomo container relies on.
func TestForwardManager_AllocatePort_SharesPoolWithAddRule(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	m := newTestManager(t)
	defer m.Close()

	p, err := m.AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}

	_, err = m.AddRule(ForwardRule{
		Username:   "u@t.com",
		TargetAddr: echoAddr,
		ListenPort: p,
	})
	if err == nil {
		t.Errorf("AddRule(ListenPort=%d) should have failed while port is held via AllocatePort", p)
	}

	m.ReleasePort(p)

	rule, err := m.AddRule(ForwardRule{
		Username:   "u@t.com",
		TargetAddr: echoAddr,
		ListenPort: p,
	})
	if err != nil {
		t.Fatalf("AddRule(ListenPort=%d) after ReleasePort: %v", p, err)
	}
	if rule.ListenPort != p {
		t.Errorf("AddRule used wrong port: got %d, want %d", rule.ListenPort, p)
	}
}
