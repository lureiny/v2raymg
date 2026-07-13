package usermanager

import (
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// TestGetAllDeltaTraffic_ConcurrentDrainAndCollect reproduces the P1 concurrency
// defect (usermanager.go:2273 / GetStats:1979): GetAllDeltaTraffic obtained the
// aggregate via GetStats — which RLocked, returned sc.stats whose ByUser map is
// SHARED, then released the lock — and ranged that shared map with NO lock held,
// concurrent with collect()'s sc.mu-guarded writes. That is a "concurrent map
// iteration and map write" fatal (whole-process crash) / DATA RACE. GetTrafficStats
// had the identical shared-map escape.
//
// The test hammers both lock-free reader paths against an active collect loop.
// It fails under -race before the fix (DATA RACE on sc.stats.ByUser) and passes
// after (drain snapshots under sc.mu; GetStats returns a private deep copy).
func TestGetAllDeltaTraffic_ConcurrentDrainAndCollect(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{RuleKey: "xray:in1:alice", Username: "alice", ContainerType: contracts.ContainerXray, InboundTag: "in1"},
			{RuleKey: "xray:in1:bob", Username: "bob", ContainerType: contracts.ContainerXray, InboundTag: "in1"},
		},
	}
	um := NewUserManager(mockFM, "test-node")
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest alice: %v", err)
	}
	if err := um.AddUserForTest("bob", nil); err != nil {
		t.Fatalf("AddUserForTest bob: %v", err)
	}

	um.StartTrafficStats(time.Hour) // manual drive only
	defer um.StopTrafficStats()

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: grow the forward counters and drive collect(), which writes
	// sc.stats.ByUser/ByInbound/ByContainer under sc.mu.
	go func() {
		defer wg.Done()
		var up, down int64
		for i := 0; i < iterations; i++ {
			up += 100
			down += 200
			mockFM.setRecordTraffic(0, up, down)
			mockFM.setRecordTraffic(1, up, down)
			um.ForceTrafficStatsCollection()
		}
	}()

	// Reader: the two lock-free escape paths. Ranging the returned map must be
	// safe — it has to be a private copy, never the collector's live map.
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			for range um.GetAllDeltaTraffic(true) {
			}
			all := um.GetTrafficStats()
			for range all.ByUser {
			}
			for range all.ByInbound {
			}
			for range all.ByContainer {
			}
		}
	}()

	wg.Wait()
}

// TestGetAllDeltaTraffic_NoLostTrafficUnderConcurrency pins the accounting
// invariant behind the second half of the defect: the old GetAllDeltaTraffic
// read via GetStats and reset under a SEPARATE lock, so a collect() landing in
// the (nanosecond) gap zeroed a period's delta that was never returned
// (under-billing). A single writer produces a known total while a drainer
// concurrently calls the public GetAllDeltaTraffic(true); after a final drain
// the sum of everything returned must equal EXACTLY what the forward layer
// produced — no traffic lost, none double-counted. The atomic snapshot+reset
// makes this hold by construction.
//
// Note: this is a forward-looking conservation guard, not the fail-before anchor
// for the fix — the old two-lock gap is too narrow to lose deltas reliably in a
// test. TestGetAllDeltaTraffic_ConcurrentDrainAndCollect is the deterministic
// fail-before/pass-after case (it reproduces the shared-map fatal directly).
//
// A bare, unstarted statsCollector is installed so collect() has exactly one
// driver (this test) — no background collectLoop acting as a second writer.
func TestGetAllDeltaTraffic_NoLostTrafficUnderConcurrency(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{RuleKey: "xray:in1:alice", Username: "alice", ContainerType: contracts.ContainerXray, InboundTag: "in1"},
		},
	}
	um := NewUserManager(mockFM, "test-node")
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest alice: %v", err)
	}
	// Install a collector but do NOT Start() it, so the only collect() calls are
	// the ones this test issues from the writer goroutine.
	um.statsCollector = newStatsCollector(mockFM, time.Hour)

	const steps = 500
	const stepUp, stepDown = int64(100), int64(200)
	var drainedUp, drainedDown int64 // owned solely by the drainer goroutine

	writerDone := make(chan struct{})
	drainerDone := make(chan struct{})

	go func() {
		defer close(drainerDone)
		drain := func() {
			for _, d := range um.GetAllDeltaTraffic(true) {
				drainedUp += d.DeltaUplink
				drainedDown += d.DeltaDownlink
			}
		}
		for {
			select {
			case <-writerDone:
				drain() // final drain catches everything produced before the writer closed
				return
			default:
				drain()
			}
		}
	}()

	var up, down int64
	for i := 0; i < steps; i++ {
		up += stepUp
		down += stepDown
		mockFM.setRecordTraffic(0, up, down)
		um.statsCollector.collect()
	}
	close(writerDone)
	<-drainerDone

	if drainedUp != steps*stepUp || drainedDown != steps*stepDown {
		t.Fatalf("lost/duplicated traffic: drained up=%d down=%d, want up=%d down=%d",
			drainedUp, drainedDown, steps*stepUp, steps*stepDown)
	}
}

// TestGetUserDeltaTraffic_AtomicReadReset covers the per-user twin of the fix:
// GetUserDeltaTraffic(reset=true) must return the accrued delta and zero it in
// one atomic step (drainUserDelta), so the value is reported exactly once. A
// second read with no intervening collect must return zero.
func TestGetUserDeltaTraffic_AtomicReadReset(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{RuleKey: "xray:in1:alice", Username: "alice", ContainerType: contracts.ContainerXray, InboundTag: "in1"},
		},
	}
	um := NewUserManager(mockFM, "test-node")
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest alice: %v", err)
	}
	um.statsCollector = newStatsCollector(mockFM, time.Hour) // unstarted: single collect driver

	mockFM.setRecordTraffic(0, 300, 700)
	um.statsCollector.collect()

	got, ok := um.GetUserDeltaTraffic("alice", true /* reset */)
	if !ok {
		t.Fatal("expected alice stats")
	}
	if got.DeltaUplink != 300 || got.DeltaDownlink != 700 {
		t.Fatalf("first read: got up=%d down=%d, want 300/700", got.DeltaUplink, got.DeltaDownlink)
	}
	// No new traffic collected -> delta must have been zeroed by the reset.
	got2, ok := um.GetUserDeltaTraffic("alice", true)
	if !ok {
		t.Fatal("expected alice stats on second read")
	}
	if got2.DeltaUplink != 0 || got2.DeltaDownlink != 0 {
		t.Fatalf("second read after reset: got up=%d down=%d, want 0/0", got2.DeltaUplink, got2.DeltaDownlink)
	}
}
