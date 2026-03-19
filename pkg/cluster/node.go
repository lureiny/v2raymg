package cluster

import (
	"fmt"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

const NodeTimeOut int64 = 60

type Node struct {
	*proto.Node
	InToken             string // 远端节点访问本地节点时使用, 用于验证远端节点是否有权限访问本地节点
	OutToken            string // 本地节点访问远端节点时使用, 用于验证本地节点是否有权限访问远端节点
	GetHeartBeatTime    int64  // 上次获取该节点心跳的时间
	ReportHeartBeatTime int64  // 上次上报到该节点的时间
	CreateTime          int64
	isLocal             bool // 是否为从本地文件中加载的node, 本地节点是为了不使用中心节点的场景而设计的

	grpcClientConn *grpc.ClientConn
}

// 比较两个node是否相同, 相同返回true
func (n1 *Node) Compare(n2 *Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.ClusterName == n2.ClusterName && n1.Name == n2.Name
}

// 比较cluster node与proto node是否相同, 相同返回true
func (n1 *Node) CompareWithProtoNode(n2 *proto.Node) bool {
	return n1.Host == n2.Host && n1.Port == n2.Port && n1.ClusterName == n2.ClusterName && n1.Name == n2.Name
}

func (node *Node) GetGrpcClientConn() (*grpc.ClientConn, error) {
	var err error = nil
	if node.grpcClientConn == nil || node.grpcClientConn.GetState() != connectivity.Ready {
		addr := fmt.Sprintf("%s:%d", node.GetHost(), node.GetPort())
		node.grpcClientConn, err = grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	return node.grpcClientConn, err
}

func (n *Node) IsLocal() bool {
	return n.isLocal
}

// IsValid 有效返回true
func (node *Node) IsValid() bool {
	currentTime := time.Now().Unix()
	return node.GetHeartBeatTime+NodeTimeOut > currentTime ||
		node.ReportHeartBeatTime+NodeTimeOut > currentTime ||
		node.CreateTime+NodeTimeOut > currentTime
}

// IsCompleteRegister 是否完成双向注册
func (node *Node) IsCompleteRegister() bool {
	currentTime := time.Now().Unix()
	return node.GetHeartBeatTime+NodeTimeOut > currentTime &&
		node.ReportHeartBeatTime+NodeTimeOut > currentTime
}

// 本地是否已经在node上注册
func (node *Node) RegisteredRemote() bool {
	return node.OutToken != "" && node.ReportHeartBeatTime+NodeTimeOut > time.Now().Unix()
}

// 节点node在本地注册过
func (node *Node) RegisteredLocal() bool {
	return node.InToken != "" && node.GetHeartBeatTime+NodeTimeOut > time.Now().Unix()
}

func (node *Node) IsComplete() bool {
	return node.Host != "" && node.Port > 1000 && node.ClusterName != "" && node.Name != ""
}

type StaticNode struct {
	Name string `json:"name,omitempty"`
	Host string `json:"host,omitempty"`
	Port int32  `json:"port,omitempty"`
}

// IsValide 判断静态节点是否有效.
// 过滤条件：host+port 与本地节点完全相同（同一个实例），或 name 相同。
func (sn *StaticNode) IsValide(node *Node) bool {
	sameInstance := sn.Host == node.Host && sn.Port == node.Port
	sameName := sn.Name == node.Name
	return sn.Host != "" && sn.Port > 1000 && !sameInstance && !sameName
}

var globalLocalNode = &LocalNode{}

type LocalNode struct {
	proto.Node
	Token string // for req local rpc server
}

// GetLocalNode return global local node
func GetLocalNode() *LocalNode {
	return globalLocalNode
}
