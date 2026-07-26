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

	// Get returns the node with the given name, or nil if not found.
	Get(nodeName string) *cluster.Node

	// Add registers a node in the local cluster.
	Add(node *cluster.Node)

	// Delete removes a node from the local cluster.
	Delete(nodeName string)

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
type NodeStore interface {
	// ListNames returns the names of every persisted peer. Only names are needed:
	// the reconcile pass uses them solely to find rows with no in-memory
	// counterpart.
	ListNames() ([]string, error)
	// Upsert records a peer that has completed bidirectional registration.
	Upsert(name, host string, port int32) error
	// Delete removes a peer that is no longer in the in-memory directory.
	Delete(name string) error
}
