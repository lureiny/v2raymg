package cluster

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ClusterInitConfig carries the information needed to bootstrap an EndNodeClusterManager.
type ClusterInitConfig struct {
	// ClusterName is the shared cluster identifier.
	ClusterName string
	// ClusterToken is the shared secret for authentication and gRPC encryption.
	ClusterToken string
	// StaticNodes is an optional list of known peers for bootstrapping without a center node.
	StaticNodes []StaticNode
}

// NodeInitConfig carries the local node's identity.
type NodeInitConfig struct {
	Name      string
	Host      string
	Port      int32
}

// NewEndNodeClusterManagerFromConfig creates an EndNodeClusterManager, initialises the
// local node, registers it in the cluster, and loads any static peer nodes.
// Returns the manager, the local node, and any error.
func NewEndNodeClusterManagerFromConfig(clusterCfg ClusterInitConfig, nodeCfg NodeInitConfig) (*EndNodeClusterManager, *LocalNode, error) {
	localNode := GetLocalNode()
	localNode.Token = uuid.New().String()
	localNode.Node = proto.Node{
		Host:        nodeCfg.Host,
		Port:        nodeCfg.Port,
		ClusterName: clusterCfg.ClusterName,
		Name:        nodeCfg.Name,
	}

	mgr := &EndNodeClusterManager{}
	mgr.Init()
	mgr.Name = clusterCfg.ClusterName
	mgr.Token = clusterCfg.ClusterToken

	// Register the local node with permanent heartbeat timestamps so it is
	// never filtered out as "expired".
	mgr.Add(&Node{
		Node:                &localNode.Node,
		InToken:             localNode.Token,
		OutToken:            localNode.Token,
		ReportHeartBeatTime: math.MaxInt64 - NodeTimeOut,
		GetHeartBeatTime:    math.MaxInt64 - NodeTimeOut,
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
func (m *EndNodeClusterManager) IsSameCluster(clusterName, token string) error {
	return m.Cluster.IsSameCluster(clusterName, token)
}
