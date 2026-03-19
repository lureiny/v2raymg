package store

import (
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
