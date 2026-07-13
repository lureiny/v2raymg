package forward

import (
	"fmt"
	"testing"
	"time"
)

func drainEndLen(l *remoteIPClientLimiter) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.drainEnd)
}

// TestClientLimiter_DrainEndBounded_CancelPath verifies the drainEnd map does
// not grow one stale entry per unique client IP. Each IP seeds a drain deadline
// and then has its slot removed via the CancelAcquire (pending->0) path; with
// the eviction the map must return to empty. Before the fix the entries were
// never deleted (ClearDrain had no callers), so drainEnd grew unbounded.
func TestClientLimiter_DrainEndBounded_CancelPath(t *testing.T) {
	l := newRemoteIPClientLimiter(ClientLimitConfig{
		MaxClients:              50,
		RecycleDelaySec:         60,
		SingleDirectionDrainSec: 2,
	})
	const n = 300
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("172.16.%d.%d", i/256, i%256)
		if !l.Acquire(ip) {
			t.Fatalf("Acquire(%s) rejected", ip)
		}
		l.OnSingleDirectionEnd(ip) // seed drainEnd[ip]
		l.CancelAcquire(ip)        // pending 0, active 0 -> slot removed + drain cleared
	}
	if got := drainEndLen(l); got != 0 {
		t.Fatalf("drainEnd leaked %d entries via the CancelAcquire path (want 0)", got)
	}
}

// TestClientLimiter_DrainEndBounded_RecyclePath covers the other slot-removal
// site: the real-time recycle timer in Release. With RecycleDelaySec=0 the
// timers fire promptly; drainEnd must drain to empty as slots are recycled.
func TestClientLimiter_DrainEndBounded_RecyclePath(t *testing.T) {
	const n = 300
	// RecycleDelaySec must be >= 1: newRemoteIPClientLimiter clamps <=0 to 60s,
	// which would push the recycle timers past the poll window.
	l := newRemoteIPClientLimiter(ClientLimitConfig{
		MaxClients:              n + 100, // headroom so async recycling never blocks Acquire
		RecycleDelaySec:         1,
		SingleDirectionDrainSec: 2,
	})
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("10.20.%d.%d", i/256, i%256)
		if !l.Acquire(ip) {
			t.Fatalf("Acquire(%s) rejected", ip)
		}
		l.ConfirmAcquire(ip)       // pending -> active
		l.OnSingleDirectionEnd(ip) // seed drainEnd[ip]
		l.Release(ip)              // active 0 -> recycle timer (delay 0) removes slot + drain
	}
	// Recycle timers fire ~1s after each Release; poll until the map drains.
	deadline := time.Now().Add(5 * time.Second)
	for drainEndLen(l) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := drainEndLen(l); got != 0 {
		t.Fatalf("drainEnd leaked %d entries via the Release recycle path (want 0)", got)
	}
}
