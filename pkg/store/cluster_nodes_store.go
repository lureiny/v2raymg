package store

import (
	"fmt"
	"time"
)

// ClusterNode is one persisted peer: the identity needed to recognise it again,
// plus the address needed to dial it.
//
// NodeID is the primary key, not Name. Name, Host and Port are all mutable
// config fields — a peer that was renamed or moved must update its own row
// rather than accumulate a second one, and only the id survives those edits.
//
// Session state is deliberately absent. The in/out tokens are minted fresh by
// each registration and would be meaningless after a restart, and the heartbeat
// timestamps must NOT survive either — a loaded node has to look un-registered
// until it actually completes a handshake, or it would be advertised to peers as
// authenticated when nothing has been verified this run.
type ClusterNode struct {
	NodeID string
	Name   string
	Host   string
	Port   int32
}

// ClusterSeed is an address-only bootstrap hint with no known identity.
//
// It exists solely to carry the pre-2.8 directory across the upgrade: those rows
// were keyed by name and have no node_id, so they cannot become ClusterNodes.
// They are replayed once, at the next boot, as provisional directory entries and
// then cleared — without them a node whose config lists no static_nodes would be
// orphaned by the first restart after upgrading.
type ClusterSeed struct {
	Host string
	Port int32
	Name string
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
	// Delete removes a node by id. Deleting an absent id is not an error.
	Delete(nodeID string) error
	// ListSeeds returns the one-shot upgrade hints, if any remain.
	ListSeeds() ([]ClusterSeed, error)
	// ClearSeeds drops every hint. Called once they have been loaded.
	ClearSeeds() error
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
	rows, err := s.db.DB().Query(`SELECT node_id, name, host, port FROM cluster_nodes`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.List: %w", err)
	}
	defer rows.Close()

	nodes := []ClusterNode{}
	for rows.Next() {
		var n ClusterNode
		if err := rows.Scan(&n.NodeID, &n.Name, &n.Host, &n.Port); err != nil {
			return nil, fmt.Errorf("SQLiteClusterNodesStore.List scan: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.List rows: %w", err)
	}
	return nodes, nil
}

// Upsert inserts or refreshes a node. It is keyed by id, so a peer that was
// renamed or moved to a new address overwrites its own row instead of leaving a
// stale one behind under its old name.
func (s *SQLiteClusterNodesStore) Upsert(node ClusterNode) error {
	if node.NodeID == "" {
		return fmt.Errorf("SQLiteClusterNodesStore.Upsert: empty node id")
	}
	_, err := s.db.DB().Exec(`
		INSERT INTO cluster_nodes (node_id, name, host, port, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name, host = excluded.host, port = excluded.port`,
		node.NodeID, node.Name, node.Host, node.Port, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("SQLiteClusterNodesStore.Upsert %q: %w", node.NodeID, err)
	}
	return nil
}

// Delete removes a node by id; absent ids are a no-op.
func (s *SQLiteClusterNodesStore) Delete(nodeID string) error {
	if _, err := s.db.DB().Exec(`DELETE FROM cluster_nodes WHERE node_id = ?`, nodeID); err != nil {
		return fmt.Errorf("SQLiteClusterNodesStore.Delete %q: %w", nodeID, err)
	}
	return nil
}

// ListSeeds returns the pre-2.8 rows drained by migration 16, or an empty
// (non-nil) slice once they have been consumed.
func (s *SQLiteClusterNodesStore) ListSeeds() ([]ClusterSeed, error) {
	rows, err := s.db.DB().Query(`SELECT host, port, name FROM cluster_seeds`)
	if err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.ListSeeds: %w", err)
	}
	defer rows.Close()

	seeds := []ClusterSeed{}
	for rows.Next() {
		var sd ClusterSeed
		if err := rows.Scan(&sd.Host, &sd.Port, &sd.Name); err != nil {
			return nil, fmt.Errorf("SQLiteClusterNodesStore.ListSeeds scan: %w", err)
		}
		seeds = append(seeds, sd)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SQLiteClusterNodesStore.ListSeeds rows: %w", err)
	}
	return seeds, nil
}

// ClearSeeds drops every bootstrap hint. Seeds are a one-shot upgrade bridge:
// keeping them would resurrect addresses the cluster has legitimately moved on
// from, since nothing ever refreshes them.
func (s *SQLiteClusterNodesStore) ClearSeeds() error {
	if _, err := s.db.DB().Exec(`DELETE FROM cluster_seeds`); err != nil {
		return fmt.Errorf("SQLiteClusterNodesStore.ClearSeeds: %w", err)
	}
	return nil
}

// Close is a no-op: the DB handle is owned by StoreManager.
func (s *SQLiteClusterNodesStore) Close() error { return nil }
