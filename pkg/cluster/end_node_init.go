package cluster

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ClusterInitConfig carries the information needed to bootstrap an EndNodeClusterManager.
type ClusterInitConfig struct {
	// ClusterToken is the shared secret for authentication and gRPC encryption.
	ClusterToken string
	// StaticNodes is an optional list of known peers for bootstrapping without a center node.
	StaticNodes []StaticNode
}

// NodeInitConfig carries the local node's identity.
type NodeInitConfig struct {
	Name string
	Host string
	Port int32
	// ID is this node's permanent identity, read from (or minted into) the local
	// database by store.LocalIdentityStore. It is what lets peers recognise this
	// node across a rename, a move, or a port change. Empty only in tests and in
	// deployments running without persistence, which then fall back to the
	// pre-2.8 name/address matching.
	ID string
}

// NewEndNodeClusterManagerFromConfig creates an EndNodeClusterManager, initialises the
// local node, registers it in the cluster, and loads any static peer nodes.
// Returns the manager, the local node, and any error.
func NewEndNodeClusterManagerFromConfig(clusterCfg ClusterInitConfig, nodeCfg NodeInitConfig) (*EndNodeClusterManager, *LocalNode, error) {
	localNode := GetLocalNode()
	localNode.Token = uuid.New().String()
	// Every outbound request carries &localNode.Node (see rpcClient.NewNodeAuthInfo),
	// and the directory entry below embeds the same pointer, so writing the id
	// here is the only place it needs to be set — it propagates to registrations,
	// heartbeats and the advertised set for free. It must be written before Serve
	// starts, since from then on the value is read unlocked by other goroutines.
	localNode.Node = proto.Node{
		Host:   nodeCfg.Host,
		Port:   nodeCfg.Port,
		Name:   nodeCfg.Name,
		NodeId: nodeCfg.ID,
	}

	mgr := &EndNodeClusterManager{}
	mgr.Init()
	mgr.Token = clusterCfg.ClusterToken

	// Register the local node with permanent heartbeat timestamps so it is
	// never filtered out as "expired". These sentinel values (not now()) are
	// load-bearing: registerOrHeartBeatToEndNode may skip the local node's
	// periodic refresh, so the local node must never appear expired. This
	// in-package struct literal writes the unexported runtime fields directly
	// (single-threaded, before Add publishes the node) — do NOT use the
	// Set*/Touch accessors here, which would overwrite the sentinels with now().
	mgr.Add(&Node{
		Node:                &localNode.Node,
		isSelf:              true,
		inToken:             localNode.Token,
		outToken:            localNode.Token,
		reportHeartBeatTime: math.MaxInt64 - NodeTimeOut,
		recvHeartBeatTime:   math.MaxInt64 - NodeTimeOut,
		CreateTime:          time.Now().Unix(),
	})

	// Load static peers.
	if err := mgr.LoadStaticNode(clusterCfg.StaticNodes); err != nil {
		return nil, nil, err
	}
	return mgr, localNode, nil
}

// --- ClusterState interface implementation ---
// EndNodeClusterManager already has Get, Add, Delete, GetAllNode, GetProtoNodesWithFilter,
// Filter, AddToWrongNodeList, DeleteFromWrongTokenNodeList, GetNodeFromWrongNodeList, and
// AuthRemoteNode via the embedded Cluster. We only need to add GetClusterToken and
// IsSameCluster as thin wrappers.

// GetClusterToken returns the cluster's shared token.
func (m *EndNodeClusterManager) GetClusterToken() string {
	return m.Token
}

// IsSameCluster checks whether the given clusterName and token match this cluster.
func (m *EndNodeClusterManager) IsSameCluster(token string) error {
	return m.Cluster.IsSameCluster(token)
}
