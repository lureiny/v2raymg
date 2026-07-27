package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// LocalIdentityStore holds this node's own permanent identity.
//
// The id is minted once, on the first start against a given database, and never
// changes afterwards — not when the node is renamed, moved to a new host, given
// a new port, or upgraded. That stability is the whole point: peers key their
// directory by it, so every one of those edits becomes an in-place update of an
// existing entry instead of a stranger competing with a stale one.
//
// The identity is therefore tied to the DATABASE, not the config file. Two
// consequences worth knowing:
//
//   - Deleting the database mints a new id. Peers see the address answer with an
//     unfamiliar id, tombstone the old one, and adopt the new node. Costs one
//     heartbeat, no operator action.
//   - COPYING a data directory clones the identity, and two live nodes then
//     claim one directory slot. Peers detect it (a peer registering with our own
//     id is rejected; an id that keeps flipping between two addresses is logged)
//     but cannot repair it. Clone a config, never a data directory.
type LocalIdentityStore struct {
	db *DB
}

// NewLocalIdentityStore creates a store backed by db. The local_node_identity
// table must already exist (run Migrate with migrations.All).
func NewLocalIdentityStore(db *DB) *LocalIdentityStore {
	return &LocalIdentityStore{db: db}
}

// GetOrCreate returns this node's permanent id, minting and persisting one on
// first call. Concurrent callers converge on the same value: the insert is
// guarded by the single-row primary key, and the winner's row is what the
// follow-up read returns.
func (s *LocalIdentityStore) GetOrCreate() (string, error) {
	if id, err := s.get(); err != nil {
		return "", err
	} else if id != "" {
		return id, nil
	}

	if _, err := s.db.DB().Exec(
		`INSERT OR IGNORE INTO local_node_identity (id, node_id, created_at) VALUES (1, ?, ?)`,
		uuid.New().String(), time.Now().Unix()); err != nil {
		return "", fmt.Errorf("LocalIdentityStore.GetOrCreate insert: %w", err)
	}

	id, err := s.get()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("LocalIdentityStore.GetOrCreate: identity row missing after insert")
	}
	return id, nil
}

// get returns the stored id, or "" when none has been minted yet.
func (s *LocalIdentityStore) get() (string, error) {
	var id string
	err := s.db.DB().QueryRow(`SELECT node_id FROM local_node_identity WHERE id = 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("LocalIdentityStore.get: %w", err)
	}
	return id, nil
}

// Close is a no-op: the DB handle is owned by StoreManager.
func (s *LocalIdentityStore) Close() error { return nil }
