package forward

import (
	"context"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startUDPEchoServer launches a UDP echo server on 127.0.0.1:0 and returns
// its address plus a cleanup function.
func startUDPEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("udp echo listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		buf := make([]byte, udpReadBufferSize)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = pc.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	return pc.LocalAddr().String(), func() {
		close(done)
		_ = pc.Close()
	}
}

// dialUDP connects to addr for UDP send/recv in tests.
func dialUDP(t *testing.T, addr string) *net.UDPConn {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve %s: %v", addr, err)
	}
	c, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	return c
}

func readOnce(t *testing.T, c *net.UDPConn, timeout time.Duration) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func TestUDPRelay_BasicForward(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()

	msg := []byte("hello-udp")
	if _, err := client.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := readOnce(t, client, time.Second)
	if string(resp) != string(msg) {
		t.Fatalf("echo mismatch: got %q want %q", resp, msg)
	}

	// allow a moment for counter ordering
	time.Sleep(30 * time.Millisecond)
	if up := counter.Upload(); up != int64(len(msg)) {
		t.Errorf("upload counter = %d, want %d", up, len(msg))
	}
	if down := counter.Download(); down != int64(len(msg)) {
		t.Errorf("download counter = %d, want %d", down, len(msg))
	}
	if active := counter.ActiveConns(); active != 1 {
		t.Errorf("active conns = %d, want 1", active)
	}
}

func TestUDPRelay_SessionReuse(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()

	// Three packets from the same client → one session.
	for i := 0; i < 3; i++ {
		if _, err := client.Write([]byte{byte(i)}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		_ = readOnce(t, client, 500*time.Millisecond)
	}

	time.Sleep(30 * time.Millisecond)
	if got := relay.ActiveSessions(); got != 1 {
		t.Errorf("active sessions = %d, want 1", got)
	}
}

func TestUDPRelay_MultipleClients(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			client := dialUDP(t, relay.ListenAddr())
			defer client.Close()
			if _, err := client.Write([]byte{byte(i)}); err != nil {
				return
			}
			_ = readOnce(t, client, time.Second)
		}(i)
	}
	wg.Wait()

	time.Sleep(50 * time.Millisecond)
	if got := relay.ActiveSessions(); got != int64(n) {
		t.Errorf("active sessions = %d, want %d", got, n)
	}
}

func TestUDPRelay_MaxSessionsLimit(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:  "127.0.0.1:0",
		TargetAddr:  echoAddr,
		Counter:     counter,
		MaxSessions: 1,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	c1 := dialUDP(t, relay.ListenAddr())
	defer c1.Close()
	_, _ = c1.Write([]byte("a"))
	if r := readOnce(t, c1, 500*time.Millisecond); string(r) != "a" {
		t.Fatalf("c1 echo missing: %q", r)
	}

	// c2 is a distinct source → should be rejected by maxSessions.
	c2 := dialUDP(t, relay.ListenAddr())
	defer c2.Close()
	_, _ = c2.Write([]byte("b"))
	if r := readOnce(t, c2, 300*time.Millisecond); r != nil {
		t.Fatalf("expected drop for c2, got echo %q", r)
	}

	if got := relay.ActiveSessions(); got != 1 {
		t.Errorf("active sessions = %d, want 1", got)
	}
}

func TestUDPRelay_ClientLimiterReject(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	// Pre-occupy the single slot with a distinct IP so the UDP packet from
	// 127.0.0.1 is the second IP and must be rejected.
	cl := NewTestClientLimiter(1)
	if !cl.Acquire("10.0.0.42") {
		t.Fatal("pre-acquire failed")
	}
	cl.ConfirmAcquire("10.0.0.42")

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    echoAddr,
		ClientLimiter: cl,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()
	_, _ = client.Write([]byte("x"))
	if r := readOnce(t, client, 300*time.Millisecond); r != nil {
		t.Fatalf("expected drop, got echo %q", r)
	}
	if got := relay.ActiveSessions(); got != 0 {
		t.Errorf("active sessions = %d, want 0", got)
	}
}

func TestUDPRelay_IdleGC(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:         "127.0.0.1:0",
		TargetAddr:         echoAddr,
		SessionIdleTimeout: 100 * time.Millisecond,
	})
	// Shrink GC interval via override (test-only).
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()
	_, _ = client.Write([]byte("hi"))
	_ = readOnce(t, client, 500*time.Millisecond)

	if got := relay.ActiveSessions(); got != 1 {
		t.Fatalf("active sessions after first packet = %d, want 1", got)
	}

	// Wait > idle + gc tick.
	deadline := time.Now().Add(udpGCInterval + 2*time.Second)
	for time.Now().Before(deadline) {
		if relay.ActiveSessions() == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("GC did not reap idle session, active=%d", relay.ActiveSessions())
}

func TestUDPRelay_StopDrainsAll(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}

	// Produce a few sessions.
	clients := make([]*net.UDPConn, 5)
	for i := range clients {
		clients[i] = dialUDP(t, relay.ListenAddr())
		_, _ = clients[i].Write([]byte{byte(i)})
		_ = readOnce(t, clients[i], 500*time.Millisecond)
	}

	time.Sleep(50 * time.Millisecond)
	if got := relay.ActiveSessions(); got != 5 {
		t.Fatalf("expected 5 sessions, got %d", got)
	}

	relay.Stop()

	if got := relay.ActiveSessions(); got != 0 {
		t.Errorf("after Stop active sessions = %d, want 0", got)
	}

	// After Stop returns, there should be no live udp reader / goroutines.
	// We can't directly introspect runtime, but a second Stop should no-op
	// and return instantly.
	done := make(chan struct{})
	go func() { relay.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("second Stop did not return promptly")
	}

	for _, c := range clients {
		_ = c.Close()
	}
}

// TestUDPRelay_ClientLimiterReleaseOnStop ensures that Stop releases every
// slot held in the ClientLimiter (shared with TCP in production).
func TestUDPRelay_ClientLimiterReleaseOnStop(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	cl := NewTestClientLimiter(8)
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    echoAddr,
		ClientLimiter: cl,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}

	client := dialUDP(t, relay.ListenAddr())
	_, _ = client.Write([]byte("ping"))
	_ = readOnce(t, client, 500*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if got := cl.ActiveConnections(); got != 1 {
		t.Fatalf("expected 1 active client conn, got %d", got)
	}
	client.Close()

	relay.Stop()

	// Wait briefly for release bookkeeping.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cl.ActiveConnections() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("client limiter still holding %d active conns after Stop", cl.ActiveConnections())
}

// TestUDPRelay_BytesCountedWithLimiter asserts counter increments under a
// bandwidth limiter (no drops expected in this low-volume scenario).
func TestUDPRelay_BytesCountedWithLimiter(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	lim := NewTokenBucketLimiter(1024*1024, 1024*1024) // 1MB/s - comfortable
	counter := NewTrafficCounter()
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:  "127.0.0.1:0",
		TargetAddr:  echoAddr,
		Counter:     counter,
		LimiterUp:   lim,
		LimiterDown: lim,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()

	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = byte(i)
	}
	const iterations = 10
	for i := 0; i < iterations; i++ {
		if _, err := client.Write(payload); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		_ = readOnce(t, client, 500*time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	expected := int64(iterations * len(payload))
	if up := counter.Upload(); up != expected {
		t.Errorf("upload counter = %d, want %d", up, expected)
	}
	if down := counter.Download(); down != expected {
		t.Errorf("download counter = %d, want %d", down, expected)
	}
}

// TestUDPRelay_ConcurrentClients mirrors "many clients, each sending a few
// datagrams" and verifies no panic, counter monotonicity, and session count.
func TestUDPRelay_ConcurrentClients(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	counter := NewTrafficCounter()
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	const clients = 20
	const perClient = 5
	var wg sync.WaitGroup
	var totalBytes int64
	wg.Add(clients)
	for i := 0; i < clients; i++ {
		go func(i int) {
			defer wg.Done()
			c := dialUDP(t, relay.ListenAddr())
			defer c.Close()
			for j := 0; j < perClient; j++ {
				payload := []byte{byte(i), byte(j)}
				if _, err := c.Write(payload); err != nil {
					return
				}
				atomic.AddInt64(&totalBytes, int64(len(payload)))
				_ = readOnce(t, c, 300*time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
	time.Sleep(80 * time.Millisecond)

	if got := relay.ActiveSessions(); got != clients {
		t.Errorf("active sessions = %d, want %d", got, clients)
	}
	if up := counter.Upload(); up != totalBytes {
		t.Errorf("upload counter = %d, want %d", up, totalBytes)
	}
}

// TestUDPRelay_StopIsIdempotent verifies that repeated Stop calls do not
// panic or block.
func TestUDPRelay_StopIsIdempotent(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	relay.Stop()
	relay.Stop()
	relay.Stop()
}

// TestUDPRelay_DoubleStartFails asserts that calling Start twice without an
// intervening Stop fails the second call at ListenPacket (address in use),
// mirroring TCPRelay's behavior. This replaces the silent-no-op that the
// previous sync.Once wrapper produced.
func TestUDPRelay_DoubleStartFails(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	// Pre-bind a port so ListenPacket is deterministic on the second call.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := pc.LocalAddr().String()
	// Don't close pc yet — it holds the port so the first Start of relay
	// will fail. That's enough to prove "explicit error" instead of
	// silently succeeding.
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: addr,
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err == nil {
		t.Fatal("expected Start to fail on occupied port")
	}
	pc.Close()

	// Now the port is free — first Start succeeds.
	relay2 := NewUDPRelay(UDPRelayConfig{
		ListenAddr: addr,
		TargetAddr: echoAddr,
	})
	if err := relay2.Start(); err != nil {
		t.Fatalf("first Start should succeed: %v", err)
	}
	defer relay2.Stop()

	// Calling Start again on the same relay must fail (port held by relay2
	// itself) instead of silently returning nil.
	if err := relay2.Start(); err == nil {
		t.Fatal("expected second Start to fail")
	}
}

// TestUDPRelay_UpstreamWriteFailureReapsQuickly asserts that when the upstream
// socket is broken, forwardUpstream triggers an immediate reap instead of
// waiting for the downstream Read timeout (~250ms).
func TestUDPRelay_UpstreamWriteFailureReapsQuickly(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)

	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()

	// First packet establishes the session.
	if _, err := client.Write([]byte("seed")); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if r := readOnce(t, client, time.Second); string(r) != "seed" {
		t.Fatalf("seed echo failed: %q", r)
	}
	if active := relay.ActiveSessions(); active != 1 {
		t.Fatalf("expected 1 active session, got %d", active)
	}

	// Break the upstream: close the echo server so the session's upstream
	// socket, which was net.Dial'd to the echo addr, starts returning
	// ECONNREFUSED on Write.
	stopEcho()

	// Now directly close the session's upstream socket from test-side by
	// finding it in the relay's sessions map — this reliably induces a
	// Write error on the next upstream.Write, independent of OS behavior.
	key := client.LocalAddr().String()
	sAny, ok := relay.sessions.Load(key)
	if !ok {
		t.Fatal("session not in map")
	}
	s := sAny.(*udpSession)
	_ = s.upstream.Close()

	start := time.Now()
	// Trigger a forwardUpstream call that will hit Write error and reap.
	if _, err := client.Write([]byte("boom")); err != nil {
		t.Fatalf("trigger write: %v", err)
	}

	// Wait for ActiveSessions to drop to 0 within 100ms (F3 SLA).
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if relay.ActiveSessions() == 0 {
			t.Logf("reap observed in %v", time.Since(start))
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("reap did not complete within 100ms; active=%d", relay.ActiveSessions())
}

// TestUDPRelay_LimiterDropsPacketOnTimeout asserts that when the upload
// limiter cannot supply tokens within the WaitN deadline, packets are
// dropped: counter.Upload() remains well below the sent byte total.
func TestUDPRelay_LimiterDropsPacketOnTimeout(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	// 100 B/s with burst ≈ 64 KB (the TokenBucketLimiter's minimum). A 1 KB
	// datagram costs ~10 seconds of refill at this rate — far beyond the
	// 10 ms WaitN deadline — so once the burst drains, each incoming packet
	// should be dropped by the limiter.
	lim := NewTokenBucketLimiter(100, 100)
	counter := NewTrafficCounter()
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr: "127.0.0.1:0",
		TargetAddr: echoAddr,
		Counter:    counter,
		LimiterUp:  lim,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	// Drain the burst upfront so the first relayed packet already has to
	// wait on refill and should hit the WaitN deadline.
	if err := lim.WaitN(context.Background(), 64*1024); err != nil {
		t.Fatalf("warmup WaitN: %v", err)
	}

	client := dialUDP(t, relay.ListenAddr())
	defer client.Close()

	const packets = 20
	const payloadSize = 1024
	payload := make([]byte, payloadSize)
	for i := 0; i < packets; i++ {
		if _, err := client.Write(payload); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Give the relay plenty of time to process every packet (drops included).
	time.Sleep(500 * time.Millisecond)

	total := int64(packets * payloadSize)
	up := counter.Upload()
	// At least half of the bytes should have been dropped — the exact
	// number depends on scheduler jitter and token refill, but the
	// limiter-bound path should never let all bytes through.
	if up >= total/2 {
		t.Errorf("expected majority of bytes dropped under rate-limiting; sent=%d, forwarded=%d", total, up)
	}
	t.Logf("sent=%d forwarded=%d (drop ratio ~%.1f%%)", total, up, 100*float64(total-up)/float64(total))
}

// countUDPRelayGoroutines returns the number of live goroutines whose stack
// mentions relay_udp.go. This is more reliable than runtime.NumGoroutine()
// when the test binary has background goroutines (echo servers, test
// runner, etc.) that jitter by ±1 independently of the relay.
func countUDPRelayGoroutines() int {
	return strings.Count(goroutineDump(), "relay_udp.go")
}

// goroutineDump returns all goroutine stacks as a string.
func goroutineDump() string {
	buf := make([]byte, 64*1024)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// gateClientLimiter wraps a ClientLimiter and blocks Release on a signal
// channel until the test explicitly releases the gate. This lets a test
// hold a reap goroutine in a measurable "in-progress" state so we can
// observe whether Stop waited for it.
type gateClientLimiter struct {
	inner  ClientLimiter
	gate   chan struct{} // closed when the test wants Release to proceed
	inside chan struct{} // closed by Release the first time it enters
	once   sync.Once
}

func (s *gateClientLimiter) Acquire(ip string) bool   { return s.inner.Acquire(ip) }
func (s *gateClientLimiter) ConfirmAcquire(ip string) { s.inner.ConfirmAcquire(ip) }
func (s *gateClientLimiter) CancelAcquire(ip string)  { s.inner.CancelAcquire(ip) }
func (s *gateClientLimiter) Release(ip string) {
	s.once.Do(func() { close(s.inside) })
	<-s.gate
	s.inner.Release(ip)
}
func (s *gateClientLimiter) ActiveConnections() int64 { return s.inner.ActiveConnections() }
func (s *gateClientLimiter) CleanupExpiredSlots()     { s.inner.CleanupExpiredSlots() }
func (s *gateClientLimiter) OnSingleDirectionEnd(ip string) time.Time {
	return s.inner.OnSingleDirectionEnd(ip)
}
func (s *gateClientLimiter) RecordActivity(ip string)  { s.inner.RecordActivity(ip) }
func (s *gateClientLimiter) IsDrainExpired(ip string) bool { return s.inner.IsDrainExpired(ip) }

// TestUDPRelay_StopWaitsForOrphanReap locks in the F10 invariant:
// forwardUpstream's Write-error path spawns an async reapSession goroutine,
// and that goroutine MUST be tracked in r.wg so Stop's wg.Wait() includes
// it. Without F10 the goroutine is a "bare go" orphan: correctness-safe
// only because downstreamLoop (which IS in r.wg) happens to also reach
// reapSession via upstream.Close; that accidental coverage violates the
// "Stop returned = all spawned goroutines exited" contract.
//
// Test construction:
//  1. Inject a gateClientLimiter whose Release blocks on a test gate and
//     signals the test once (cl.inside) when any reaper enters it.
//  2. Establish a session. Close upstream so forwardUpstream.Write fails
//     on the next packet (and downstreamLoop's Read errors out too).
//  3. Trigger the Write error, which schedules the orphan reap.
//  4. Wait for the gate to be entered. Call Stop concurrently.
//  5. Assert Stop does NOT return while the gate is closed — if it did,
//     wg.Wait would be violating the contract.
//  6. Open the gate; Stop must return, and no relay_udp.go frames must
//     remain.
//
// NOTE on regression isolation: because downstreamLoop is also in r.wg
// and races to reapSession via the closed upstream, a purely-external
// black-box bug-detection test cannot differentiate "orphan in wg" from
// "downstreamLoop covers for orphan". This test instead locks down the
// full contract: any reaper blocking inside Release must hold Stop, and
// after the gate opens Stop returns with a clean goroutine table. That
// combined with F10's wg.Add in the source code is the contract.
func TestUDPRelay_StopWaitsForOrphanReap(t *testing.T) {
	echoAddr, stopEcho := startUDPEchoServer(t)
	defer stopEcho()

	baseline := countUDPRelayGoroutines()

	cl := &gateClientLimiter{
		inner:  NewTestClientLimiter(8),
		gate:   make(chan struct{}),
		inside: make(chan struct{}),
	}
	relay := NewUDPRelay(UDPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    echoAddr,
		ClientLimiter: cl,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}

	client := dialUDP(t, relay.ListenAddr())

	if _, err := client.Write([]byte("seed")); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if r := readOnce(t, client, time.Second); string(r) != "seed" {
		t.Fatalf("seed echo failed: %q", r)
	}

	key := client.LocalAddr().String()
	sAny, ok := relay.sessions.Load(key)
	if !ok {
		t.Fatal("session not in sessions map")
	}
	s := sAny.(*udpSession)

	// Poison downstreamLoop so it takes the ctx.Done early-exit branch in
	// its next loop iteration. That branch calls reapSession, but since we
	// will let the orphan win closeOnce first, downstreamLoop's reapSession
	// falls through to the fast-exit (first=false) path and doesn't touch
	// Release. This isolates the orphan as the only goroutine that reaches
	// the gated Release.
	s.cancel()

	// Close upstream so forwardUpstream.Write fails on the next packet.
	_ = s.upstream.Close()

	// Give downstreamLoop a moment to notice ctx cancellation and exit
	// (its deferred wg.Done fires then). This shrinks its window for
	// winning closeOnce before the orphan.
	time.Sleep(20 * time.Millisecond)

	// Trigger forwardUpstream.Write error → orphan reap.
	if _, err := client.Write([]byte("boom")); err != nil {
		t.Fatalf("trigger write: %v", err)
	}

	// Wait for a goroutine to enter Release.
	select {
	case <-cl.inside:
	case <-time.After(2 * time.Second):
		t.Fatal("no reap entered Release within 2s")
	}

	client.Close()
	stopDone := make(chan struct{})
	stopStart := time.Now()
	go func() {
		relay.Stop()
		close(stopDone)
	}()

	// Stop must NOT return while the orphan is gated. If it does, F10 is
	// broken: wg.Wait did not include the blocked orphan goroutine.
	select {
	case <-stopDone:
		t.Fatalf("Stop returned in %v while orphan reap is gated; wg.Wait missed it", time.Since(stopStart))
	case <-time.After(100 * time.Millisecond):
		// expected: Stop is blocked on the gated orphan
	}

	close(cl.gate)

	select {
	case <-stopDone:
		t.Logf("Stop returned %v after gate open", time.Since(stopStart))
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within 2s after gate opened")
	}

	if n := countUDPRelayGoroutines(); n > baseline {
		t.Errorf("Stop returned but %d UDPRelay goroutine(s) still live (baseline %d)\n%s", n, baseline, goroutineDump())
	}
}
