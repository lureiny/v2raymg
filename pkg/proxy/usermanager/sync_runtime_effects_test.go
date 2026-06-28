package usermanager

import (
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// newSyncRuntimeTestManager wires a UserManager to a real DefaultForwardManager
// so assertions about forward-layer state after SyncUpsertUser actually prove
// the helper ran and produced the right effects (not just that a mock method
// was called).
func newSyncRuntimeTestManager(t *testing.T) (*UserManager, *forward.DefaultForwardManager) {
	t.Helper()
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 30000,
		MaxPort: 40000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })
	return NewUserManager(fwdMgr, "test-node"), fwdMgr
}

// TestSyncUpsertUser_NewUser_AppliesRuntimeSideEffects verifies that when a
// fresh user arrives via cluster sync, its bandwidth and client-limit fields
// end up in the forward layer — the very side effect the HTTP path relies on
// via SetUserBandwidthLimit / SetUserClientLimit.
func TestSyncUpsertUser_NewUser_AppliesRuntimeSideEffects(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)

	incoming := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           100,
		OriginNode:            "node-b",
		BandwidthUploadBps:    2048,
		BandwidthDownloadBps:  4096,
		MaxClients:            3,
		ClientRecycleDelaySec: 42,
		ClientDrainSec:        7,
	}
	applied, err := um.SyncUpsertUser(incoming)
	if err != nil {
		t.Fatalf("SyncUpsertUser: %v", err)
	}
	if !applied {
		t.Fatal("expected new user to be applied")
	}

	upRate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload)
	if !ok || upRate != 2048 {
		t.Errorf("forward upload limit: ok=%v rate=%d, want ok=true rate=2048", ok, upRate)
	}
	downRate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthDownload)
	if !ok || downRate != 4096 {
		t.Errorf("forward download limit: ok=%v rate=%d, want ok=true rate=4096", ok, downRate)
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok {
		t.Fatal("expected client limit config to be present in forward layer")
	}
	if cfg.MaxClients != 3 || cfg.RecycleDelaySec != 42 || cfg.SingleDirectionDrainSec != 7 {
		t.Errorf("client limit config: %+v, want MaxClients=3 Recycle=42 Drain=7", cfg)
	}
}

// TestSyncUpsertUser_UpdateExisting_AppliesRuntimeSideEffects verifies that
// when an existing user is re-synced with newer values, the forward layer
// adopts the new bandwidth/client-limit in-place — this is the bug we're
// fixing where SyncUpsertUser only mutated in-memory fields.
func TestSyncUpsertUser_UpdateExisting_AppliesRuntimeSideEffects(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)

	base := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           100,
		OriginNode:            "node-b",
		BandwidthUploadBps:    1024,
		BandwidthDownloadBps:  2048,
		MaxClients:            2,
		ClientRecycleDelaySec: 30,
		ClientDrainSec:        3,
	}
	if _, err := um.SyncUpsertUser(base); err != nil {
		t.Fatalf("SyncUpsertUser seed: %v", err)
	}

	// Newer version from the same origin with different limits.
	updated := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           200, // newer
		OriginNode:            "node-b",
		BandwidthUploadBps:    8192,
		BandwidthDownloadBps:  0, // explicitly unlimited on download
		MaxClients:            5,
		ClientRecycleDelaySec: 60,
		ClientDrainSec:        2,
	}
	applied, err := um.SyncUpsertUser(updated)
	if err != nil {
		t.Fatalf("SyncUpsertUser update: %v", err)
	}
	if !applied {
		t.Fatal("expected newer version to be applied")
	}

	// Upload got raised 1024 -> 8192; download got cleared; client cap went 2 -> 5.
	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok || rate != 8192 {
		t.Errorf("forward upload limit after update: ok=%v rate=%d, want ok=true rate=8192", ok, rate)
	}
	if _, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthDownload); ok {
		t.Error("forward download limit should be unset (unlimited) after update to 0")
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok {
		t.Fatal("expected client limit config to remain present in forward layer")
	}
	if cfg.MaxClients != 5 || cfg.RecycleDelaySec != 60 || cfg.SingleDirectionDrainSec != 2 {
		t.Errorf("client limit config after update: %+v, want MaxClients=5 Recycle=60 Drain=2", cfg)
	}
}

// TestSetUserBandwidthLimit_HTTPPath_RaceFree exercises the shared helper
// under concurrent mutateUser activity. In an earlier revision the HTTP path
// released m.mu, re-acquired RLock to grab a *User pointer, then dereferenced
// fields outside the lock — -race flagged the read of BandwidthUploadBps as a
// data race against a concurrent SetUserBandwidthLimit. The fix takes a value
// snapshot under RLock and passes scalars into applyRuntimeSideEffects; this
// test is the regression oracle.
func TestSetUserBandwidthLimit_HTTPPath_RaceFree(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			rate := int64(1024 + (i%7)*128)
			if err := um.SetUserBandwidthLimit("alice", forward.BandwidthUpload, rate); err != nil {
				t.Errorf("SetUserBandwidthLimit upload: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := um.SetUserClientLimit("alice", 1+(i%5), 30, 1); err != nil {
				t.Errorf("SetUserClientLimit: %v", err)
				return
			}
		}
	}()
	wg.Wait()

	// Sanity: forward layer ended up with *some* positive limit; the exact
	// final value depends on scheduling but both dimensions must have been
	// pushed through the helper at least once.
	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok || rate <= 0 {
		t.Errorf("forward upload limit never applied: ok=%v rate=%d", ok, rate)
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok || cfg.MaxClients <= 0 {
		t.Errorf("forward client-limit config not applied: ok=%v cfg=%+v", ok, cfg)
	}
}

// TestRemoveUser_DropsForwardState guards the leak fix: RemoveUser must
// release the forward layer's per-user maps so that high user churn does not
// monotonically grow userBandwidth / userClientLimiters / userClientLimitConfigs.
func TestRemoveUser_DropsForwardState(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	if err := um.SetUserBandwidthLimit("alice", forward.BandwidthUpload, 2048); err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}
	if err := um.SetUserClientLimit("alice", 3, 30, 1); err != nil {
		t.Fatalf("SetUserClientLimit: %v", err)
	}

	// Precondition: forward layer has state for alice.
	if _, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok {
		t.Fatal("expected upload limit to be present before RemoveUser")
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("alice"); !ok {
		t.Fatal("expected client-limit config to be present before RemoveUser")
	}

	if err := um.RemoveUser(RemoveUserRequest{Username: "alice"}); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	if _, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); ok {
		t.Error("RemoveUser did not drop upload bandwidth state in the forward layer")
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("alice"); ok {
		t.Error("RemoveUser did not drop client-limit config in the forward layer")
	}
}

// TestSyncUpsertUser_Tombstone_DropsForwardState mirrors TestRemoveUser_DropsForwardState
// for the cluster sync path: receiving a tombstone for an active local user
// must release the forward per-user state in the same way the HTTP path does.
func TestSyncUpsertUser_Tombstone_DropsForwardState(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)

	active := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           100,
		OriginNode:            "node-b",
		BandwidthUploadBps:    4096,
		MaxClients:            4,
		ClientRecycleDelaySec: 30,
		ClientDrainSec:        1,
	}
	if _, err := um.SyncUpsertUser(active); err != nil {
		t.Fatalf("SyncUpsertUser active: %v", err)
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("alice"); !ok {
		t.Fatal("expected forward state to be present after active sync")
	}

	tombstone := &contracts.User{
		Username:    "alice",
		UpdatedAtUs: 200,
		OriginNode:  "node-b",
	}
	tombstone.MarkDeleting()
	if _, err := um.SyncUpsertUser(tombstone); err != nil {
		t.Fatalf("SyncUpsertUser tombstone: %v", err)
	}

	if _, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); ok {
		t.Error("tombstone sync did not drop upload bandwidth state")
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("alice"); ok {
		t.Error("tombstone sync did not drop client-limit config")
	}
}

// TestSetUserBandwidthLimit_HTTPPath_UsesSharedHelper is a tripwire: the HTTP
// path's UserManager.SetUserBandwidthLimit must keep producing the same
// forward-layer effect as the cluster path, because both now route through
// applyRuntimeSideEffects.
func TestSetUserBandwidthLimit_HTTPPath_UsesSharedHelper(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)
	if err := um.AddUserForTest("alice", nil); err != nil {
		t.Fatalf("AddUserForTest: %v", err)
	}
	// Seed the client-limit fields so the helper has something to push down for
	// the client-limit dimension as well.
	if err := um.SetUserClientLimit("alice", 4, 30, 1); err != nil {
		t.Fatalf("SetUserClientLimit: %v", err)
	}
	if err := um.SetUserBandwidthLimit("alice", forward.BandwidthUpload, 1500); err != nil {
		t.Fatalf("SetUserBandwidthLimit: %v", err)
	}

	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok || rate != 1500 {
		t.Errorf("upload limit: ok=%v rate=%d, want ok=true rate=1500", ok, rate)
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok || cfg.MaxClients != 4 {
		t.Errorf("client limit config: ok=%v cfg=%+v, want ok=true MaxClients=4", ok, cfg)
	}
}

// TestNewUserManagerWithStore_RehydratesRuntimeSideEffects guards the restart
// path: persisted per-user bandwidth and client-limit must be pushed into the
// forward layer on construction, otherwise a process restart silently leaves
// the user unrate-limited until an admin re-issues Set*Limit. Tombstoned users
// must be skipped so their DropUser'd state is not resurrected as a zero-value
// config.
func TestNewUserManagerWithStore_RehydratesRuntimeSideEffects(t *testing.T) {
	storeMgr := openTestStoreManager(t)

	alice := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		BandwidthUploadBps:    4096,
		BandwidthDownloadBps:  8192,
		MaxClients:            5,
		ClientRecycleDelaySec: 60,
		ClientDrainSec:        3,
	}
	if err := storeMgr.UserStore().Save(alice); err != nil {
		t.Fatalf("seed alice: %v", err)
	}

	ghost := &contracts.User{
		Username:           "ghost",
		AuthToken:          "22222222-2222-4222-8222-222222222222",
		BandwidthUploadBps: 1234,
	}
	ghost.MarkDeleting()
	if err := storeMgr.UserStore().Save(ghost); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}

	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
		MinPort: 30000,
		MaxPort: 40000,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })

	if _, err := NewUserManagerWithStore(fwdMgr, storeMgr, "test-node"); err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok || rate != 4096 {
		t.Errorf("upload limit after restart: ok=%v rate=%d, want ok=true rate=4096", ok, rate)
	}
	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthDownload); !ok || rate != 8192 {
		t.Errorf("download limit after restart: ok=%v rate=%d, want ok=true rate=8192", ok, rate)
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok {
		t.Fatal("client-limit config missing after restart")
	}
	if cfg.MaxClients != 5 || cfg.RecycleDelaySec != 60 || cfg.SingleDirectionDrainSec != 3 {
		t.Errorf("client-limit config after restart: %+v, want MaxClients=5 Recycle=60 Drain=3", cfg)
	}

	if _, ok := fwdMgr.GetUserBandwidthLimit("ghost", forward.BandwidthUpload); ok {
		t.Error("tombstoned user was rehydrated into forward layer; expected skip")
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("ghost"); ok {
		t.Error("tombstoned user's client-limit config was rehydrated; expected skip")
	}
}

// TestSyncUpsertUser_Reactivation_RestoresRuntimeState pins down the
// tombstone→active revival path. Previous revisions carried a TODO suggesting
// this branch might need to emit UserEventAdd so containers re-allocate ports;
// in practice the container handlers (snell/hysteria GetBindPort, xray
// syncUserToInbound) already take the "became visible" branch of
// UserEventUpdate because their addedUsers tracking was cleared by the earlier
// UserEventRemove. This test locks in that contract:
//  1. Forward layer state must be refilled by applyRuntimeSideEffects.
//  2. The emitted event must be UserEventUpdate (not Add) — if a future change
//     splits revival into its own branch it will trip this assertion and
//     force a review of every container handler.
func TestSyncUpsertUser_Reactivation_RestoresRuntimeState(t *testing.T) {
	um, fwdMgr := newSyncRuntimeTestManager(t)

	events := um.Subscribe()
	defer um.Unsubscribe(events)

	active := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           100,
		OriginNode:            "node-b",
		BandwidthUploadBps:    1024,
		MaxClients:            2,
		ClientRecycleDelaySec: 30,
		ClientDrainSec:        1,
	}
	if _, err := um.SyncUpsertUser(active); err != nil {
		t.Fatalf("seed active: %v", err)
	}

	tombstone := &contracts.User{
		Username:    "alice",
		UpdatedAtUs: 200,
		OriginNode:  "node-b",
	}
	tombstone.MarkDeleting()
	if _, err := um.SyncUpsertUser(tombstone); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if _, ok := fwdMgr.GetUserClientLimitConfig("alice"); ok {
		t.Fatal("precondition: tombstone should have cleared client-limit state")
	}

	revived := &contracts.User{
		Username:              "alice",
		AuthToken:             "11111111-1111-4111-8111-111111111111",
		UpdatedAtUs:           300,
		OriginNode:            "node-b",
		BandwidthUploadBps:    8192,
		BandwidthDownloadBps:  4096,
		MaxClients:            7,
		ClientRecycleDelaySec: 45,
		ClientDrainSec:        2,
	}
	applied, err := um.SyncUpsertUser(revived)
	if err != nil {
		t.Fatalf("revive: %v", err)
	}
	if !applied {
		t.Fatal("expected revival to apply")
	}

	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthUpload); !ok || rate != 8192 {
		t.Errorf("upload after revival: ok=%v rate=%d, want ok=true rate=8192", ok, rate)
	}
	if rate, ok := fwdMgr.GetUserBandwidthLimit("alice", forward.BandwidthDownload); !ok || rate != 4096 {
		t.Errorf("download after revival: ok=%v rate=%d, want ok=true rate=4096", ok, rate)
	}
	cfg, ok := fwdMgr.GetUserClientLimitConfig("alice")
	if !ok {
		t.Fatal("expected client-limit config to be refilled on revival")
	}
	if cfg.MaxClients != 7 || cfg.RecycleDelaySec != 45 || cfg.SingleDirectionDrainSec != 2 {
		t.Errorf("client-limit after revival: %+v, want MaxClients=7 Recycle=45 Drain=2", cfg)
	}

	// emitEvent is synchronous-non-blocking into a buffered channel, so by the
	// time SyncUpsertUser returned the events are already queued.
	want := []UserEventType{UserEventAdd, UserEventRemove, UserEventUpdate}
	got := make([]UserEventType, 0, len(want))
	for range want {
		select {
		case ev := <-events:
			got = append(got, ev.Type)
		default:
			t.Fatalf("event sequence truncated: got %v, want %v", got, want)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event sequence: %v, want %v (revival should emit UserEventUpdate, not UserEventAdd)", got, want)
		}
	}
}
