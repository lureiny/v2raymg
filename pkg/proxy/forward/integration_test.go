package forward

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// integrationManager constructs a DefaultForwardManager with a wide test
// port range so we can run several rules in parallel without collisions.
func integrationManager(t *testing.T) *DefaultForwardManager {
	t.Helper()
	m, err := NewDefaultForwardManager(PortAllocatorConfig{
		MinPort: 31000,
		MaxPort: 31999,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return m
}

// TestIntegration_TCPUDPShareUserBandwidth verifies HARD CONSTRAINT A:
// TCP and UDP rules of the same user must share the same userBandwidthLimiter
// instance (and therefore the same underlying token buckets). The object
// identity check is the strongest available assertion — it guarantees that
// ANY bucket-related behavior (rate changes, WaitN pacing, LimitReader)
// is observed by both transports. Bucket-sharing at the byte level is
// already asserted in TestTokenBucketLimiter_WaitNSharesBucket.
func TestIntegration_TCPUDPShareUserBandwidth(t *testing.T) {
	echoTCP, stopTCP := startEchoServer(t)
	defer stopTCP()
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	const user = "bw-user"
	const rateBps = int64(256 * 1024)
	if err := m.SetUserBandwidthLimit(user, BandwidthUpload, rateBps); err != nil {
		t.Fatalf("SetUserBandwidthLimit upload: %v", err)
	}
	if err := m.SetUserBandwidthLimit(user, BandwidthDownload, rateBps); err != nil {
		t.Fatalf("SetUserBandwidthLimit download: %v", err)
	}

	tcpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "tcp-in",
		TargetAddr:    echoTCP,
	})
	if err != nil {
		t.Fatalf("add tcp: %v", err)
	}
	udpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "udp-in",
		TargetAddr:    echoUDP,
		Network:       "udp",
	})
	if err != nil {
		t.Fatalf("add udp: %v", err)
	}

	m.mu.RLock()
	tcpMR := m.rules[tcpRule.RuleKey()]
	udpMR := m.rules[udpRule.RuleKey()]
	m.mu.RUnlock()
	if tcpMR == nil || udpMR == nil {
		t.Fatal("managedRule missing")
	}

	// (1) Shared userBandwidthLimiter object.
	if tcpMR.userBandwidthLim != udpMR.userBandwidthLim {
		t.Fatal("TCP and UDP rules do NOT share a userBandwidthLimiter instance")
	}
	if tcpMR.userBandwidthLim == nil {
		t.Fatal("userBandwidthLim is nil")
	}

	// (2) Shared underlying TokenBucketLimiter — the actual bucket used by
	// TCP's LimitReader/LimitWriter and UDP's WaitN.
	tcpUp, ok := tcpMR.userBandwidthLim.UploadLimiter().(*TokenBucketLimiter)
	if !ok {
		t.Fatal("upload limiter is not TokenBucketLimiter")
	}
	udpUp, ok := udpMR.userBandwidthLim.UploadLimiter().(*TokenBucketLimiter)
	if !ok {
		t.Fatal("upload limiter is not TokenBucketLimiter")
	}
	if tcpUp != udpUp {
		t.Fatal("TCP and UDP don't share upload TokenBucketLimiter")
	}
	tcpDown, _ := tcpMR.userBandwidthLim.DownloadLimiter().(*TokenBucketLimiter)
	udpDown, _ := udpMR.userBandwidthLim.DownloadLimiter().(*TokenBucketLimiter)
	if tcpDown != udpDown {
		t.Fatal("TCP and UDP don't share download TokenBucketLimiter")
	}

	// (3) Rate set by SetUserBandwidthLimit is visible on the shared bucket.
	if got := tcpUp.GetRate(); got != rateBps {
		t.Errorf("upload shared bucket rate = %d, want %d", got, rateBps)
	}
	if got := tcpDown.GetRate(); got != rateBps {
		t.Errorf("download shared bucket rate = %d, want %d", got, rateBps)
	}

	// (4) End-to-end smoke: both relays still carry bytes through the shared
	// bucket. We don't assert total throughput (the token bucket's enforcement
	// fidelity is covered elsewhere) but we do want to ensure neither channel
	// starves entirely.
	done := make(chan struct{})
	var wg sync.WaitGroup
	var tcpBytes, udpBytes int64

	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpRule.ListenPort))
		if err != nil {
			return
		}
		defer conn.Close()
		go io.Copy(io.Discard, conn)
		buf := make([]byte, 1024)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, werr := conn.Write(buf)
			if werr != nil {
				return
			}
			atomic.AddInt64(&tcpBytes, int64(n))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		c := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", udpRule.ListenPort))
		defer c.Close()
		payload := make([]byte, 512)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, werr := c.Write(payload)
			if werr != nil {
				return
			}
			atomic.AddInt64(&udpBytes, int64(n))
			_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			buf := make([]byte, 1024)
			_, _ = c.Read(buf)
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(done)
	wg.Wait()

	if tcpBytes == 0 {
		t.Error("tcp traffic starved (0 bytes)")
	}
	if udpBytes == 0 {
		t.Error("udp traffic starved (0 bytes)")
	}
	t.Logf("tcp_bytes=%d udp_bytes=%d (rate=%d/s)", tcpBytes, udpBytes, rateBps)
}

// TestIntegration_SameIPSharesClientSlot verifies HARD CONSTRAINT A:
// a single IP running both TCP and UDP at the same time occupies exactly one
// MaxClients slot (not two).
func TestIntegration_SameIPSharesClientSlot(t *testing.T) {
	echoTCP, stopTCP := startEchoServer(t)
	defer stopTCP()
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	const user = "slot-user"
	if err := m.SetUserClientLimitConfig(user, ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:         60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	tcpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "tcp-in",
		TargetAddr:    echoTCP,
	})
	if err != nil {
		t.Fatalf("add tcp: %v", err)
	}
	udpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "udp-in",
		TargetAddr:    echoUDP,
		Network:       "udp",
	})
	if err != nil {
		t.Fatalf("add udp: %v", err)
	}

	// Sanity: shared ClientLimiter instance.
	m.mu.RLock()
	tcpMR := m.rules[tcpRule.RuleKey()]
	udpMR := m.rules[udpRule.RuleKey()]
	m.mu.RUnlock()
	if tcpMR.clientLimiter != udpMR.clientLimiter {
		t.Fatal("TCP and UDP rules do NOT share ClientLimiter instance")
	}

	// Same client IP (127.0.0.1) connects TCP then UDP.
	tcpConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpRule.ListenPort))
	if err != nil {
		t.Fatalf("tcp dial: %v", err)
	}
	defer tcpConn.Close()
	// Kick off a non-trivial TCP exchange to make sure the slot is confirmed.
	if _, err := tcpConn.Write([]byte("hello")); err != nil {
		t.Fatalf("tcp write: %v", err)
	}
	buf := make([]byte, 128)
	_ = tcpConn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := tcpConn.Read(buf); err != nil {
		t.Fatalf("tcp read: %v", err)
	}

	udpClient := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", udpRule.ListenPort))
	defer udpClient.Close()
	if _, err := udpClient.Write([]byte("udp-data")); err != nil {
		t.Fatalf("udp write: %v", err)
	}
	resp := readOnce(t, udpClient, time.Second)
	if !bytes.Equal(resp, []byte("udp-data")) {
		t.Fatalf("udp echo got %q want udp-data (UDP was blocked by MaxClients?)", resp)
	}
}

// TestIntegration_DistinctIPsFillSlot verifies that an IP distinct from the
// one occupying MaxClients=1 is rejected when it tries to open a UDP session.
func TestIntegration_DistinctIPsFillSlot(t *testing.T) {
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	const user = "slot2-user"
	if err := m.SetUserClientLimitConfig(user, ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:         60,
		SingleDirectionDrainSec: 2,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	udpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "udp-in",
		TargetAddr:    echoUDP,
		Network:       "udp",
	})
	if err != nil {
		t.Fatalf("add udp: %v", err)
	}

	m.mu.RLock()
	mr := m.rules[udpRule.RuleKey()]
	m.mu.RUnlock()
	if mr.clientLimiter == nil {
		t.Fatal("clientLimiter is nil")
	}

	// Pre-occupy the slot under a non-loopback IP so the subsequent UDP
	// session (from 127.0.0.1) is rejected as a second IP.
	if !mr.clientLimiter.Acquire("10.99.0.1") {
		t.Fatal("pre-acquire failed")
	}
	mr.clientLimiter.ConfirmAcquire("10.99.0.1")

	client := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", udpRule.ListenPort))
	defer client.Close()
	if _, err := client.Write([]byte("blocked")); err != nil {
		t.Fatalf("udp write: %v", err)
	}
	if r := readOnce(t, client, 400*time.Millisecond); r != nil {
		t.Fatalf("expected drop for second IP, got echo %q", r)
	}

	mr.clientLimiter.Release("10.99.0.1")
}

// TestIntegration_TCPCloseUDPActiveKeepsSlot verifies that as long as UDP
// is active, closing TCP does not start the idle recycle for the slot —
// because RecordActivity on either side resets the timer.
func TestIntegration_TCPCloseUDPActiveKeepsSlot(t *testing.T) {
	echoTCP, stopTCP := startEchoServer(t)
	defer stopTCP()
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	const user = "recycle-user"
	if err := m.SetUserClientLimitConfig(user, ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:         1,
		SingleDirectionDrainSec: 1,
	}); err != nil {
		t.Fatalf("SetUserClientLimitConfig: %v", err)
	}

	tcpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "tcp-in",
		TargetAddr:    echoTCP,
	})
	if err != nil {
		t.Fatalf("add tcp: %v", err)
	}
	udpRule, err := m.AddRule(ForwardRule{
		Username:      user,
		ContainerType: "xray",
		InboundTag:    "udp-in",
		TargetAddr:    echoUDP,
		Network:       "udp",
	})
	if err != nil {
		t.Fatalf("add udp: %v", err)
	}

	m.mu.RLock()
	cl := m.rules[tcpRule.RuleKey()].clientLimiter
	m.mu.RUnlock()
	if cl == nil {
		t.Fatal("expected clientLimiter")
	}

	// 1. Establish TCP.
	tcpConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", tcpRule.ListenPort))
	if err != nil {
		t.Fatalf("tcp dial: %v", err)
	}
	if _, err := tcpConn.Write([]byte("x")); err != nil {
		t.Fatalf("tcp write: %v", err)
	}
	_ = tcpConn.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 16)
	_, _ = tcpConn.Read(buf)

	// 2. Establish UDP session; it shares the same slot (same loopback IP).
	udpClient := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", udpRule.ListenPort))
	defer udpClient.Close()
	if _, err := udpClient.Write([]byte("up")); err != nil {
		t.Fatalf("udp write: %v", err)
	}
	if r := readOnce(t, udpClient, time.Second); !bytes.Equal(r, []byte("up")) {
		t.Fatalf("udp echo failed: %q", r)
	}

	// 3. Close TCP. UDP should keep the slot occupied.
	tcpConn.Close()

	// 4. Keep UDP alive across the recycle window with periodic activity.
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		<-ticker.C
		if _, err := udpClient.Write([]byte("keep")); err != nil {
			t.Fatalf("udp keepalive write: %v", err)
		}
		_ = readOnce(t, udpClient, 200*time.Millisecond)
	}

	// 5. A second IP should still be rejected → slot not recycled.
	if cl.Acquire("10.99.0.2") {
		t.Error("slot was recycled while UDP was active; second IP Acquire should have failed")
		cl.Release("10.99.0.2")
	}
}

// TestIntegration_SetBandwidthAfterAddRule_AppliesToBothRelays verifies that
// calling SetUserBandwidthLimit after both rules are created applies the new
// rate to the shared bucket (visible to both TCP and UDP).
func TestIntegration_SetBandwidthAfterAddRule_AppliesToBothRelays(t *testing.T) {
	echoTCP, stopTCP := startEchoServer(t)
	defer stopTCP()
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	const user = "dyn-bw"
	tcpRule, err := m.AddRule(ForwardRule{
		Username: user, ContainerType: "xray", InboundTag: "tcp-in",
		TargetAddr: echoTCP,
	})
	if err != nil {
		t.Fatalf("add tcp: %v", err)
	}
	udpRule, err := m.AddRule(ForwardRule{
		Username: user, ContainerType: "xray", InboundTag: "udp-in",
		TargetAddr: echoUDP, Network: "udp",
	})
	if err != nil {
		t.Fatalf("add udp: %v", err)
	}

	m.mu.RLock()
	tcpMR := m.rules[tcpRule.RuleKey()]
	udpMR := m.rules[udpRule.RuleKey()]
	m.mu.RUnlock()

	if tcpMR.userBandwidthLim != udpMR.userBandwidthLim {
		t.Fatal("rules do not share userBandwidthLimiter")
	}
	// Initially unlimited.
	if tcpMR.userBandwidthLim.IsUploadSet() {
		t.Error("expected upload set=false initially")
	}

	// Dynamic set → should be observable via GetUserBandwidthLimit.
	if err := m.SetUserBandwidthLimit(user, BandwidthUpload, 123456); err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}
	rate, ok := m.GetUserBandwidthLimit(user, BandwidthUpload)
	if !ok {
		t.Fatal("expected upload limit to be set")
	}
	if rate != 123456 {
		t.Errorf("rate = %d, want 123456", rate)
	}

	// Both relays' limiters must reflect the change (they share the bucket).
	tcpLim, _ := tcpMR.userBandwidthLim.UploadLimiter().(*TokenBucketLimiter)
	udpLim, _ := udpMR.userBandwidthLim.UploadLimiter().(*TokenBucketLimiter)
	if tcpLim != udpLim {
		t.Fatal("TCP and UDP don't share upload TokenBucketLimiter")
	}
	if got := tcpLim.GetRate(); got != 123456 {
		t.Errorf("shared limiter rate = %d, want 123456", got)
	}
}

// TestIntegration_UDPRuleRestartIdempotency verifies that after removing and
// re-adding a UDP rule with the same preferred port, the same ListenPort is
// recovered — mirroring the PortMappings restart flow at the FM layer.
func TestIntegration_UDPRuleRestartIdempotency(t *testing.T) {
	echoUDP, stopUDP := startUDPEchoServer(t)
	defer stopUDP()

	m := integrationManager(t)
	defer m.Close()

	rule := ForwardRule{
		Username:      "restart-user",
		ContainerType: "xray",
		InboundTag:    "udp-in",
		TargetAddr:    echoUDP,
		Network:       "udp",
	}

	first, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("first AddRule: %v", err)
	}
	port := first.ListenPort
	if port == 0 {
		t.Fatal("expected non-zero allocated port")
	}

	if err := m.RemoveRule(first.RuleKey()); err != nil {
		t.Fatalf("RemoveRule: %v", err)
	}

	// Re-add with preferred port — simulates what UserManager.GetBindPort
	// does on restart using PortMappings[dstPort].
	rule.ListenPort = port
	second, err := m.AddRule(rule)
	if err != nil {
		t.Fatalf("second AddRule: %v", err)
	}
	if second.ListenPort != port {
		t.Errorf("expected same port %d, got %d", port, second.ListenPort)
	}

	// Sanity: UDP still works after the re-add.
	client := dialUDP(t, fmt.Sprintf("127.0.0.1:%d", second.ListenPort))
	defer client.Close()
	if _, err := client.Write([]byte("restart")); err != nil {
		t.Fatalf("udp write: %v", err)
	}
	if r := readOnce(t, client, time.Second); !bytes.Equal(r, []byte("restart")) {
		t.Fatalf("udp echo = %q, want restart", r)
	}
}
