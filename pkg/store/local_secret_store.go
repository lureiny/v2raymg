package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SecretKeyHTTPToken is the management API's static token, auto-generated when
// end_node.http_token is left empty.
const SecretKeyHTTPToken = "http_token"

// LocalSecretStore holds credentials this node minted for itself rather than
// read from its config.
//
// Persisted, not regenerated per boot. jwt_secret takes the other approach — a
// fresh secret each start, which merely forces users to log in again — but a
// management token cannot: operators put it in a CLI config, so regenerating it
// would break their tooling on every restart of every node. Tying it to the
// database instead gives it the same lifetime as the node's identity, which is
// the property that makes it usable.
//
// Like LocalIdentityStore, this follows the DATABASE. Deleting the database
// mints a new token; copying a data directory clones it.
type LocalSecretStore struct {
	db *DB
}

// NewLocalSecretStore creates a store backed by db. The local_secrets table
// must already exist (run Migrate with migrations.All).
func NewLocalSecretStore(db *DB) *LocalSecretStore {
	return &LocalSecretStore{db: db}
}

// GetOrCreate returns the stored secret for key, generating and persisting one
// with gen on first call. The second return value reports whether this call is
// the one that created it — callers use it to show a newly minted credential
// once, instead of on every boot.
//
// Concurrent callers converge: the insert is guarded by the primary key, and
// the follow-up read returns whichever writer won, so nobody walks away with a
// value that was not stored.
func (s *LocalSecretStore) GetOrCreate(key string, gen func() (string, error)) (string, bool, error) {
	if v, err := s.get(key); err != nil {
		return "", false, err
	} else if v != "" {
		return v, false, nil
	}

	generated, err := gen()
	if err != nil {
		return "", false, fmt.Errorf("LocalSecretStore.GetOrCreate generate %q: %w", key, err)
	}
	if generated == "" {
		return "", false, fmt.Errorf("LocalSecretStore.GetOrCreate: generator produced an empty %q", key)
	}

	res, err := s.db.DB().Exec(
		`INSERT OR IGNORE INTO local_secrets (key, value, created_at) VALUES (?, ?, ?)`,
		key, generated, time.Now().Unix())
	if err != nil {
		return "", false, fmt.Errorf("LocalSecretStore.GetOrCreate insert %q: %w", key, err)
	}

	// A losing racer inserts nothing; it must report "not created" so the value
	// is not announced twice.
	created := false
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		created = true
	}

	v, err := s.get(key)
	if err != nil {
		return "", false, err
	}
	if v == "" {
		return "", false, fmt.Errorf("LocalSecretStore.GetOrCreate: %q missing after insert", key)
	}
	return v, created, nil
}

// get returns the stored value, or "" when the key has never been set.
func (s *LocalSecretStore) get(key string) (string, error) {
	var v string
	err := s.db.DB().QueryRow(`SELECT value FROM local_secrets WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("LocalSecretStore.get %q: %w", key, err)
	}
	return v, nil
}
