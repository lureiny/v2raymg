package store

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Create directory if needed
	dir := filepath.Dir(dsn)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
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

	return &StoreManager{
		db:           db,
		userStore:    NewSQLiteUserStore(db),
		inboundStore: NewSQLiteInboundStore(db),
	}, nil
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
// hashFn is called with the user's plaintext password and must return a bcrypt hash.
// This is blocking and must be called before starting the HTTP server to ensure
// existing users can log in with their current proxy password.
func (m *StoreManager) InitLoginPasswords(hashFn func(string) (string, error)) error {
	sqlDB := m.db.DB()

	rows, err := sqlDB.Query(`SELECT username, password FROM users WHERE login_password = ''`)
	if err != nil {
		return fmt.Errorf("InitLoginPasswords: query: %w", err)
	}
	defer rows.Close()

	type row struct{ username, password string }
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.username, &r.password); err != nil {
			return fmt.Errorf("InitLoginPasswords: scan: %w", err)
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("InitLoginPasswords: rows: %w", err)
	}

	for _, r := range pending {
		hash, err := hashFn(r.password)
		if err != nil {
			return fmt.Errorf("InitLoginPasswords: hash user %q: %w", r.username, err)
		}
		if _, err := sqlDB.Exec(`UPDATE users SET login_password = ? WHERE username = ?`, hash, r.username); err != nil {
			return fmt.Errorf("InitLoginPasswords: update user %q: %w", r.username, err)
		}
	}
	return nil
}
