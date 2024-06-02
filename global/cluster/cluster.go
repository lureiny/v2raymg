package cluster

import (
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var globalEndNodeClusterManager = &cluster.EndNodeClusterManager{}
var LocalNode = cluster.GetLocalNode()

// GetClusterToken ...
func GetClusterToken() string {
	return globalEndNodeClusterManager.Token
}

func AuthRemoteNode(node **cluster.Node) error {
	return globalEndNodeClusterManager.AuthRemoteNode(node)
}

func initLocalNode() {
	LocalNode.Token = uuid.New().String()
	LocalNode.Node = proto.Node{
		Host:        config.GetString(common.ConfigProxyHost),
		Port:        int32(config.GetInt(common.ConfigServerRpcPort)),
		ClusterName: config.GetString(common.ConfigClusterName),
		Name:        config.GetString(common.ConfigServerName),
	}
}

func InitCluster() error {
	initLocalNode()
	globalEndNodeClusterManager.Init()
	globalEndNodeClusterManager.Name = config.GetString(common.ConfigClusterName)
	globalEndNodeClusterManager.Token = config.GetString(common.ConfigClusterToken)
	globalEndNodeClusterManager.Add(&cluster.Node{
		InToken:             LocalNode.Token,
		OutToken:            LocalNode.Token,
		Node:                &LocalNode.Node,
		ReportHeartBeatTime: math.MaxInt64 - common.NodeTimeOut,
		GetHeartBeatTime:    math.MaxInt64 - common.NodeTimeOut,
		CreateTime:          time.Now().Unix(),
	})
	return globalEndNodeClusterManager.LoadStaticNode()
}

// AddNode ...
func AddNode(node *cluster.Node) {
	globalEndNodeClusterManager.Add(node)
}

// IsSameCluster 根据clusterName和token验证该配置是否与本节点配置相同
func IsSameCluster(clusterName, token string) error {
	return globalEndNodeClusterManager.IsSameCluster(clusterName, token)
}

// AddToWrongNodeList 将节点添加异常节点列表中
func AddToWrongNodeList(node *cluster.Node) {
	globalEndNodeClusterManager.AddToWrongNodeList(node)
}

// DeleteFromWrongTokenNodeList ...
func DeleteFromWrongTokenNodeList(nodeName string) {
	globalEndNodeClusterManager.DeleteFromWrongTokenNodeList(nodeName)
}

// GetNodeFromWrongNodeList ...
func GetNodeFromWrongNodeList(nodeName string) *cluster.Node {
	return globalEndNodeClusterManager.GetNodeFromWrongNodeList(nodeName)
}

// Get get node by name
func Get(nodeName string) *cluster.Node {
	return globalEndNodeClusterManager.Get(nodeName)
}

// Add add node
func Add(node *cluster.Node) {
	globalEndNodeClusterManager.Add(node)
}

// GetProtoNodesWithFilter 获取proto Node列表, 返回满足过滤条件的node集合
func GetProtoNodesWithFilter(f cluster.NodeFilter) map[string]*proto.Node {
	return globalEndNodeClusterManager.GetProtoNodesWithFilter(f)
}

// GetNodesWithFilter 获取cluster Node列表, 返回满足过滤条件的node集合
func GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	return globalEndNodeClusterManager.GetNodesWithFilter(f)
}

// Delete delete node
func Delete(nodeName string) {
	globalEndNodeClusterManager.Delete(nodeName)
}

// GetAllNode ...
func GetAllNode() map[string]*cluster.Node {
	return globalEndNodeClusterManager.GetAllNode()
}

// Filter ...
func Filter(f cluster.NodeFilter) {
	globalEndNodeClusterManager.Filter(f)
}
