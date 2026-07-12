package ping

import (
	"errors"
	"testing"
	"time"
)

// TestBasePingChecker_StopSetsNotRunning covers finding #4: Stop must set
// isRunning to false (it wrongly stored true, making SetPingCheck a one-way
// switch and IsRunning permanently true).
func TestBasePingChecker_StopSetsNotRunning(t *testing.T) {
	bpc := newBasePingChecker()
	bpc.isRunning.Store(true)

	bpc.Stop()

	if bpc.IsRunning() {
		t.Fatal("IsRunning must be false after Stop")
	}
	if bpc.ctx.Err() == nil {
		t.Fatal("context must be cancelled after Stop")
	}
}

// TestIsTimedOut covers finding #1: the loss/timeout comparison must honor the
// configured timeout rather than the package-global pingTimeout.
func TestIsTimedOut(t *testing.T) {
	now := time.Now()
	if !isTimedOut(now.Add(-2*time.Second), now, time.Second) {
		t.Error("a 2s-old packet must be timed out at a 1s timeout")
	}
	if isTimedOut(now.Add(-500*time.Millisecond), now, time.Second) {
		t.Error("a 500ms-old packet must NOT be timed out at a 1s timeout")
	}
	// A larger configured timeout must keep a 2s-old packet alive.
	if isTimedOut(now.Add(-2*time.Second), now, 3*time.Second) {
		t.Error("a 2s-old packet must NOT be timed out at a 3s timeout")
	}
}

// TestNewPingerInfo_StoresConfiguredDurations verifies the configured
// interval/timeout are carried into pingerInfo (they were stored but the loop
// ignored them; this guards the plumbing the loop now reads).
func TestNewPingerInfo_StoresConfiguredDurations(t *testing.T) {
	pi := newPingerInfo(&PingNodeInfo{Host: "1.2.3.4"}, "icmp_ping", 10*time.Second, 3*time.Second)
	if pi.interval != 10*time.Second || pi.timeout != 3*time.Second {
		t.Fatalf("expected interval=10s timeout=3s, got interval=%v timeout=%v", pi.interval, pi.timeout)
	}
}

// TestRunPinger_CapturesError covers finding #3: a pinger Run failure (e.g. no
// CAP_NET_RAW) must be surfaced (logged + marked failed), not swallowed. The run
// seam lets us test this without opening a raw ICMP socket.
func TestRunPinger_CapturesError(t *testing.T) {
	pi := &pingerInfo{
		nodeInfo: &PingNodeInfo{Host: "1.2.3.4", Geo: "cn", ISP: "ct"},
		run:      func() error { return errors.New("operation not permitted") },
	}
	pi.runPinger()
	if !pi.failed.Load() {
		t.Fatal("a Run() error must mark the pinger failed (not be silently swallowed)")
	}
}

func TestRunPinger_NoErrorNotFailed(t *testing.T) {
	pi := &pingerInfo{
		nodeInfo: &PingNodeInfo{Host: "1.2.3.4"},
		run:      func() error { return nil },
	}
	pi.runPinger()
	if pi.failed.Load() {
		t.Fatal("a clean Run() must not mark the pinger failed")
	}
}
