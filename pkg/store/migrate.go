package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
)`

// Migration represents a single schema migration.
type Migration struct {
	Version int
	SQL     string
}

// Migrate applies any pending migrations in ascending version order.
// Versions must be consecutive starting from 1 with no gaps relative to what
// has already been applied (i.e. the next expected version is maxApplied+1).
func Migrate(db *DB, migrations []Migration) error {
	sqlDB := db.DB()

	if _, err := sqlDB.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	var maxVersion int
	row := sqlDB.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err := row.Scan(&maxVersion); err != nil {
		return fmt.Errorf("migrate: query max version: %w", err)
	}

	// Sort a copy so callers don't need to pre-sort.
	sorted := make([]Migration, len(migrations))
	copy(sorted, migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })

	for _, m := range sorted {
		if m.Version <= maxVersion {
			continue // already applied
		}
		if m.Version != maxVersion+1 {
			return fmt.Errorf("migrate: version gap detected: expected %d, got %d", maxVersion+1, m.Version)
		}
		if err := applyMigration(sqlDB, m); err != nil {
			return err
		}
		maxVersion = m.Version
	}
	return nil
}

func applyMigration(db *sql.DB, m Migration) error {
	return WithTx(context.Background(), db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(m.SQL); err != nil {
			return fmt.Errorf("migrate: apply version %d: %w", m.Version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, m.Version); err != nil {
			return fmt.Errorf("migrate: record version %d: %w", m.Version, err)
		}
		return nil
	})
}
