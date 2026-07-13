package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StoreManager provides unified access to all persistence stores.
// It manages the database connection and provides access to UserStore and InboundStore.
type StoreManager struct {
	db           *DB
	userStore    UserStore
	inboundStore InboundStore
}

// NewStoreManager creates a new StoreManager with the given DSN and migrations.
// It creates the database directory if needed, opens the connection,
// runs migrations, and initializes all stores.
func NewStoreManager(dsn string, migrations []Migration) (*StoreManager, error) {
	// Create directory if needed. The DB stores plaintext auth tokens, so keep
	// the directory owner-only (a shared/existing dir keeps its perms — MkdirAll
	// does not chmod what already exists; the file perms below are the guarantee).
	dir := filepath.Dir(dsn)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}

	// Open database
	db, err := Open(dsn)
	if err != nil {
		return nil, err
	}

	// Run migrations
	if err := Migrate(db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Tighten perms on the DB file and its WAL/SHM sidecars (created by the
	// migrations above): the users table holds plaintext auth_token, which the
	// sqlite driver would otherwise leave world-readable (0644).
	secureSQLiteFiles(dsn)

	return &StoreManager{
		db:           db,
		userStore:    NewSQLiteUserStore(db),
		inboundStore: NewSQLiteInboundStore(db),
	}, nil
}

// secureSQLiteFiles chmods the sqlite database and its -wal/-shm sidecars to
// 0600. Best-effort: skips non-file DSNs (":memory:", or a DSN carrying query
// params we can't map to a path) and sidecars that don't exist yet.
func secureSQLiteFiles(dsn string) {
	if dsn == "" || strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "?") {
		return
	}
	for _, f := range []string{dsn, dsn + "-wal", dsn + "-shm"} {
		if err := os.Chmod(f, 0600); err != nil && !os.IsNotExist(err) {
			// Non-fatal: the DB is already open and usable. A failure to tighten
			// perms is a hardening gap, not a correctness error.
			continue
		}
	}
}

// UserStore returns the user store.
func (m *StoreManager) UserStore() UserStore {
	return m.userStore
}

// InboundStore returns the inbound store.
func (m *StoreManager) InboundStore() InboundStore {
	return m.inboundStore
}

// DB returns the underlying database connection (for advanced use).
func (m *StoreManager) DB() *DB {
	return m.db
}

// Close closes the database connection.
func (m *StoreManager) Close() error {
	return m.db.Close()
}

// InitLoginPasswords initializes login_password for users whose login_password is empty.
// hashFn is called with the user's auth_token and must return a bcrypt hash.
// This is blocking and must be called before starting the HTTP server to ensure
// existing users can log in with their current auth token.
func (m *StoreManager) InitLoginPasswords(hashFn func(string) (string, error)) error {
	sqlDB := m.db.DB()

	rows, err := sqlDB.Query(`SELECT username, auth_token FROM users WHERE login_password = ''`)
	if err != nil {
		return fmt.Errorf("InitLoginPasswords: query: %w", err)
	}
	defer rows.Close()

	type row struct{ username, authToken string }
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.username, &r.authToken); err != nil {
			return fmt.Errorf("InitLoginPasswords: scan: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("InitLoginPasswords: rows: %w", err)
	}

	for _, r := range pending {
		hash, err := hashFn(r.authToken)
		if err != nil {
			return fmt.Errorf("InitLoginPasswords: hash user %q: %w", r.username, err)
		}
		if _, err := sqlDB.Exec(`UPDATE users SET login_password = ? WHERE username = ?`, hash, r.username); err != nil {
			return fmt.Errorf("InitLoginPasswords: update user %q: %w", r.username, err)
		}
	}
	return nil
}
