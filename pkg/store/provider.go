package store

// Provider is a factory for store implementations backed by a shared DB.
type Provider interface {
	UserStore() UserStore
	InboundStore() InboundStore
}

// SQLiteProvider implements Provider using a single SQLite DB.
type SQLiteProvider struct {
	db *DB
}

// NewSQLiteProvider creates a new SQLiteProvider backed by db.
func NewSQLiteProvider(db *DB) *SQLiteProvider {
	return &SQLiteProvider{db: db}
}

// UserStore returns a UserStore backed by the provider's DB.
func (p *SQLiteProvider) UserStore() UserStore {
	return NewSQLiteUserStore(p.db)
}

// InboundStore returns an InboundStore backed by the provider's DB.
func (p *SQLiteProvider) InboundStore() InboundStore {
	return NewSQLiteInboundStore(p.db)
}
