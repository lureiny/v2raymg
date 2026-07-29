package store_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func newSecretTestStore(t *testing.T) *store.StoreManager {
	t.Helper()
	sm, err := store.NewStoreManager(filepath.Join(t.TempDir(), "s.db"), migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })
	return sm
}

func constGen(v string) func() (string, error) {
	return func() (string, error) { return v, nil }
}

func TestLocalSecretStore_GeneratesOnceThenReturnsTheSame(t *testing.T) {
	sm := newSecretTestStore(t)
	s := store.NewLocalSecretStore(sm.DB())

	got, created, err := s.GetOrCreate(store.SecretKeyHTTPToken, constGen("first"))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if !created {
		t.Error("the first call must report that it created the value")
	}
	if got != "first" {
		t.Fatalf("got %q, want %q", got, "first")
	}

	// A second call must not mint anything, even with a different generator:
	// regenerating would break every CLI config holding the old token.
	again, created, err := s.GetOrCreate(store.SecretKeyHTTPToken, constGen("second"))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if created {
		t.Error("a value that already exists must not be reported as created")
	}
	if again != "first" {
		t.Fatalf("stored value changed: got %q, want %q", again, "first")
	}
}

// The property that makes this usable at all: a restart must hand back the same
// token, or the operator's tooling breaks every time the process bounces.
func TestLocalSecretStore_SurvivesReopen(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "s.db")

	sm1, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	first, _, err := store.NewLocalSecretStore(sm1.DB()).GetOrCreate(store.SecretKeyHTTPToken, constGen("stable"))
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	_ = sm1.Close()

	sm2, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sm2.Close()

	second, created, err := store.NewLocalSecretStore(sm2.DB()).GetOrCreate(store.SecretKeyHTTPToken, constGen("different"))
	if err != nil {
		t.Fatalf("GetOrCreate after reopen: %v", err)
	}
	if created {
		t.Error("reopening must not look like a first creation")
	}
	if second != first {
		t.Fatalf("token changed across restart: %q -> %q", first, second)
	}
}

func TestLocalSecretStore_KeysAreIndependent(t *testing.T) {
	s := store.NewLocalSecretStore(newSecretTestStore(t).DB())

	a, _, err := s.GetOrCreate("key-a", constGen("value-a"))
	if err != nil {
		t.Fatalf("GetOrCreate a: %v", err)
	}
	b, created, err := s.GetOrCreate("key-b", constGen("value-b"))
	if err != nil {
		t.Fatalf("GetOrCreate b: %v", err)
	}
	if !created {
		t.Error("a distinct key must be created independently")
	}
	if a == b {
		t.Fatal("two keys returned the same value")
	}
}

func TestLocalSecretStore_GeneratorFailurePropagates(t *testing.T) {
	s := store.NewLocalSecretStore(newSecretTestStore(t).DB())
	want := errors.New("no entropy")

	if _, _, err := s.GetOrCreate("k", func() (string, error) { return "", want }); !errors.Is(err, want) {
		t.Fatalf("want the generator error wrapped, got %v", err)
	}
}

// An empty secret would be worse than an error: it is exactly the value that
// makes getAuthHandlerFunc compare equal to a missing header.
func TestLocalSecretStore_RejectsEmptyGeneratedValue(t *testing.T) {
	s := store.NewLocalSecretStore(newSecretTestStore(t).DB())

	if _, _, err := s.GetOrCreate("k", constGen("")); err == nil {
		t.Fatal("an empty generated secret must be an error, not stored")
	}
}
