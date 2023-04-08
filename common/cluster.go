package common

import (
	"fmt"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
)

// 心跳探测上限制为60s, 即60s未上报则认为对应节点掉线
const HeartBeatTimeout = 60

type Cluster struct {
	Name           string
	Token          string // 集群的token, 用于校验节点是否可以注册
	NodeManager    NodeManager
	WrongTokenNode NodeManager // 存储注册到本地但是拥有wrongtoken的节点 key = node_name:token
}

func (cluster *Cluster) Init() {
	cluster.NodeManager = NewNodeManager()
	cluster.WrongTokenNode = NewNodeManager()
}

func (cluster *Cluster) Get(nodeName string) *Node {
	return cluster.NodeManager.Get(nodeName)
}

func (cluster *Cluster) LoadStaticNode() error {
	return cluster.NodeManager.LoadStaticNode()
}

func (cluster *Cluster) Add(node *Node) {
	cluster.NodeManager.Add(node.Name, node)
}

func (cluster *Cluster) GetNodeFromWrongNodeList(nodeName string) *Node {
	return cluster.WrongTokenNode.Get(nodeName)
}

func (cluster *Cluster) AddToWrongNodeList(node *Node) {
	cluster.WrongTokenNode.Add(node.Name, node)
}

// 根据clusterName和token验证该配置是否与本节点配置相同
func (cluster *Cluster) IsSameCluster(clusterName, token string) error {
	if cluster.Name != clusterName {
		return fmt.Errorf("wrong cluster")
	}
	if cluster.Token != token {
		return fmt.Errorf("wrong token")
	}
	return nil
}

// GetNodes 获取proto Node列表
// 返回满足过滤条件的node集合
func (cluster *Cluster) GetNodes(f nodeFilter) map[string]*proto.Node {
	nodeMap := map[string]*proto.Node{}
	for key, node := range cluster.NodeManager.GetNodes() {
		if f(node) {
			nodeMap[key] = node.Node
		}
	}
	return nodeMap
}

// 获取全部node名称, 包含无效node
func (cluster *Cluster) GetNodeNameList() []string {
	nodes := cluster.GetNodes(func(node *Node) bool { return true })
	nodeNameList := []string{}
	for nodeName := range nodes {
		nodeNameList = append(nodeNameList, nodeName)
	}
	return nodeNameList
}

// 验证通过后node存储的变更为本地的Node
func (cluster *Cluster) AuthRemoteNode(node **Node) error {
	// 验证token与node是否匹配
	// n本地记录的Node, node: 根据远端访问参数构建的Node
	if n, ok := cluster.NodeManager.HaveNode((*node).Name); !ok {
		return fmt.Errorf("node not exist")
	} else if (*node).InToken != n.InToken {
		return fmt.Errorf("wrong token")
	} else if n.GetHeartBeatTime != 0 && n.GetHeartBeatTime+int64(HeartBeatTimeout) < time.Now().Unix() {
		return fmt.Errorf("invalid token, token timeout")
	} else {
		// 验证通过后即可认为对方上报了一次心跳, 更新心跳上报时间
		n.GetHeartBeatTime = time.Now().Unix()
		*node = n
	}
	return nil
}

func (cluster *Cluster) Delete(nodeName string) {
	cluster.NodeManager.Delete(nodeName)
}

func (cluster *Cluster) DeleteFromWrongTokenNodeList(nodeName string) {
	cluster.WrongTokenNode.Delete(nodeName)
}

func (cluster *Cluster) Clear() {
	cluster.NodeManager.Clear()
}

// 判断该节点是否有效
func (cluster *Cluster) IsValid(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).IsValid()
}

// 判断节点node是否在本地注册过
func (cluster *Cluster) RegisteredLocal(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).RegisteredLocal()
}

// 过滤超时未上报的节点
func (cluster *Cluster) Filter() {
	currentTime := time.Now().Unix()
	cluster.NodeManager.Filter(getNodeFilterByCurrentTime(currentTime))
}

// 判断本地是否已经在远端节点注册
func (cluster *Cluster) RegisteredRemote(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).RegisteredRemote()
}
