package server

import (
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ClusterState is the interface required by EndNodeServer for cluster membership operations.
// It is satisfied by the global/cluster package functions (via an adapter) and can be
// implemented by any pkg/cluster.EndNodeClusterManager wrapper.
type ClusterState interface {
	// GetClusterToken returns the shared cluster token used for authentication.
	GetClusterToken() string

	// IsSameCluster verifies that a peer's shared token matches the local cluster.
	// The token is the only membership boundary — it is also the HKDF input for
	// every end<->end RPC key — so nothing else needs checking.
	IsSameCluster(token string) error

	// AuthRemoteNode validates a remote node's token and updates its heartbeat time.
	AuthRemoteNode(node **cluster.Node) error

	// Get returns the node filed under a directory key, or nil if not found.
	// The key is a node_id, or a provisional address key for a peer that has not
	// identified itself yet; use cluster.DirectoryKey to derive it from a node.
	Get(key string) *cluster.Node

	// LookupPeer finds the entry a peer's self-description refers to, resolving
	// by identity first so a renamed or moved peer still matches its own entry.
	LookupPeer(claim *proto.Node) *cluster.Node

	// ResolveRegistration files a peer's self-description and reports what
	// changed — new, unchanged, moved, renamed, or a different identity taking
	// over an entry. It never refuses: the registering node is the authority on
	// its own name, address and identity.
	ResolveRegistration(claim *proto.Node, freshToken string, now int64) cluster.ResolveResult

	// AdoptIdentity upgrades the provisional entry at an address to the identity
	// that address reported, without stamping heartbeat timestamps.
	AdoptIdentity(host string, port int32, nodeID, name string) (*cluster.Node, bool)

	// RefreshName records a new name for an already-identified peer, keeping its
	// session and directory key. Names are display and addressing labels now, but
	// a stale one still makes ?target=<name> resolve on some nodes and not others.
	RefreshName(nodeID, name string) (*cluster.Node, bool)

	// MarkDirty tombstones a superseded node_id and drops its entry, so gossip
	// cannot reintroduce it before the cluster has converged.
	MarkDirty(nodeID string, now int64)

	// IsDirty reports whether a node_id is currently tombstoned.
	IsDirty(nodeID string, now int64) bool

	// Add registers a node in the local cluster.
	Add(node *cluster.Node)

	// AddIfAbsent files a node only when no existing entry already matches it,
	// atomically. Gossip must use this rather than Lookup-then-Add: those are two
	// lock acquisitions, and a registration landing in between would be
	// overwritten, destroying the session of a peer that just handshook.
	AddIfAbsent(node *cluster.Node) bool

	// Delete removes the node filed under a directory key.
	Delete(key string)

	// DeleteNode removes an entry only if the directory still holds this exact
	// node — a key recomputed from a stale pointer may belong to a different one.
	DeleteNode(node *cluster.Node)

	// GetAllNode returns all known nodes.
	GetAllNode() map[string]*cluster.Node

	// GetProtoNodesWithFilter returns nodes matching the filter as proto.Node map.
	GetProtoNodesWithFilter(f cluster.NodeFilter) map[string]*proto.Node

	// GetAdvertisedNodes returns the node set advertised to peers plus its digest,
	// both from one snapshot. This is the single definition of the advertised set:
	// the heartbeat sends the digest every tick, returns the map on a mismatch, and
	// pushes the map when reconciling — all three must be the same set or peers
	// never converge. See cluster.Cluster.GetAdvertisedNodes.
	GetAdvertisedNodes() (map[string]*proto.Node, []byte)

	// Filter removes nodes that do not satisfy the predicate.
	Filter(f cluster.NodeFilter)

	// AddToWrongNodeList records a node that failed cluster authentication.
	AddToWrongNodeList(node *cluster.Node)

	// DeleteFromWrongTokenNodeList removes a node from the wrong-token list.
	DeleteFromWrongTokenNodeList(nodeName string)

	// GetNodeFromWrongNodeList returns a node from the wrong-token list, or nil.
	GetNodeFromWrongNodeList(nodeName string) *cluster.Node
}

// NodeStore persists the dynamically discovered part of the node directory.
//
// Declared here rather than imported from pkg/store so the RPC layer keeps
// depending on a behaviour, not on SQLite — the same reason ClusterState exists.
// PersistedNode is one stored peer, keyed by identity.
//
// Name is stored alongside the address purely so a restart can log and address
// peers before they have said anything; it is not what identifies the row. Keying
// by name was what made a renamed or moved peer accumulate a second row rather
// than update its own.
type PersistedNode struct {
	NodeID string
	Name   string
	Host   string
	Port   int32
}

type NodeStore interface {
	// List returns every persisted peer.
	List() ([]PersistedNode, error)
	// Upsert records a peer that has completed bidirectional registration.
	Upsert(node PersistedNode) error
	// Delete removes a peer that is no longer in the in-memory directory.
	Delete(nodeID string) error
}
