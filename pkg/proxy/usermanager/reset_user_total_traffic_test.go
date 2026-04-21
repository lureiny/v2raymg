package usermanager

import (
	"strings"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// TestResetUserTotalTraffic_PersistsToDB verifies the cumulative traffic
// counters are cleared both in-memory and in the backing store.
func TestResetUserTotalTraffic_PersistsToDB(t *testing.T) {
	mockFM := &mockStatsForwardManager{}
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(mockFM, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	um.mu.Lock()
	u := um.users["alice"]
	u.TrafficTotalUplink = 1000
	u.TrafficTotalDownlink = 2000
	um.mu.Unlock()
	if err := storeMgr.UserStore().Save(u); err != nil {
		t.Fatalf("pre-save: %v", err)
	}

	if err := um.ResetUserTotalTraffic("alice"); err != nil {
		t.Fatalf("ResetUserTotalTraffic: %v", err)
	}

	um.mu.RLock()
	gotUp := um.users["alice"].TrafficTotalUplink
	gotDown := um.users["alice"].TrafficTotalDownlink
	um.mu.RUnlock()
	if gotUp != 0 || gotDown != 0 {
		t.Errorf("in-memory totals not zero: uplink=%d downlink=%d", gotUp, gotDown)
	}

	loaded, err := storeMgr.UserStore().Get("alice")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if loaded == nil {
		t.Fatalf("store.Get returned nil user")
	}
	if loaded.TrafficTotalUplink != 0 || loaded.TrafficTotalDownlink != 0 {
		t.Errorf("persisted totals not zero: uplink=%d downlink=%d",
			loaded.TrafficTotalUplink, loaded.TrafficTotalDownlink)
	}
}

// TestResetUserTotalTraffic_UserNotFound verifies the reset surfaces a clear
// error when the user is unknown.
func TestResetUserTotalTraffic_UserNotFound(t *testing.T) {
	um := NewUserManager(&mockStatsForwardManager{}, "test-node")
	err := um.ResetUserTotalTraffic("ghost")
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention username: %v", err)
	}
}

// TestResetUserTotalTraffic_NoDoubleCounting verifies that after a reset:
//   1. the in-memory and DB totals are zero
//   2. the next stats collection with unchanged forward counters does NOT
//      roll pre-reset traffic back into the user (drain aligned prevCounters)
//   3. traffic produced after the reset is correctly counted
func TestResetUserTotalTraffic_NoDoubleCounting(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{
				RuleKey:       "xray:in1:alice",
				Username:      "alice",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "in1",
				UplinkBytes:   1000,
				DownlinkBytes: 500,
			},
		},
	}
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(mockFM, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	// Use a long tick interval; we drive collection manually.
	um.StartTrafficStats(time.Hour)
	defer um.StopTrafficStats()

	// First collection: user accumulates 1000/500.
	um.ForceTrafficStatsCollection()
	waitForUserTotal(t, um, "alice", 1000, 500)

	// Reset clears totals and aligns prevCounters to forward's current value.
	if err := um.ResetUserTotalTraffic("alice"); err != nil {
		t.Fatalf("ResetUserTotalTraffic: %v", err)
	}
	assertUserTotal(t, um, "alice", 0, 0)
	loaded, err := storeMgr.UserStore().Get("alice")
	if err != nil || loaded == nil {
		t.Fatalf("store.Get: user=%v err=%v", loaded, err)
	}
	if loaded.TrafficTotalUplink != 0 || loaded.TrafficTotalDownlink != 0 {
		t.Errorf("persisted totals not zero after reset: %+v", loaded)
	}

	// Forward counters unchanged: the next collect must not re-add 1000/500.
	// onCollect is short-circuited by globalDelta==0 so there is no async work
	// to wait for; poll briefly to guard against ordering flakes.
	um.ForceTrafficStatsCollection()
	assertUserTotal(t, um, "alice", 0, 0)

	// Forward produces +500 uplink / +250 downlink after the reset.
	mockFM.setRecordTraffic(0, 1500, 750)
	um.ForceTrafficStatsCollection()
	waitForUserTotal(t, um, "alice", 500, 250)
}

// waitForUserTotal polls the user's in-memory traffic totals until they match
// the expected values or a short deadline passes.
func waitForUserTotal(t *testing.T, um *UserManager, username string, wantUp, wantDown int64) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		um.mu.RLock()
		u := um.users[username]
		if u != nil && u.TrafficTotalUplink == wantUp && u.TrafficTotalDownlink == wantDown {
			um.mu.RUnlock()
			return
		}
		um.mu.RUnlock()
		time.Sleep(20 * time.Millisecond)
	}
	um.mu.RLock()
	defer um.mu.RUnlock()
	u := um.users[username]
	var gotUp, gotDown int64
	if u != nil {
		gotUp = u.TrafficTotalUplink
		gotDown = u.TrafficTotalDownlink
	}
	t.Fatalf("timed out waiting for user %s totals: got uplink=%d downlink=%d, want uplink=%d downlink=%d",
		username, gotUp, gotDown, wantUp, wantDown)
}

func assertUserTotal(t *testing.T, um *UserManager, username string, wantUp, wantDown int64) {
	t.Helper()
	um.mu.RLock()
	defer um.mu.RUnlock()
	u := um.users[username]
	if u == nil {
		t.Fatalf("user %s not in users map", username)
	}
	if u.TrafficTotalUplink != wantUp || u.TrafficTotalDownlink != wantDown {
		t.Errorf("user %s totals: got uplink=%d downlink=%d, want uplink=%d downlink=%d",
			username, u.TrafficTotalUplink, u.TrafficTotalDownlink, wantUp, wantDown)
	}
}

// TestResetUserTotalTraffic_ConcurrentResetAndCollect verifies the
// reset↔collect critical race path: ResetUserTotalTraffic and collect()+onCollect
// run concurrently in tight loops; because the forward counter is fixed at
// 1000/500 and reset aligns prevCounters to that value, no amount of
// interleaving may push the user's persisted totals beyond 1000/500. A number
// larger than that would indicate pre-reset state leaked back through an
// in-flight snapshot — exactly the scenario the sc.mu -> m.mu lock order and
// synchronous onCollect are supposed to prevent. Run with -race to also catch
// plain data races on the shared maps.
func TestResetUserTotalTraffic_ConcurrentResetAndCollect(t *testing.T) {
	mockFM := &mockStatsForwardManager{
		records: []forward.ForwardTrafficRecord{
			{
				RuleKey:       "xray:in1:alice",
				Username:      "alice",
				ContainerType: contracts.ContainerXray,
				InboundTag:    "in1",
				UplinkBytes:   1000,
				DownlinkBytes: 500,
			},
		},
	}
	storeMgr := openTestStoreManager(t)
	um, err := NewUserManagerWithStore(mockFM, storeMgr, "test-node")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	um.StartTrafficStats(time.Hour) // manual drive only
	defer um.StopTrafficStats()

	const iterations = 200
	collectsDone := make(chan struct{})

	go func() {
		for i := 0; i < iterations; i++ {
			um.ForceTrafficStatsCollection()
		}
		close(collectsDone)
	}()

	for i := 0; i < iterations; i++ {
		if err := um.ResetUserTotalTraffic("alice"); err != nil {
			t.Errorf("reset iteration %d: %v", i, err)
		}
	}
	<-collectsDone

	// Let any pending ForceTrafficStatsCollection goroutines drain before
	// inspecting the final state.
	time.Sleep(100 * time.Millisecond)

	um.mu.RLock()
	gotUp := um.users["alice"].TrafficTotalUplink
	gotDown := um.users["alice"].TrafficTotalDownlink
	um.mu.RUnlock()

	if gotUp > 1000 || gotDown > 500 {
		t.Errorf("user total exceeded forward counter: uplink=%d downlink=%d (forward = 1000/500) — indicates reset↔collect race", gotUp, gotDown)
	}
}
