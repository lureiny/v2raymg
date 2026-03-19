package store

import (
	"context"
	"database/sql"
	"log"
)

// WithTx runs fn inside a transaction. It commits on success and rolls back on
// error. A rollback failure is logged but does not replace the original error.
func WithTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) (retErr error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("store.WithTx: rollback failed: %v", rbErr)
			}
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
