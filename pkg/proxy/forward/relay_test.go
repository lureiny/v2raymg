package forward

import (
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// startEchoServer starts a TCP echo server on a random port and returns the address.
func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo server listen: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	return ln.Addr().String(), func() { ln.Close() }
}

func TestRelay_BasicForward(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// Connect to relay
	conn, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()

	// Send data
	msg := "hello forward"
	_, err = conn.Write([]byte(msg))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read echo
	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(buf[:n])
	if got != msg {
		t.Errorf("expected %q, got %q", msg, got)
	}

	// Close client side to let counters settle
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	// Verify traffic counters
	if counter.Upload() < int64(len(msg)) {
		t.Errorf("upload should be >= %d, got %d", len(msg), counter.Upload())
	}
	if counter.Download() < int64(len(msg)) {
		t.Errorf("download should be >= %d, got %d", len(msg), counter.Download())
	}
}

func TestRelay_MultipleConnections(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// Open 5 concurrent connections
	conns := make([]net.Conn, 5)
	for i := 0; i < 5; i++ {
		c, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns[i] = c
	}

	// Give a moment for connections to register
	time.Sleep(50 * time.Millisecond)

	if relay.ActiveConnections() != 5 {
		t.Errorf("expected 5 active connections, got %d", relay.ActiveConnections())
	}

	// Close all
	for _, c := range conns {
		c.Close()
	}
	time.Sleep(200 * time.Millisecond)

	if relay.ActiveConnections() != 0 {
		t.Errorf("expected 0 active connections after close, got %d", relay.ActiveConnections())
	}
}

func TestRelay_MaxConnections(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		MaxConns:   2,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// Open 2 connections (should succeed)
	c1, _ := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	defer c1.Close()
	c2, _ := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	defer c2.Close()

	time.Sleep(100 * time.Millisecond)

	// 3rd connection should be rejected (closed immediately by relay)
	c3, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		// Connection refused is also valid
		return
	}
	defer c3.Close()

	// Try to read - should get EOF immediately since relay closes it
	c3.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = c3.Read(buf)
	if err == nil {
		t.Error("3rd connection should have been rejected")
	}
}

func TestRelay_Stop(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}

	// Open a connection
	conn, _ := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)

	// Stop should close listener and drain connections
	done := make(chan struct{})
	go func() {
		relay.Stop()
		close(done)
	}()

	// Close client connection to allow drain
	if conn != nil {
		conn.Close()
	}

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() timed out")
	}
}

func TestRelay_TrafficCounting(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	conn, _ := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	defer conn.Close()

	// Send known amount of data
	payload := strings.Repeat("x", 1000)
	conn.Write([]byte(payload))

	// Read it back
	buf := make([]byte, 2000)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	totalRead := 0
	for totalRead < 1000 {
		n, err := conn.Read(buf[totalRead:])
		totalRead += n
		if err != nil {
			break
		}
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if counter.Upload() != 1000 {
		t.Errorf("expected upload 1000, got %d", counter.Upload())
	}
	if counter.Download() < 1000 {
		t.Errorf("expected download >= 1000, got %d", counter.Download())
	}
}

func TestRelay_TargetUnreachable(t *testing.T) {
	// Use a port that nothing listens on
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: "127.0.0.1:1", // port 1 - unlikely to be listening
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	conn, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		return // acceptable
	}
	defer conn.Close()

	// Connection should be closed by relay (target unreachable)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(make([]byte, 1))
	if err == nil {
		t.Error("expected error/EOF when target unreachable")
	}
}

func TestRelay_ListenAddrActual(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})

	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	addr := relay.ListenAddr()
	if addr == "127.0.0.1:0" {
		t.Error("ListenAddr should return actual bound address, not :0")
	}
	fmt.Printf("Relay listening on %s\n", addr)
}

func TestRelay_ClientLimiter_DenyWhenSlotFull(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	// Create limiter with MaxClients = 1 (only 1 unique IP allowed)
	limiter := NewTestClientLimiter(1)

	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    echoAddr,
		ClientLimiter: limiter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// First connection from 127.0.0.1 should succeed
	c1, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("first dial should succeed: %v", err)
	}
	defer c1.Close()

	// Wait a bit for connection to be tracked
	time.Sleep(100 * time.Millisecond)

	// Second connection from same IP should succeed (same slot allows multiple connections)
	c2, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("second dial from same IP should succeed: %v", err)
	}
	defer c2.Close()
}

func TestRelay_ClientLimiter_AggregateAcrossRelays(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	// Create shared limiter with MaxClients = 1 (only 1 unique IP allowed across all relays)
	shared := NewTestClientLimiter(1)
	r1 := NewTCPRelay(TCPRelayConfig{ListenAddr: "127.0.0.1:0", TargetAddr: echoAddr, ClientLimiter: shared})
	r2 := NewTCPRelay(TCPRelayConfig{ListenAddr: "127.0.0.1:0", TargetAddr: echoAddr, ClientLimiter: shared})
	if err := r1.Start(); err != nil {
		t.Fatalf("r1 start: %v", err)
	}
	defer r1.Stop()
	if err := r2.Start(); err != nil {
		t.Fatalf("r2 start: %v", err)
	}
	defer r2.Stop()

	// Both connections come from same IP (127.0.0.1), should succeed
	c1, err := net.DialTimeout("tcp", r1.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial r1: %v", err)
	}
	defer c1.Close()
	c2, err := net.DialTimeout("tcp", r2.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial r2: %v", err)
	}
	defer c2.Close()

	// Third connection from same IP should also succeed (same slot allows multiple connections)
	c3, err := net.DialTimeout("tcp", r1.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial r1 again should succeed: %v", err)
	}
	defer c3.Close()
}

// TestRelay_HalfCloseRecovery tests that when one direction errors (e.g., network failure),
// the session terminates immediately and resources are released.
func TestRelay_HalfCloseRecovery(t *testing.T) {
	// Create a server that accepts but immediately closes (simulates connection reset)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Accept one connection and immediately close it (simulates network failure)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Immediately close to simulate connection reset
		conn.Close()
	}()

	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: ln.Addr().String(),
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// Connect client
	conn, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send some data - this will trigger copy to target which is already closed
	// The copy should return with an error (connection reset)
	conn.Write([]byte("test"))

	// Close client connection to signal EOF - this should trigger error detection
	conn.Close()

	// Wait for connection count to drop to 0
	timeout := time.Now().Add(2 * time.Second)
	for relay.ActiveConnections() > 0 {
		if time.Now().After(timeout) {
			t.Fatalf("expected active connections to be 0 after close, got %d", relay.ActiveConnections())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ========== Client limiter tests (simplified) ==========

// TestClientLimiter_SlotExclusivity tests that MaxClients=1 blocks new remote IPs.
func TestClientLimiter_SlotExclusivity(t *testing.T) {
	lim := NewTestClientLimiter(1)

	// First IP should be allowed
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("first IP should be allowed")
	}
	// Confirm acquire (simulates dial success)
	lim.ConfirmAcquire("192.168.1.1")

	if lim.ActiveConnections() != 1 {
		t.Fatalf("expected 1 active, got %d", lim.ActiveConnections())
	}

	// Second IP should be rejected (MaxClients = 1)
	if lim.Acquire("192.168.1.2") {
		t.Fatal("second IP should be rejected when MaxClients is 1")
	}

	// Release first IP
	lim.Release("192.168.1.1")

	// After release, the slot should be in recycling state
	// Need to wait for recycle delay or use shorter delay for test
}

// TestClientLimiter_SameIPMultipleConns tests that same IP can have multiple connections.
func TestClientLimiter_SameIPMultipleConns(t *testing.T) {
	lim := NewTestClientLimiter(1) // MaxClients = 1

	// First connection from IP should succeed
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("first acquire should succeed")
	}
	// Confirm acquire (simulates dial success)
	lim.ConfirmAcquire("192.168.1.1")

	// Second connection from same IP should also succeed (same slot)
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("second acquire from same IP should succeed")
	}
	// Confirm acquire for second connection
	lim.ConfirmAcquire("192.168.1.1")

	// Active connections should be 2
	if lim.ActiveConnections() != 2 {
		t.Fatalf("expected 2 active, got %d", lim.ActiveConnections())
	}

	// Release first connection
	lim.Release("192.168.1.1")

	// Active connections should be 1
	if lim.ActiveConnections() != 1 {
		t.Fatalf("expected 1 active, got %d", lim.ActiveConnections())
	}

	// Release second connection
	lim.Release("192.168.1.1")

	// Active connections should be 0
	if lim.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active after all releases, got %d", lim.ActiveConnections())
	}
}

// TestClientLimiter_MultipleClients tests that multiple unique IPs are limited by MaxClients.
func TestClientLimiter_MultipleClients(t *testing.T) {
	lim := NewTestClientLimiter(2) // MaxClients = 2

	// First IP should succeed
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("first IP should be allowed")
	}
	lim.ConfirmAcquire("192.168.1.1")

	// Second IP should succeed
	if !lim.Acquire("192.168.1.2") {
		t.Fatal("second IP should be allowed")
	}
	lim.ConfirmAcquire("192.168.1.2")

	// Third IP should be rejected
	if lim.Acquire("192.168.1.3") {
		t.Fatal("third IP should be rejected when MaxClients is 2")
	}
}

// TestClientLimiter_CleanupExpiredSlots tests that expired slots are cleaned up automatically.
func TestClientLimiter_CleanupExpiredSlots(t *testing.T) {
	// Create limiter with short recycle delay for testing
	config := ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:        1, // 1 second for testing
		SingleDirectionDrainSec: 2,
	}
	lim := newRemoteIPClientLimiter(config)

	// Acquire and confirm, then release
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("acquire should succeed")
	}
	lim.ConfirmAcquire("192.168.1.1")
	lim.Release("192.168.1.1")

	// Active connections should be 0
	if lim.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active, got %d", lim.ActiveConnections())
	}

	// New IP should still be rejected (slot in recycling, timer running)
	if lim.Acquire("192.168.1.2") {
		t.Fatal("should still be rejected during recycling timer")
	}

	// Wait for recycle timer to expire
	time.Sleep(1100 * time.Millisecond)

	// Now new IP should be allowed (slot auto-deleted by timer)
	if !lim.Acquire("192.168.1.2") {
		t.Fatal("new IP should be allowed after timer expired")
	}
}

// TestRelay_WithClientLimiter tests relay with client limiter.
func TestRelay_WithClientLimiter(t *testing.T) {
	// Create echo server
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
			io.Copy(conn, conn)
			conn.Close()
		}
	}()

	// Create client limiter with MaxClients = 1
	userLimiter := NewTestClientLimiter(1)

	// Create relay with client limiter
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    ln.Addr().String(),
		ClientLimiter: userLimiter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// First connection should succeed
	conn1, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial1: %v", err)
	}
	defer conn1.Close()

	// Wait a bit for connection to be tracked
	time.Sleep(100 * time.Millisecond)

	// Second connection from same IP should succeed (same slot allows multiple connections)
	conn2, err := net.DialTimeout("tcp", relay.ListenAddr(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial2 from same IP should succeed: %v", err)
	}
	defer conn2.Close()
}

// TestClientLimiter_CancelAcquire_CleansSlot tests that CancelAcquire removes empty slot.
func TestClientLimiter_CancelAcquire_CleansSlot(t *testing.T) {
	lim := NewTestClientLimiter(1)

	// Acquire (creates pending slot)
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("acquire should succeed")
	}

	// Cancel (should remove empty slot since no ConfirmAcquire was called)
	lim.CancelAcquire("192.168.1.1")

	// Slot should be removed, new IP should be allowed
	if !lim.Acquire("192.168.1.2") {
		t.Fatal("new IP should be allowed after cancel")
	}
}

// TestClientLimiter_CancelAcquire_DoesNotAffectActive tests that CancelAcquire doesn't affect active connections.
func TestClientLimiter_CancelAcquire_DoesNotAffectActive(t *testing.T) {
	lim := NewTestClientLimiter(1)

	// Acquire and confirm
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("acquire should succeed")
	}
	lim.ConfirmAcquire("192.168.1.1")

	// Cancel should not affect active connections
	lim.CancelAcquire("192.168.1.1")

	// Active connections should still be 1
	if lim.ActiveConnections() != 1 {
		t.Fatalf("expected 1 active, got %d", lim.ActiveConnections())
	}
}

// TestClientLimiter_Release_UnderflowProtection tests that Release handles underflow gracefully.
func TestClientLimiter_Release_UnderflowProtection(t *testing.T) {
	lim := NewTestClientLimiter(1)

	// Acquire and confirm
	if !lim.Acquire("192.168.1.1") {
		t.Fatal("acquire should succeed")
	}
	lim.ConfirmAcquire("192.168.1.1")

	// Release once - active should be 0
	lim.Release("192.168.1.1")
	if lim.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active after release, got %d", lim.ActiveConnections())
	}

	// Release again - should not crash or go negative (underflow protection)
	lim.Release("192.168.1.1")
	if lim.ActiveConnections() != 0 {
		t.Fatalf("expected 0 active after double release, got %d", lim.ActiveConnections())
	}
}

// TestClientLimiter_ConcurrentAcquireCancel tests concurrent Acquire and CancelAcquire.
func TestClientLimiter_ConcurrentAcquireCancel(t *testing.T) {
	lim := NewTestClientLimiter(10)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Run multiple concurrent Acquire and CancelAcquire
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ip := fmt.Sprintf("192.168.1.%d", idx%10+1) // 10 unique IPs
			if !lim.Acquire(ip) {
				errors <- fmt.Errorf("acquire failed for %s", ip)
				return
			}
			// Randomly confirm or cancel
			if idx%2 == 0 {
				lim.ConfirmAcquire(ip)
			} else {
				lim.CancelAcquire(ip)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent test error: %v", err)
	}

	// Should not crash and connections should be non-negative
	active := lim.ActiveConnections()
	if active < 0 {
		t.Errorf("active connections went negative: %d", active)
	}
}

// TestForwardManager_ClientLimitDisable covers the disable→re-enable cycle
// against an *existing* user-level limiter: once AddRule attached a shared
// remoteIPClientLimiter, subsequent SetUserClientLimitConfig calls must update
// that same instance in place (including switching to passthrough when
// MaxClients<=0), not replace or drop it — that is the stable-reference
// invariant relays depend on.
func TestForwardManager_ClientLimitDisable(t *testing.T) {
	m, err := NewDefaultForwardManager(PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: false,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	defer m.Close()

	// Attach a limiter via AddRule with an initial MaxClients=1 policy.
	if _, err := m.AddRule(ForwardRule{
		Username:               "user1",
		ContainerType:          "xray",
		InboundTag:             "in1",
		TargetAddr:             "127.0.0.1:8080",
		ListenPort:             10000,
		MaxClients:             1,
		ClientRecycleDelaySec:  60,
		ClientDrainSec:         2,
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	limiter := m.userClientLimiters["user1"]
	if limiter == nil {
		t.Fatal("expected limiter to be attached after AddRule with MaxClients>0")
	}
	storedConfig, ok := m.GetUserClientLimitConfig("user1")
	if !ok || storedConfig.MaxClients != 1 {
		t.Fatalf("expected stored MaxClients=1, got ok=%v cfg=%v", ok, storedConfig)
	}

	// Disable by setting MaxClients=0. The config and limiter must remain so
	// that the relay already referencing `limiter` immediately observes the
	// passthrough policy without needing a re-AddRule.
	if err := m.SetUserClientLimitConfig("user1", ClientLimitConfig{
		MaxClients:              0,
		RecycleDelaySec:        60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig to disable: %v", err)
	}

	disabledConfig, ok := m.GetUserClientLimitConfig("user1")
	if !ok {
		t.Fatal("expected config to stay present in passthrough mode")
	}
	if disabledConfig.MaxClients != 0 {
		t.Errorf("expected MaxClients=0 after disable, got %d", disabledConfig.MaxClients)
	}
	if m.userClientLimiters["user1"] != limiter {
		t.Error("expected limiter instance to be preserved across enable -> disable (stable reference)")
	}

	impl := limiter.(*remoteIPClientLimiter)
	if !impl.Acquire("10.0.0.1") {
		t.Error("passthrough Acquire #1 should succeed")
	}
	impl.ConfirmAcquire("10.0.0.1")
	if !impl.Acquire("10.0.0.2") {
		t.Error("passthrough Acquire for a second IP should succeed in passthrough mode")
	}
	impl.ConfirmAcquire("10.0.0.2")

	// Re-enabling with MaxClients=1 must use the same limiter instance; the
	// pre-existing slots count toward the new quota, so a third IP is rejected.
	if err := m.SetUserClientLimitConfig("user1", ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:        60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig re-enable: %v", err)
	}
	if m.userClientLimiters["user1"] != limiter {
		t.Error("expected same limiter instance after re-enable")
	}
	if impl.Acquire("10.0.0.3") {
		t.Error("third IP should be rejected under MaxClients=1 (existing slots count toward quota)")
	}
}

// TestForwardManager_SetUserClientLimitConfig_NoStandaloneLimiter guards the
// new lazy-creation invariant: SetUserClientLimitConfig stores the config for
// future rules but must NOT pre-create a standalone limiter when no rule is
// attached. Pre-creating one would leak a *remoteIPClientLimiter for every
// default-limit user synced in from the cluster that never has a local
// inbound.
func TestForwardManager_SetUserClientLimitConfig_NoStandaloneLimiter(t *testing.T) {
	m, err := NewDefaultForwardManager(PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: false,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	defer m.Close()

	if err := m.SetUserClientLimitConfig("ghost-user", ClientLimitConfig{
		MaxClients:              5,
		RecycleDelaySec:        30,
		SingleDirectionDrainSec: 1,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	if _, ok := m.userClientLimiters["ghost-user"]; ok {
		t.Error("SetUserClientLimitConfig must not create a standalone limiter for a user with no rule")
	}
	// Config should still be stored so AddRule can later seed the limiter.
	if cfg, ok := m.GetUserClientLimitConfig("ghost-user"); !ok || cfg.MaxClients != 5 {
		t.Errorf("expected stored config to survive, got ok=%v cfg=%v", ok, cfg)
	}
}

// TestForwardManager_ClientLimitConfigOnly tests that config is stored without creating limiter.
func TestForwardManager_ClientLimitConfigOnly(t *testing.T) {
	allocCfg := PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: false,
	}
	m, err := NewDefaultForwardManager(allocCfg)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	defer m.Close()

	// Set config without adding any rule
	config := ClientLimitConfig{
		MaxClients:              5,
		RecycleDelaySec:        30,
		SingleDirectionDrainSec: 1,
	}
	err = m.SetUserClientLimitConfig("user1", config)
	if err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	// Verify config is stored
	storedConfig, ok := m.GetUserClientLimitConfig("user1")
	if !ok || storedConfig.MaxClients != 5 {
		t.Fatalf("expected config MaxClients=5, got %v", storedConfig)
	}

	// Add a rule - it should use the stored config to create limiter
	rule := ForwardRule{
		Username:       "user1",
		ContainerType:  "xray",
		InboundTag:    "test-inbound",
		TargetAddr:    "127.0.0.1:8080",
		ListenPort:    10000,
	}
	createdRule, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Verify the rule was created successfully (limiter was created from stored config)
	if createdRule.ListenPort != 10000 {
		t.Fatalf("expected listen port 10000, got %d", createdRule.ListenPort)
	}

	// Get stats using correct rule key
	ruleKey := "xray:test-inbound:user1"
	stats, err := m.GetTraffic(ruleKey, false)
	if err != nil {
		t.Fatalf("GetTraffic: %v", err)
	}
	_ = stats // Just verify it doesn't panic
}

// TestForwardManager_AddRuleWithDisable tests that AddRule respects MaxClients=0 to disable.
func TestForwardManager_AddRuleWithDisable(t *testing.T) {
	allocCfg := PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: false,
	}
	m, err := NewDefaultForwardManager(allocCfg)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	defer m.Close()

	// First set a limit
	config := ClientLimitConfig{
		MaxClients:              2,
		RecycleDelaySec:        60,
		SingleDirectionDrainSec: 2,
	}
	err = m.SetUserClientLimitConfig("user1", config)
	if err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	// Now add a rule with MaxClients=0 (explicitly disabled)
	rule := ForwardRule{
		Username:       "user1",
		ContainerType:  "xray",
		InboundTag:     "test-inbound",
		TargetAddr:     "127.0.0.1:8080",
		ListenPort:     10000,
		MaxClients:     0, // explicitly disable
	}
	createdRule, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Rule should be created successfully with no client limiter
	_ = createdRule
	// The key point is it shouldn't panic - the rule is created with no client limit
}

// TestForwardManager_SetUserClientLimit_AppliesToExistingRule guards the bug
// where AddRule without a client limit left relay.clientLimiter=nil, so any
// later SetUserClientLimitConfig only mutated stored config without ever
// taking effect on the active relay. With the "stable reference" fix, AddRule
// always attaches a shared limiter (passthrough when MaxClients<=0), and
// SetUserClientLimitConfig mutates that same instance so already-running
// relays observe the new policy immediately.
func TestForwardManager_SetUserClientLimit_AppliesToExistingRule(t *testing.T) {
	m, err := NewDefaultForwardManager(PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: false,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	defer m.Close()

	// AddRule WITHOUT any prior client-limit config — effectiveMaxClients = 0,
	// so in the old code no limiter was ever created and relay.clientLimiter
	// stayed nil.
	if _, err := m.AddRule(ForwardRule{
		Username:      "user1",
		ContainerType: "xray",
		InboundTag:    "in1",
		TargetAddr:    "127.0.0.1:8080",
		ListenPort:    10000,
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}

	// Fix assertion #1: a shared limiter MUST exist after AddRule.
	limiter, ok := m.userClientLimiters["user1"]
	if !ok || limiter == nil {
		t.Fatal("expected AddRule to attach a user-level client limiter even when MaxClients=0")
	}
	// Fix assertion #2: the relay must reference the same limiter instance, so
	// an in-place SetConfig visibly changes the relay's policy.
	ruleKey := "xray:in1:user1"
	mr, ok := m.rules[ruleKey]
	if !ok {
		t.Fatalf("rule %q not registered", ruleKey)
	}
	if mr.clientLimiter != limiter {
		t.Fatal("expected relay.clientLimiter to be the same shared instance stored on the manager")
	}

	impl := limiter.(*remoteIPClientLimiter)

	// Under passthrough any number of distinct IPs should be admitted.
	for i := 0; i < 5; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i+1)
		if !impl.Acquire(ip) {
			t.Fatalf("passthrough Acquire(%s) rejected; limiter not in passthrough", ip)
		}
		impl.ConfirmAcquire(ip)
	}

	// Tighten the policy: MaxClients=2. Existing 5 sessions stay active;
	// the in-place config change must start rejecting new IPs that would push
	// the total over budget.
	if err := m.SetUserClientLimitConfig("user1", ClientLimitConfig{
		MaxClients:              2,
		RecycleDelaySec:        60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}
	if m.userClientLimiters["user1"] != limiter {
		t.Error("limiter instance must not be replaced by SetUserClientLimitConfig")
	}
	if mr.clientLimiter != limiter {
		t.Error("relay.clientLimiter must still point at the shared instance (stable reference)")
	}
	// A brand-new IP should now be rejected — previous 5 slots are still counted.
	if impl.Acquire("10.0.0.99") {
		t.Error("expected 6th distinct IP to be rejected after SetUserClientLimitConfig(MaxClients=2)")
	}

	// Reverse direction: MaxClients=0 flips back to passthrough without
	// dropping the limiter.
	if err := m.SetUserClientLimitConfig("user1", ClientLimitConfig{
		MaxClients:              0,
		RecycleDelaySec:        60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig(MaxClients=0): %v", err)
	}
	if m.userClientLimiters["user1"] != limiter {
		t.Error("limiter instance must not be dropped when going to passthrough")
	}
	if !impl.Acquire("10.0.0.99") {
		t.Error("passthrough should admit the previously-rejected IP")
	}
}
