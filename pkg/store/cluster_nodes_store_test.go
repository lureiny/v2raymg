package store_test

import (
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func newClusterNodesStore(t *testing.T) *store.SQLiteClusterNodesStore {
	t.Helper()
	mgr, err := store.NewStoreManager(filepath.Join(t.TempDir(), "test.db"), migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return store.NewSQLiteClusterNodesStore(mgr.DB())
}

func TestClusterNodesStore_EmptyIsNotNil(t *testing.T) {
	s := newClusterNodesStore(t)
	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if nodes == nil {
		t.Error("List returned nil; callers range over it and expect an empty slice")
	}
	if len(nodes) != 0 {
		t.Errorf("fresh store has %d nodes, want 0", len(nodes))
	}
}

func TestClusterNodesStore_UpsertListDelete(t *testing.T) {
	s := newClusterNodesStore(t)

	want := store.ClusterNode{NodeID: "id-1", Name: "peer-1", Host: "10.0.0.1", Port: 5000}
	if err := s.Upsert(want); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 1 || nodes[0] != want {
		t.Fatalf("List = %+v, want exactly [%+v]", nodes, want)
	}

	if err := s.Delete("id-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if nodes, _ := s.List(); len(nodes) != 0 {
		t.Errorf("after Delete, List = %+v, want empty", nodes)
	}
}

// TestClusterNodesStore_UpsertIsIdempotentAndUpdates: the reconcile pass re-upserts
// every registered peer on every tick, so a repeat must not duplicate rows; and a
// peer that moved must overwrite its old address rather than leave a stale row that
// would be dialled forever.
//
// This is now reachable in production, which it was not while rows were keyed by
// name: the in-memory entry's address was immutable, so no "same peer, new
// address" upsert could ever be issued. Identity keying plus entry replacement in
// NodeManager.ResolveRegistration is what produces it.
func TestClusterNodesStore_UpsertIsIdempotentAndUpdates(t *testing.T) {
	s := newClusterNodesStore(t)

	for i := 0; i < 3; i++ {
		if err := s.Upsert(store.ClusterNode{NodeID: "id-1", Name: "peer-1", Host: "10.0.0.1", Port: 5000}); err != nil {
			t.Fatalf("Upsert #%d: %v", i, err)
		}
	}
	if nodes, _ := s.List(); len(nodes) != 1 {
		t.Fatalf("repeated Upsert produced %d rows, want 1", len(nodes))
	}

	moved := store.ClusterNode{NodeID: "id-1", Name: "peer-1", Host: "10.9.9.9", Port: 6000}
	if err := s.Upsert(moved); err != nil {
		t.Fatalf("Upsert after move: %v", err)
	}
	nodes, _ := s.List()
	if len(nodes) != 1 || nodes[0] != moved {
		t.Errorf("List = %+v, want the updated address [%+v]", nodes, moved)
	}
}

// TestClusterNodesStore_RenameUpdatesSameRow pins the reason rows are keyed by
// identity. Under the old name-keyed schema a rename wrote a second row and
// orphaned the first, and nothing could ever collect it: the reconcile pass only
// deletes rows absent from memory, and the old name was absent from memory by
// definition.
func TestClusterNodesStore_RenameUpdatesSameRow(t *testing.T) {
	s := newClusterNodesStore(t)

	if err := s.Upsert(store.ClusterNode{NodeID: "id-1", Name: "before", Host: "10.0.0.1", Port: 5000}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Upsert(store.ClusterNode{NodeID: "id-1", Name: "after", Host: "10.0.0.1", Port: 5000}); err != nil {
		t.Fatalf("Upsert after rename: %v", err)
	}

	nodes, _ := s.List()
	if len(nodes) != 1 {
		t.Fatalf("rename produced %d rows, want 1: %+v", len(nodes), nodes)
	}
	if nodes[0].Name != "after" {
		t.Errorf("name = %q, want after", nodes[0].Name)
	}
}

// TestClusterNodesStore_DistinctIdsCoexist: two nodes may legitimately share a
// name now that the directory is identity-keyed, so the store must not collapse
// them.
func TestClusterNodesStore_DistinctIdsCoexist(t *testing.T) {
	s := newClusterNodesStore(t)

	if err := s.Upsert(store.ClusterNode{NodeID: "id-a", Name: "dup", Host: "10.0.0.1", Port: 5000}); err != nil {
		t.Fatalf("Upsert a: %v", err)
	}
	if err := s.Upsert(store.ClusterNode{NodeID: "id-b", Name: "dup", Host: "10.0.0.2", Port: 5000}); err != nil {
		t.Fatalf("Upsert b: %v", err)
	}

	if nodes, _ := s.List(); len(nodes) != 2 {
		t.Errorf("got %d rows, want 2: two identities sharing a name must both persist", len(nodes))
	}
}

// TestClusterNodesStore_DeleteAbsentIsNoError: the reconcile pass deletes rows it
// believes are gone; racing with another delete must not surface as an error.
func TestClusterNodesStore_DeleteAbsentIsNoError(t *testing.T) {
	s := newClusterNodesStore(t)
	if err := s.Delete("never-existed"); err != nil {
		t.Errorf("Delete of an absent id returned %v, want nil", err)
	}
}

func TestClusterNodesStore_RejectsEmptyID(t *testing.T) {
	s := newClusterNodesStore(t)
	if err := s.Upsert(store.ClusterNode{Name: "peer-1", Host: "10.0.0.1", Port: 5000}); err == nil {
		t.Error("Upsert accepted an empty node id; the id is the primary key")
	}
}

// TestClusterNodesStore_SeedsAreEmptyOnFreshDatabase: seeds exist only to carry a
// pre-2.8 directory across the upgrade. A database created at the current schema
// never had name-keyed rows, so there is nothing to replay.
func TestClusterNodesStore_SeedsAreEmptyOnFreshDatabase(t *testing.T) {
	s := newClusterNodesStore(t)
	seeds, err := s.ListSeeds()
	if err != nil {
		t.Fatalf("ListSeeds: %v", err)
	}
	if seeds == nil {
		t.Error("ListSeeds returned nil; callers range over it")
	}
	if len(seeds) != 0 {
		t.Errorf("fresh database has %d seeds, want 0", len(seeds))
	}
	if err := s.ClearSeeds(); err != nil {
		t.Errorf("ClearSeeds on an empty table returned %v, want nil", err)
	}
}

// TestMigration16_UpgradesAnExistingV15Database is the data-loss guard for the
// schema change.
//
// Before 2.8 cluster_nodes was keyed by node name and had no identity column, so
// migration 16 has to rebuild the table. Those rows cannot become identified
// peers — nothing knows their node_id — but dropping them outright would leave a
// node whose config lists no static_nodes orphaned on its first restart after the
// upgrade, which is exactly what persisting the directory exists to prevent. They
// are drained into cluster_seeds instead and replayed once as address-only hints.
func TestMigration16_UpgradesAnExistingV15Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// Build a database at the pre-2.8 schema and put a row in it.
	upTo15 := migrations.All[:15]
	if v := upTo15[len(upTo15)-1].Version; v != 15 {
		t.Fatalf("expected to slice through version 15, got %d", v)
	}
	mgr, err := store.NewStoreManager(path, upTo15)
	if err != nil {
		t.Fatalf("NewStoreManager at v15: %v", err)
	}
	if _, err := mgr.DB().DB().Exec(
		`INSERT INTO cluster_nodes (name, host, port, created_at) VALUES (?, ?, ?, ?)`,
		"legacy-peer", "10.0.0.1", 5000, 1); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Re-open with the full migration set: version 16 must apply cleanly on top.
	mgr2, err := store.NewStoreManager(path, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager with migration 16: %v", err)
	}
	t.Cleanup(func() { _ = mgr2.Close() })
	s := store.NewSQLiteClusterNodesStore(mgr2.DB())

	nodes, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("cluster_nodes = %+v, want empty: a pre-2.8 row has no identity to key it by", nodes)
	}

	seeds, err := s.ListSeeds()
	if err != nil {
		t.Fatalf("ListSeeds: %v", err)
	}
	if len(seeds) != 1 {
		t.Fatalf("seeds = %+v, want the one drained legacy row", seeds)
	}
	if seeds[0].Host != "10.0.0.1" || seeds[0].Port != 5000 || seeds[0].Name != "legacy-peer" {
		t.Errorf("seed = %+v, want the legacy peer's address and name", seeds[0])
	}

	// The rebuilt table must be usable, and identity-keyed.
	if err := s.Upsert(store.ClusterNode{NodeID: "id-1", Name: "peer", Host: "10.0.0.2", Port: 5000}); err != nil {
		t.Fatalf("Upsert after migration: %v", err)
	}
	if nodes, _ := s.List(); len(nodes) != 1 || nodes[0].NodeID != "id-1" {
		t.Errorf("List = %+v, want the identity-keyed row", nodes)
	}

	// And the identity table exists.
	id, err := store.NewLocalIdentityStore(mgr2.DB()).GetOrCreate()
	if err != nil || id == "" {
		t.Errorf("GetOrCreate after migration: id=%q err=%v", id, err)
	}
}

// TestLocalIdentityStore_IsStableAcrossReopen: the id must survive restarts, or
// every restart would look to the cluster like a node being replaced.
func TestLocalIdentityStore_IsStableAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.db")

	mgr, err := store.NewStoreManager(path, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	first, err := store.NewLocalIdentityStore(mgr.DB()).GetOrCreate()
	if err != nil || first == "" {
		t.Fatalf("GetOrCreate: id=%q err=%v", first, err)
	}
	// Idempotent within one process.
	again, _ := store.NewLocalIdentityStore(mgr.DB()).GetOrCreate()
	if again != first {
		t.Errorf("GetOrCreate returned %q then %q in one process", first, again)
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	mgr2, err := store.NewStoreManager(path, migrations.All)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = mgr2.Close() })
	after, _ := store.NewLocalIdentityStore(mgr2.DB()).GetOrCreate()
	if after != first {
		t.Errorf("identity changed across a restart: %q -> %q; every restart would look "+
			"to peers like the node being replaced", first, after)
	}
}
