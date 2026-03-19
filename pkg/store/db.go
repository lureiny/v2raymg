package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB and provides SQLite-specific initialization.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path and applies best-practice PRAGMAs.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store.Open: %w", err)
	}

	// Single writer to avoid SQLITE_BUSY under WAL; reads can share the pool.
	sqlDB.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA cache_size=-20000",
	}
	for _, p := range pragmas {
		if _, err := sqlDB.Exec(p); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("store.Open pragma %q: %w", p, err)
		}
	}

	return &DB{db: sqlDB}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// DB returns the underlying *sql.DB for use by migrate/tx helpers.
func (d *DB) DB() *sql.DB {
	return d.db
}
