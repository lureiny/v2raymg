package cluster

import (
	"fmt"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
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
	cluster.WrongTokenNode.SetName("WrongNodeManager")
}

// Get get node by name
func (cluster *Cluster) Get(nodeName string) *Node {
	return cluster.NodeManager.Get(nodeName)
}

func (cluster *Cluster) LoadStaticNode(nodes []StaticNode) error {
	return cluster.NodeManager.LoadStaticNode(nodes)
}

// Add add node
func (cluster *Cluster) Add(node *Node) {
	cluster.NodeManager.Add(node.Name, node)
}

// GetNodeFromWrongNodeList ...
func (cluster *Cluster) GetNodeFromWrongNodeList(nodeName string) *Node {
	return cluster.WrongTokenNode.Get(nodeName)
}

// AddToWrongNodeList 将节点添加异常节点列表中
func (cluster *Cluster) AddToWrongNodeList(node *Node) {
	cluster.WrongTokenNode.Add(node.Name, node)
}

// IsSameCluster 根据clusterName和token验证该配置是否与本节点配置相同
func (cluster *Cluster) IsSameCluster(clusterName, token string) error {
	if cluster.Name != clusterName {
		return fmt.Errorf("wrong cluster")
	}
	if cluster.Token != token {
		return fmt.Errorf("wrong token")
	}
	return nil
}

// GetProtoNodesWithFilter 获取proto Node列表, 返回满足过滤条件的node集合
func (cluster *Cluster) GetProtoNodesWithFilter(f NodeFilter) map[string]*proto.Node {
	nodeMap := map[string]*proto.Node{}
	for key, node := range cluster.NodeManager.GetAllNode() {
		if f(node) {
			nodeMap[key] = node.Node
		}
	}
	return nodeMap
}

// GetAdvertisedNodes returns the node set this node advertises to its peers,
// together with its ComputeNodesSum digest — both derived from ONE snapshot so
// the map and the sum always correspond.
//
// This is the single definition point of the advertised set S(X). The heartbeat
// path uses it in three places (the sum sent every tick, the full map returned
// on a mismatch, and the full map pushed to a peer); if any of them computed the
// set with its own predicate, two nodes could advertise a sum for one set while
// exchanging a different one and never converge.
//
// The predicate is IsCompleteRegister() alone — deliberately WITHOUT the
// "name != self" exclusion the raw heartbeat response used to apply. The local
// node must be part of its own advertised set: otherwise A folds "everything
// except A" and B folds "everything except B", so their sums never match even
// when both hold the identical cluster view, and the mismatch path would run
// forever. The local node is registered with sentinel heartbeat timestamps (see
// InitEndNodeCluster), so IsCompleteRegister() is always true for it.
//
// IsCompleteRegister() is symmetric across a pair: it asserts "I received their
// heartbeat" and "I reported to them", which are the same two facts observed
// from either side. Hence B ∈ S(A) ⟺ A ∈ S(B), which is what makes converged
// peers agree on the sum.
func (cluster *Cluster) GetAdvertisedNodes() (map[string]*proto.Node, []byte) {
	nodeMap := cluster.GetProtoNodesWithFilter(func(node *Node) bool {
		return node.IsCompleteRegister()
	})
	list := make([]*proto.Node, 0, len(nodeMap))
	for _, n := range nodeMap {
		list = append(list, n)
	}
	sum, _ := ComputeNodesSum(list)
	return nodeMap, sum
}

// GetNodesWithFilter 获取cluster Node列表, 返回满足过滤条件的node集合
func (cluster *Cluster) GetNodesWithFilter(f NodeFilter) []*Node {
	return cluster.NodeManager.GetNodesWithFilter(f)
}

// 获取全部node名称, 包含无效node
func (cluster *Cluster) GetNodeNameList() []string {
	nodes := cluster.GetProtoNodesWithFilter(func(node *Node) bool { return true })
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
	if n := cluster.NodeManager.Get((*node).Name); n == nil {
		return fmt.Errorf("node not exist")
	} else if !(*node).Compare(n) {
		// node的meta info指 host+port+cluster+name, 这四个值可以指定唯一一个node
		return fmt.Errorf("there at least two node with same name[%s], but have different meta info.", n.GetName())
	} else if err := n.AuthAndTouch((*node).GetInToken()); err != nil {
		// 验证 token 与心跳未超时, 通过则原子刷新收到心跳时间 (check-then-act 在 Node 内加锁完成)
		return err
	} else {
		*node = n
	}
	return nil
}

// Delete delete node
func (cluster *Cluster) Delete(nodeName string) {
	cluster.NodeManager.Delete(nodeName)
}

// DeleteFromWrongTokenNodeList ...
func (cluster *Cluster) DeleteFromWrongTokenNodeList(nodeName string) {
	cluster.WrongTokenNode.Delete(nodeName)
}

func (cluster *Cluster) Clear() {
	cluster.NodeManager.Clear()
}

// IsValid 判断该节点是否有效
func (cluster *Cluster) IsValid(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).IsValid()
}

// RegisteredLocal 判断节点node是否在本地注册过
func (cluster *Cluster) RegisteredLocal(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).RegisteredLocal()
}

// Filter 过滤超时未上报的节点
func (cluster *Cluster) FilterTimeoutNode() {
	currentTime := time.Now().Unix()
	cluster.NodeManager.Filter(getNodeFilterByCurrentTime(currentTime))
}

// RegisteredRemote 判断本地是否已经在远端节点注册
func (cluster *Cluster) RegisteredRemote(nodeName string) bool {
	return cluster.NodeManager.Get(nodeName).RegisteredRemote()
}

// HaveNode 判断是否存在该node
func (cluster *Cluster) HaveNode(nodeName string) bool {
	return cluster.NodeManager.HaveNode(nodeName)
}

// GetAllNode ...
func (cluster *Cluster) GetAllNode() map[string]*Node {
	return cluster.NodeManager.GetAllNode()
}

// Filter ...
func (cluster *Cluster) Filter(f NodeFilter) {
	cluster.NodeManager.Filter(f)
	cluster.WrongTokenNode.Filter(func(n *Node) bool {
		// 过滤掉注册时间很久的wrong node
		return n.CreateTime+NodeTimeOut > time.Now().Unix()
	})
}
