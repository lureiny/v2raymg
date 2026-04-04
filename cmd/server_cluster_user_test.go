package cmd

import (
	"context"
	"path/filepath"
	"testing"

	clusteruserbootstrap "github.com/lureiny/v2raymg/pkg/cluster_user/bootstrap"
	clusterusercontroller "github.com/lureiny/v2raymg/pkg/cluster_user/controller"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// openTestDB opens a migrated in-memory (temp file) SQLite DB for tests.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.Migrate(db, migrations.All); err != nil {
		db.Close()
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestNewClusterUserStores_Disabled_ReturnsNil: cfg.Enabled=false → nil (feature off).
func TestNewClusterUserStores_Disabled_ReturnsNil(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{Enabled: false}
	got := newClusterUserStores(cfg, db, "node-a")
	if got != nil {
		t.Errorf("expected nil when disabled, got %+v", got)
	}
}

// TestNewClusterUserStores_Enabled_ReturnsNonNil: cfg.Enabled=true → all fields populated.
func TestNewClusterUserStores_Enabled_ReturnsNonNil(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{Enabled: true}
	got := newClusterUserStores(cfg, db, "node-b")
	if got == nil {
		t.Fatal("expected non-nil clusterUserStores when enabled")
	}
	if got.cuStore == nil {
		t.Error("cuStore is nil")
	}
	if got.ngStore == nil {
		t.Error("ngStore is nil")
	}
	if got.syncr == nil {
		t.Error("syncr is nil")
	}
}

// TestNewClusterUserStores_Enabled_StoresAreFunctional: the returned stores can
// execute basic operations without error (verifies they are properly wired to the DB).
func TestNewClusterUserStores_Enabled_StoresAreFunctional(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{Enabled: true, DefaultGroup: "default"}
	layer := newClusterUserStores(cfg, db, "node-c")
	if layer == nil {
		t.Fatal("expected non-nil")
	}

	// cuStore: List should return empty slice (no error).
	users, err := layer.cuStore.List()
	if err != nil {
		t.Fatalf("cuStore.List: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected empty list, got %d", len(users))
	}

	// ngStore: List should return empty slice (no error).
	groups, err := layer.ngStore.List()
	if err != nil {
		t.Fatalf("ngStore.List: %v", err)
	}
	_ = groups
}

// ---------------------------------------------------------------------------
// Startup wiring integration tests (Finding 2)
// ---------------------------------------------------------------------------

// TestStartupWiring_Disabled_NoComponentsAssembled: when cluster_user.enabled=false,
// newClusterUserStores returns nil, gating all downstream assembly (mirrors the
// `if cuLayer := newClusterUserStores(...); cuLayer != nil { ... }` block in server.go).
func TestStartupWiring_Disabled_NoComponentsAssembled(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{Enabled: false, DefaultGroup: "default", SyncIntervalSec: 5}

	layer := newClusterUserStores(cfg, db, "node-x")

	if layer != nil {
		t.Fatal("disabled config must produce nil layer — startup wiring would incorrectly assemble components")
	}
	// The if-block in server.go is skipped: no store, no syncer, no controller created.
}

// TestStartupWiring_Enabled_AllComponentsAssembled: when cluster_user.enabled=true,
// newClusterUserStores returns a fully-wired layer that can be passed directly to
// PlacementController without error — verifying the complete assembly path.
func TestStartupWiring_Enabled_AllComponentsAssembled(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{
		Enabled:         true,
		DefaultGroup:    "default",
		SyncIntervalSec: 1,
	}

	layer := newClusterUserStores(cfg, db, "node-y")
	if layer == nil {
		t.Fatal("enabled config must produce non-nil layer")
	}

	// Verify PlacementController can be constructed from the returned stores
	// (nil userMgr is acceptable for construction alone; Start() is not called).
	ctrl := clusterusercontroller.New(layer.cuStore, layer.ngStore, nil, cfg)
	if ctrl == nil {
		t.Error("PlacementController.New returned nil with valid stores")
	}
}

// ---------------------------------------------------------------------------
// Main-flow startup sequence tests (mirrors cmd/server.go step-by-step)
// ---------------------------------------------------------------------------

// TestStartupSequence_Disabled_NoBranchEntered: mirrors the `if cuLayer := ...; cuLayer != nil`
// guard in server.go — disabled config means the entire block is bypassed.
func TestStartupSequence_Disabled_NoBranchEntered(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{
		Enabled:            false,
		BootstrapFromLocal: true, // enabled flag overrides this
		DefaultGroup:       "default",
		SyncIntervalSec:    5,
	}

	// Step 1: gate — same as `if cuLayer := newClusterUserStores(...); cuLayer != nil`
	cuLayer := newClusterUserStores(cfg, db, "node-seq-off")
	if cuLayer != nil {
		t.Fatal("disabled config: cuLayer must be nil — startup branch must not be entered")
	}
	// If we got here the branch was skipped: bootstrap, rpcServer.InitClusterUser,
	// and PlacementController are all correctly omitted.
}

// TestStartupSequence_Enabled_WithBootstrap_FullFlow: mirrors the exact cmd/server.go
// sequence for cluster_user.enabled=true with bootstrap_from_local=true:
//  1. newClusterUserStores
//  2. bootstrapper.Bootstrap (with nil userMgr → no users imported, but no error)
//  3. PlacementController.New + Start + Stop
func TestStartupSequence_Enabled_WithBootstrap_FullFlow(t *testing.T) {
	db := openTestDB(t)
	cfg := appconfig.ClusterUserConfig{
		Enabled:            true,
		BootstrapFromLocal: true,
		DefaultGroup:       "default",
		SyncIntervalSec:    1,
	}

	// Step 1: create stores (same gate as server.go)
	cuLayer := newClusterUserStores(cfg, db, "node-seq-on")
	if cuLayer == nil {
		t.Fatal("enabled config: cuLayer must not be nil")
	}

	// Step 2: bootstrap (nil userMgr is safe — bootstrapper skips import when userMgr is nil)
	bootstrapper := clusteruserbootstrap.NewBootstrapper(
		cuLayer.cuStore, cuLayer.ngStore, nil,
		"node-seq-on", cfg.DefaultGroup,
	)
	if err := bootstrapper.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Verify bootstrap seeded the default group (mirrors "Step 1" in Bootstrap logic).
	groups, err := cuLayer.ngStore.List()
	if err != nil {
		t.Fatalf("ngStore.List after bootstrap: %v", err)
	}
	if len(groups) != 1 || groups[0] != "default" {
		t.Errorf("expected [default] after bootstrap, got %v", groups)
	}

	// Step 3: PlacementController assembly + brief Start/Stop
	// (nil userMgr is acceptable — reconcile loop no-ops when store is empty)
	ctrl := clusterusercontroller.New(cuLayer.cuStore, cuLayer.ngStore, nil, cfg)
	if ctrl == nil {
		t.Fatal("PlacementController.New returned nil")
	}
	ctrl.Start()
	ctrl.Stop() // clean shutdown — must not hang or panic
}
