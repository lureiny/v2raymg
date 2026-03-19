package usermanager

import "github.com/lureiny/v2raymg/pkg/store"

// UserStore is an alias for store.UserStore for backward compatibility.
type UserStore = store.UserStore

// SQLiteUserStore is an alias for store.SQLiteUserStore for backward compatibility.
type SQLiteUserStore = store.SQLiteUserStore

// NewSQLiteUserStore creates a new SQLiteUserStore backed by db.
var NewSQLiteUserStore = store.NewSQLiteUserStore
