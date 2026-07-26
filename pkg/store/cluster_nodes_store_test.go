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

	want := store.ClusterNode{Name: "peer-1", Host: "10.0.0.1", Port: 5000}
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

	if err := s.Delete("peer-1"); err != nil {
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
func TestClusterNodesStore_UpsertIsIdempotentAndUpdates(t *testing.T) {
	s := newClusterNodesStore(t)

	for i := 0; i < 3; i++ {
		if err := s.Upsert(store.ClusterNode{Name: "peer-1", Host: "10.0.0.1", Port: 5000}); err != nil {
			t.Fatalf("Upsert #%d: %v", i, err)
		}
	}
	if nodes, _ := s.List(); len(nodes) != 1 {
		t.Fatalf("repeated Upsert produced %d rows, want 1", len(nodes))
	}

	moved := store.ClusterNode{Name: "peer-1", Host: "10.9.9.9", Port: 6000}
	if err := s.Upsert(moved); err != nil {
		t.Fatalf("Upsert after move: %v", err)
	}
	nodes, _ := s.List()
	if len(nodes) != 1 || nodes[0] != moved {
		t.Errorf("List = %+v, want the updated address [%+v]", nodes, moved)
	}
}

// TestClusterNodesStore_DeleteAbsentIsNoError: the reconcile pass deletes rows it
// believes are gone; racing with another delete must not surface as an error.
func TestClusterNodesStore_DeleteAbsentIsNoError(t *testing.T) {
	s := newClusterNodesStore(t)
	if err := s.Delete("never-existed"); err != nil {
		t.Errorf("Delete of an absent name returned %v, want nil", err)
	}
}

func TestClusterNodesStore_RejectsEmptyName(t *testing.T) {
	s := newClusterNodesStore(t)
	if err := s.Upsert(store.ClusterNode{Host: "10.0.0.1", Port: 5000}); err == nil {
		t.Error("Upsert accepted an empty node name; the name is the primary key")
	}
}
