package common

import (
	"fmt"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

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

// node连续60s没有更新则认为无效
const nodeTimeOut int64 = 60

// 比较两个node是否相同, 相同返回true
func (n1 *Node) Compare(n2 *Node) bool {
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

// 有效返回true
func (node *Node) IsValid() bool {
	currentTime := time.Now().Unix()
	return node.GetHeartBeatTime+nodeTimeOut > currentTime ||
		node.ReportHeartBeatTime+nodeTimeOut > currentTime ||
		node.CreateTime+nodeTimeOut > currentTime
}

// 本地是否已经在node上注册
func (node *Node) RegisteredRemote() bool {
	return node.OutToken != "" && node.ReportHeartBeatTime+nodeTimeOut > time.Now().Unix()
}

// 节点node在本地注册过
func (node *Node) RegisteredLocal() bool {
	return node.InToken != "" && node.GetHeartBeatTime+nodeTimeOut > time.Now().Unix()
}

func (node *Node) IsComplete() bool {
	return node.Host != "" && node.Port > 1000 && node.ClusterName != "" && node.Name != ""
}

type staticNode struct {
	Name string `json:"name,omitempty"`
	Host string `json:"host,omitempty"`
	Port int32  `json:"port,omitempty"`
}

// 静态节点不应该和本地节点重名
func (sn *staticNode) IsValide(node *Node) bool {
	return sn.Host != "" && sn.Host != node.Host &&
		sn.Port > 1000 &&
		sn.Name != node.Name
}

var GlobalLocalNode = &LocalNode{}

type LocalNode struct {
	proto.Node
	Token string // for req local rpc server
}
