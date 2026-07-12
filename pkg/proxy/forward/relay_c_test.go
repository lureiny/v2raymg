package forward

import (
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// startDribbleServer accepts one connection, discards whatever the client sends,
// and writes `chunks` fixed payloads spaced by `interval` (so it keeps the
// download direction active for chunks*interval). Returns the address and the
// total bytes it will send.
func startDribbleServer(t *testing.T, chunks int, interval time.Duration) (addr string, total int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	payload := []byte("0123456789abcdef") // 16 bytes
	total = chunks * len(payload)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		go io.Copy(io.Discard, conn) // drain the client's request half
		for i := 0; i < chunks; i++ {
			if _, err := conn.Write(payload); err != nil {
				return
			}
			time.Sleep(interval)
		}
	}()
	return ln.Addr().String(), total, func() { _ = ln.Close() }
}

// TestRelay_DrainRenewsOnActiveReverseTraffic covers findings #1/#2: after the
// client half-closes (upload EOF starts the drain), an actively-streaming
// download direction must renew the drain deadline and NOT be force-closed at
// drainSec. Before the fix, the relay compared a frozen drainDeadline snapshot,
// so the download was truncated ~drainSec after the half-close.
func TestRelay_DrainRenewsOnActiveReverseTraffic(t *testing.T) {
	// drainSec=1, download streams for ~3s (15 * 200ms).
	targetAddr, total, stopTarget := startDribbleServer(t, 15, 200*time.Millisecond)
	defer stopTarget()

	limiter := newRemoteIPClientLimiter(ClientLimitConfig{MaxClients: 1, SingleDirectionDrainSec: 1})
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    targetAddr,
		ClientLimiter: limiter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	c, err := net.Dial("tcp", relay.ListenAddr())
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte("hi"))
	// Half-close the upload direction to start the drain while download streams.
	if err := c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("closewrite: %v", err)
	}

	got, _ := io.ReadAll(c)
	if len(got) != total {
		t.Fatalf("download truncated by drain: got %d bytes, want %d (active reverse traffic was force-closed)", len(got), total)
	}
}

// TestRelay_DrainReclaimsIdleHalfOpen ensures the renewal change does not break
// idle reclamation: after a half-close with NO further reverse traffic, the
// connection is still closed within ~drainSec.
func TestRelay_DrainReclaimsIdleHalfOpen(t *testing.T) {
	// Target accepts, drains, and stays silent (0 chunks).
	targetAddr, _, stopTarget := startDribbleServer(t, 0, time.Millisecond)
	defer stopTarget()

	limiter := newRemoteIPClientLimiter(ClientLimitConfig{MaxClients: 1, SingleDirectionDrainSec: 1})
	relay := NewTCPRelay(TCPRelayConfig{
		ListenAddr:    "127.0.0.1:0",
		TargetAddr:    targetAddr,
		ClientLimiter: limiter,
	})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	c, err := net.Dial("tcp", relay.ListenAddr())
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer c.Close()

	_, _ = c.Write([]byte("hi"))
	_ = c.(*net.TCPConn).CloseWrite()

	// Read should hit EOF (connection reclaimed) well within a few drainSec.
	_ = c.SetReadDeadline(time.Now().Add(4 * time.Second))
	_, err = io.ReadAll(c)
	if err != nil && !errors.Is(err, io.EOF) {
		// A read deadline error means the idle half-open was NOT reclaimed.
		t.Fatalf("idle half-open not reclaimed within deadline: %v", err)
	}
}

// TestTCPRelay_DualStackListen covers finding #5: a wildcard listen must accept
// both IPv4 and IPv6 clients, not IPv4 only.
func TestTCPRelay_DualStackListen(t *testing.T) {
	// Skip if the host has no usable IPv6 loopback.
	if l6, err := net.Listen("tcp", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback available")
	} else {
		_ = l6.Close()
	}

	targetAddr, _, stopTarget := startDribbleServer(t, 1, time.Millisecond)
	defer stopTarget()

	relay := NewTCPRelay(TCPRelayConfig{ListenAddr: ":0", TargetAddr: targetAddr})
	if err := relay.Start(); err != nil {
		t.Fatalf("relay start: %v", err)
	}
	defer relay.Stop()

	_, port, err := net.SplitHostPort(relay.ListenAddr())
	if err != nil {
		t.Fatalf("split host port %q: %v", relay.ListenAddr(), err)
	}

	for _, host := range []string{"127.0.0.1", "::1"} {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 2*time.Second)
		if err != nil {
			t.Fatalf("dial %s: dual-stack listen did not accept this family: %v", host, err)
		}
		_ = c.Close()
	}
}

// --- accept backoff (#6) ---

type tempNetErr struct{}

func (tempNetErr) Error() string   { return "temporary accept error" }
func (tempNetErr) Timeout() bool   { return false }
func (tempNetErr) Temporary() bool { return true }

type fakeListener struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeListener) Accept() (net.Conn, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return nil, f.err
}
func (f *fakeListener) Close() error   { return nil }
func (f *fakeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4zero} }
func (f *fakeListener) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestTCPRelay_AcceptBackoffOnTemporaryError covers finding #6: a persistent
// temporary Accept error (e.g. EMFILE) must be handled with exponential backoff,
// not a 100% CPU busy loop. Over 200ms the loop should make only a handful of
// Accept calls (5ms,10,20,40,80,160... ~6), not thousands.
func TestTCPRelay_AcceptBackoffOnTemporaryError(t *testing.T) {
	r := NewTCPRelay(TCPRelayConfig{ListenAddr: "x", TargetAddr: "y"})
	fl := &fakeListener{err: tempNetErr{}}
	r.listener = fl

	r.wg.Add(1)
	go r.acceptLoop()
	time.Sleep(200 * time.Millisecond)
	r.cancel()
	r.wg.Wait()

	if n := fl.callCount(); n > 20 {
		t.Fatalf("accept loop busy-spun on temporary error: %d calls in 200ms (expected backoff, <20)", n)
	}
}

// TestTCPRelay_AcceptExitsOnPermanentError covers finding #6: a permanent Accept
// error must stop the accept loop, not spin forever.
func TestTCPRelay_AcceptExitsOnPermanentError(t *testing.T) {
	r := NewTCPRelay(TCPRelayConfig{ListenAddr: "x", TargetAddr: "y"})
	fl := &fakeListener{err: errors.New("permanent listener failure")}
	r.listener = fl

	done := make(chan struct{})
	r.wg.Add(1)
	go func() {
		r.acceptLoop()
		close(done)
	}()

	select {
	case <-done:
		// exited promptly — good
	case <-time.After(2 * time.Second):
		r.cancel()
		t.Fatal("accept loop did not exit on a permanent error (busy loop)")
	}
}
