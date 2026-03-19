package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// --- Open / Close ---

func TestOpenClose(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestDB_ExposesUnderlying(t *testing.T) {
	db := openTemp(t)
	if db.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

// --- Migrate ---

func TestMigrate_FirstRun(t *testing.T) {
	db := openTemp(t)
	migrations := []Migration{
		{Version: 1, SQL: `CREATE TABLE foo (id INTEGER PRIMARY KEY)`},
		{Version: 2, SQL: `CREATE TABLE bar (id INTEGER PRIMARY KEY)`},
	}
	if err := Migrate(db, migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Verify tables exist.
	for _, tbl := range []string{"foo", "bar"} {
		var name string
		row := db.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl)
		if err := row.Scan(&name); err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := openTemp(t)
	m := []Migration{
		{Version: 1, SQL: `CREATE TABLE foo (id INTEGER PRIMARY KEY)`},
	}
	if err := Migrate(db, m); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	// Running again must not error (table already applied).
	if err := Migrate(db, m); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestMigrate_VersionGap(t *testing.T) {
	db := openTemp(t)
	m := []Migration{
		{Version: 1, SQL: `CREATE TABLE foo (id INTEGER PRIMARY KEY)`},
		{Version: 3, SQL: `CREATE TABLE bar (id INTEGER PRIMARY KEY)`}, // gap: 2 missing
	}
	err := Migrate(db, m)
	if err == nil {
		t.Fatal("expected error for version gap, got nil")
	}
}

func TestMigrate_PartialThenExtend(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db, []Migration{
		{Version: 1, SQL: `CREATE TABLE foo (id INTEGER PRIMARY KEY)`},
	}); err != nil {
		t.Fatalf("Migrate v1: %v", err)
	}
	// Add v2 in a second call.
	if err := Migrate(db, []Migration{
		{Version: 1, SQL: `CREATE TABLE foo (id INTEGER PRIMARY KEY)`},
		{Version: 2, SQL: `CREATE TABLE bar (id INTEGER PRIMARY KEY)`},
	}); err != nil {
		t.Fatalf("Migrate v2: %v", err)
	}
	var count int
	row := db.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 applied migrations, got %d", count)
	}
}

// --- WithTx ---

func TestWithTx_Commit(t *testing.T) {
	db := openTemp(t)
	sqlDB := db.DB()
	if _, err := sqlDB.Exec(`CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	err := WithTx(context.Background(), sqlDB, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO kv VALUES ('hello', 'world')`)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	var v string
	row := sqlDB.QueryRow(`SELECT v FROM kv WHERE k='hello'`)
	if err := row.Scan(&v); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if v != "world" {
		t.Fatalf("expected 'world', got %q", v)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	db := openTemp(t)
	sqlDB := db.DB()
	if _, err := sqlDB.Exec(`CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	wantErr := errors.New("intentional error")
	err := WithTx(context.Background(), sqlDB, func(tx *sql.Tx) error {
		if _, e := tx.Exec(`INSERT INTO kv VALUES ('hello', 'world')`); e != nil {
			return e
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	// Row must not have been committed.
	var count int
	row := sqlDB.QueryRow(`SELECT COUNT(*) FROM kv`)
	if e := row.Scan(&count); e != nil {
		t.Fatalf("scan: %v", e)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}
