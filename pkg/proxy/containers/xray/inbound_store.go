// Package xray provides Xray container implementation.
package xray

import "github.com/lureiny/v2raymg/pkg/store"

// InboundRecord is an alias for store.InboundRecord for backward compatibility.
type InboundRecord = store.InboundRecord

// InboundStore is an alias for store.InboundStore for backward compatibility.
type InboundStore = store.InboundStore

// SQLiteInboundStore is an alias for store.SQLiteInboundStore for backward compatibility.
type SQLiteInboundStore = store.SQLiteInboundStore

// NewSQLiteInboundStore creates a new SQLiteInboundStore backed by db.
var NewSQLiteInboundStore = store.NewSQLiteInboundStore
