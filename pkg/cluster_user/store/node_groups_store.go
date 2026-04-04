package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	pkgstore "github.com/lureiny/v2raymg/pkg/store"
)

// NodeGroupsStore is the persistence interface for the local node group assignments.
type NodeGroupsStore interface {
	// List returns all group names currently assigned to this node.
	List() ([]string, error)
	// Set atomically replaces all group assignments.
	// Pass an empty slice to clear all groups.
	Set(groups []string) error
	// Close releases any held resources.
	Close() error
}

// SQLiteNodeGroupsStore implements NodeGroupsStore using the shared SQLite DB.
type SQLiteNodeGroupsStore struct {
	db *pkgstore.DB
}

// NewSQLiteNodeGroupsStore creates a new SQLiteNodeGroupsStore backed by db.
// The local_node_groups table must already exist (run Migrate with migrations.All).
func NewSQLiteNodeGroupsStore(db *pkgstore.DB) *SQLiteNodeGroupsStore {
	return &SQLiteNodeGroupsStore{db: db}
}

// List returns all group names from local_node_groups.
// Returns an empty (non-nil) slice when no groups are stored.
func (s *SQLiteNodeGroupsStore) List() ([]string, error) {
	rows, err := s.db.DB().Query(`SELECT group_name FROM local_node_groups`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteNodeGroupsStore.List: %w", err)
	}
	defer rows.Close()

	groups := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("SQLiteNodeGroupsStore.List scan: %w", err)
		}
		groups = append(groups, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteNodeGroupsStore.List rows: %w", err)
	}
	return groups, nil
}

// Set atomically replaces all group assignments inside a single transaction.
// An empty groups slice clears all stored groups.
func (s *SQLiteNodeGroupsStore) Set(groups []string) error {
	now := time.Now().Unix()

	// Deduplicate group names to avoid PK conflicts.
	seen := make(map[string]struct{}, len(groups))
	unique := make([]string, 0, len(groups))
	for _, g := range groups {
		if _, dup := seen[g]; !dup {
			seen[g] = struct{}{}
			unique = append(unique, g)
		}
	}

	err := pkgstore.WithTx(context.Background(), s.db.DB(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM local_node_groups`); err != nil {
			return fmt.Errorf("delete local_node_groups: %w", err)
		}
		for _, g := range unique {
			if _, err := tx.Exec(
				`INSERT INTO local_node_groups (group_name, created_at, updated_at) VALUES (?, ?, ?)`,
				g, now, now,
			); err != nil {
				return fmt.Errorf("insert group %q: %w", g, err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("SQLiteNodeGroupsStore.Set: %w", err)
	}
	return nil
}

// Close is a no-op; the underlying DB lifecycle is managed by the caller.
func (s *SQLiteNodeGroupsStore) Close() error {
	return nil
}
