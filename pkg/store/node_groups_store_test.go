package store_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func openNodeGroupsStore(t *testing.T) store.NodeGroupsStore {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("store.Migrate: %v", err)
	}
	return store.NewSQLiteNodeGroupsStore(db)
}

func TestNodeGroupsStore_ListEmpty(t *testing.T) {
	s := openNodeGroupsStore(t)

	groups, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if groups == nil {
		t.Error("List returned nil, expected empty non-nil slice")
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestNodeGroupsStore_Set_List(t *testing.T) {
	s := openNodeGroupsStore(t)

	want := []string{"asia", "europe", "us"}
	if err := s.Set(want); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d groups, got %d", len(want), len(got))
	}

	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("group[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNodeGroupsStore_Set_Replaces(t *testing.T) {
	s := openNodeGroupsStore(t)

	if err := s.Set([]string{"a", "b", "c"}); err != nil {
		t.Fatalf("first Set: %v", err)
	}

	if err := s.Set([]string{"x", "y"}); err != nil {
		t.Fatalf("second Set: %v", err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 groups after replace, got %d: %v", len(got), got)
	}
}

func TestNodeGroupsStore_Set_Empty_ClearsAll(t *testing.T) {
	s := openNodeGroupsStore(t)

	if err := s.Set([]string{"grp1", "grp2"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if err := s.Set([]string{}); err != nil {
		t.Fatalf("Set empty: %v", err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got == nil {
		t.Error("List returned nil after clearing, expected empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 groups after clear, got %d: %v", len(got), got)
	}
}

func TestNodeGroupsStore_Close(t *testing.T) {
	s := openNodeGroupsStore(t)
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
