package store

import (
	"fmt"
	"time"
)

// ClusterNode is one persisted peer: only the identity needed to dial it again.
//
// Session state is deliberately absent. The in/out tokens are minted fresh by
// each registration and would be meaningless after a restart, and the heartbeat
// timestamps must NOT survive either — a loaded node has to look un-registered
// until it actually completes a handshake, or it would be advertised to peers as
// authenticated when nothing has been verified this run.
type ClusterNode struct {
	Name string
	Host string
	Port int32
}

// ClusterNodesStore persists the dynamically discovered part of the node
// directory so a restart does not lose the cluster.
//
// Why this exists: the directory used to live only in memory. Peers drop an
// unreachable node after cluster.NodeTimeOut (60s) — address included — so a
// restart that took longer than a minute left the node orphaned: it knew nobody,
// and nobody knew it. With the directory persisted, a node comes back with its
// last known peers and re-registers; and because a node that is DOWN is not
// running its own eviction loop, its stored view stays frozen while it is away.
// That is what makes recovery converge even when the peers that recovered first
// have already forgotten it.
//
// Statically configured peers are NOT stored: they come from the config file on
// every start and are never evicted, so persisting them would only create a
// second, staler source of truth.
type ClusterNodesStore interface {
	// List returns every persisted node.
	List() ([]ClusterNode, error)
	// Upsert records a node that completed bidirectional registration.
	Upsert(node ClusterNode) error
	// Delete removes a node by name. Deleting an absent name is not an error.
	Delete(name string) error
	// Close releases any held resources.
	Close() error
}

// SQLiteClusterNodesStore implements ClusterNodesStore on the shared SQLite DB.
type SQLiteClusterNodesStore struct {
	db *DB
}

// NewSQLiteClusterNodesStore creates a store backed by db. The cluster_nodes
// table must already exist (run Migrate with migrations.All).
func NewSQLiteClusterNodesStore(db *DB) *SQLiteClusterNodesStore {
	return &SQLiteClusterNodesStore{db: db}
}

// List returns all persisted nodes, or an empty (non-nil) slice when there are none.
func (s *SQLiteClusterNodesStore) List() ([]ClusterNode, error) {
	rows, err := s.db.DB().Query(`SELECT name, host, port FROM cluster_nodes`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.List: %w", err)
	}
	defer rows.Close()

	nodes := []ClusterNode{}
	for rows.Next() {
		var n ClusterNode
		if err := rows.Scan(&n.Name, &n.Host, &n.Port); err != nil {
			return nil, fmt.Errorf("SQLiteClusterNodesStore.List scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.List rows: %w", err)
	}
	return nodes, nil
}

// Upsert inserts or refreshes a node. It is keyed by name, so a peer that moved
// to a new address overwrites its old row rather than accumulating a stale one.
func (s *SQLiteClusterNodesStore) Upsert(node ClusterNode) error {
	if node.Name == "" {
		return fmt.Errorf("SQLiteClusterNodesStore.Upsert: empty node name")
	}
	_, err := s.db.DB().Exec(`
		INSERT INTO cluster_nodes (name, host, port, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET host = excluded.host, port = excluded.port`,
		node.Name, node.Host, node.Port, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("SQLiteClusterNodesStore.Upsert %q: %w", node.Name, err)
	}
	return nil
}

// Delete removes a node by name; absent names are a no-op.
func (s *SQLiteClusterNodesStore) Delete(name string) error {
	if _, err := s.db.DB().Exec(`DELETE FROM cluster_nodes WHERE name = ?`, name); err != nil {
		return fmt.Errorf("SQLiteClusterNodesStore.Delete %q: %w", name, err)
	}
	return nil
}

// Close is a no-op: the DB handle is owned by StoreManager.
func (s *SQLiteClusterNodesStore) Close() error { return nil }
