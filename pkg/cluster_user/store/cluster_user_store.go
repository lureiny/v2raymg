package store

import (
	"database/sql"
	"fmt"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	pkgstore "github.com/lureiny/v2raymg/pkg/store"
)

// ClusterUserStore is the persistence interface for ClusterUser records.
type ClusterUserStore interface {
	// List returns all known ClusterUser records, including logically deleted ones.
	List() ([]*clusteruser.ClusterUser, error)
	// ListByGroup returns all ClusterUser records (including logically deleted)
	// whose target_group matches the given group name.
	// The caller is responsible for version arbitration before calling Upsert.
	ListByGroup(group string) ([]*clusteruser.ClusterUser, error)
	// Get returns the ClusterUser with the given username.
	// Returns (nil, nil) when not found.
	Get(username string) (*clusteruser.ClusterUser, error)
	// Upsert inserts or replaces a ClusterUser record.
	// Logical deletion is expressed by setting Deleted=true on the record.
	// The caller is responsible for version arbitration before calling Upsert.
	Upsert(u *clusteruser.ClusterUser) error
	// Count returns the total number of records (including logically deleted ones).
	Count() (int, error)
	// Close releases any held resources.
	Close() error
}

// SQLiteClusterUserStore implements ClusterUserStore using the shared SQLite DB.
type SQLiteClusterUserStore struct {
	db *pkgstore.DB
}

// NewSQLiteClusterUserStore creates a new SQLiteClusterUserStore backed by db.
// The cluster_users table must already exist (run Migrate with migrations.All).
func NewSQLiteClusterUserStore(db *pkgstore.DB) *SQLiteClusterUserStore {
	return &SQLiteClusterUserStore{db: db}
}

// List returns all ClusterUser records from the database.
func (s *SQLiteClusterUserStore) List() ([]*clusteruser.ClusterUser, error) {
	rows, err := s.db.DB().Query(`
		SELECT username, password, expire, role, target_group,
		       deleted, updated_at_us, origin_node, hash
		FROM cluster_users`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterUserStore.List: %w", err)
	}
	defer rows.Close()

	users := make([]*clusteruser.ClusterUser, 0)
	for rows.Next() {
		u, err := scanClusterUser(rows)
		if err != nil {
			return nil, fmt.Errorf("SQLiteClusterUserStore.List scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteClusterUserStore.List rows: %w", err)
	}
	return users, nil
}

// ListByGroup returns all ClusterUser records whose target_group matches the given
// group name, including logically deleted ones. Uses idx_cluster_users_target_group.
func (s *SQLiteClusterUserStore) ListByGroup(group string) ([]*clusteruser.ClusterUser, error) {
	rows, err := s.db.DB().Query(`
		SELECT username, password, expire, role, target_group,
		       deleted, updated_at_us, origin_node, hash
		FROM cluster_users WHERE target_group = ?
		ORDER BY username`, group)
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterUserStore.ListByGroup: %w", err)
	}
	defer rows.Close()

	users := make([]*clusteruser.ClusterUser, 0)
	for rows.Next() {
		u, err := scanClusterUser(rows)
		if err != nil {
			return nil, fmt.Errorf("SQLiteClusterUserStore.ListByGroup scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteClusterUserStore.ListByGroup rows: %w", err)
	}
	return users, nil
}

// Get returns the ClusterUser with the given username, or (nil, nil) if not found.
func (s *SQLiteClusterUserStore) Get(username string) (*clusteruser.ClusterUser, error) {
	row := s.db.DB().QueryRow(`
		SELECT username, password, expire, role, target_group,
		       deleted, updated_at_us, origin_node, hash
		FROM cluster_users
		WHERE username = ?`, username)

	u, err := scanClusterUserRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterUserStore.Get: %w", err)
	}
	return u, nil
}

// Upsert inserts or replaces a ClusterUser record.
func (s *SQLiteClusterUserStore) Upsert(u *clusteruser.ClusterUser) error {
	now := time.Now().Unix()
	deletedInt := 0
	if u.Deleted {
		deletedInt = 1
	}

	_, err := s.db.DB().Exec(`
		INSERT INTO cluster_users (
			username, password, expire, role, target_group,
			deleted, updated_at_us, origin_node, hash,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET
			password     = excluded.password,
			expire       = excluded.expire,
			role         = excluded.role,
			target_group = excluded.target_group,
			deleted      = excluded.deleted,
			updated_at_us = excluded.updated_at_us,
			origin_node  = excluded.origin_node,
			hash         = excluded.hash,
			updated_at   = excluded.updated_at`,
		u.Username, u.Password, u.Expire, u.Role, u.TargetGroup,
		deletedInt, u.UpdatedAtUs, u.OriginNode, u.Hash,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("SQLiteClusterUserStore.Upsert: %w", err)
	}
	return nil
}

// Count returns the total number of records in cluster_users.
func (s *SQLiteClusterUserStore) Count() (int, error) {
	var count int
	row := s.db.DB().QueryRow(`SELECT COUNT(*) FROM cluster_users`)
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("SQLiteClusterUserStore.Count: %w", err)
	}
	return count, nil
}

// Close is a no-op; the underlying DB lifecycle is managed by the caller.
func (s *SQLiteClusterUserStore) Close() error {
	return nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for the shared scan helper.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanClusterUser(row rowScanner) (*clusteruser.ClusterUser, error) {
	u := &clusteruser.ClusterUser{}
	var deletedInt int
	err := row.Scan(
		&u.Username, &u.Password, &u.Expire, &u.Role, &u.TargetGroup,
		&deletedInt, &u.UpdatedAtUs, &u.OriginNode, &u.Hash,
	)
	if err != nil {
		return nil, err
	}
	u.Deleted = deletedInt != 0
	return u, nil
}

func scanClusterUserRow(row *sql.Row) (*clusteruser.ClusterUser, error) {
	u := &clusteruser.ClusterUser{}
	var deletedInt int
	err := row.Scan(
		&u.Username, &u.Password, &u.Expire, &u.Role, &u.TargetGroup,
		&deletedInt, &u.UpdatedAtUs, &u.OriginNode, &u.Hash,
	)
	if err != nil {
		return nil, err
	}
	u.Deleted = deletedInt != 0
	return u, nil
}
