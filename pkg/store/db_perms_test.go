package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewStoreManager_DBFiles0600 guards the P2 fix: the users table stores
// plaintext auth_token, so the sqlite DB file (and its WAL/SHM sidecars) must be
// 0600, not the driver's default world-readable 0644.
func TestNewStoreManager_DBFiles0600(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "v2raymg.db")

	sm, err := NewStoreManager(dsn, []Migration{{Version: 1, SQL: "CREATE TABLE IF NOT EXISTS probe(x INTEGER)"}})
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	defer sm.Close()

	fi, err := os.Stat(dsn)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("db perms = %#o, want 0600 (plaintext auth_token must not be world-readable)", perm)
	}

	// WAL/SHM sidecars, when present, must be 0600 too.
	for _, suf := range []string{"-wal", "-shm"} {
		if si, err := os.Stat(dsn + suf); err == nil {
			if perm := si.Mode().Perm(); perm != 0600 {
				t.Errorf("%s perms = %#o, want 0600", suf, perm)
			}
		}
	}
}
