package cluster

import (
	"fmt"
	"strconv"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
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

// Get returns the node filed under a directory key (a node_id, or a provisional
// address key). To find a peer from its self-description use LookupPeer; to find
// one by the name an operator typed use FindByName.
func (cluster *Cluster) Get(key string) *Node {
	return cluster.NodeManager.Get(key)
}

// LookupPeer finds the entry a peer's self-description refers to (id, then
// address, then — only between two nodes that both lack an id — name).
func (cluster *Cluster) LookupPeer(claim *proto.Node) *Node {
	return cluster.NodeManager.LookupPeer(claim)
}

// FindByName returns the entry carrying this name and how many entries do.
// A count above one means two nodes are misconfigured with the same name;
// callers that address peers by name should surface that rather than guess.
func (cluster *Cluster) FindByName(name string) (*Node, int) {
	return cluster.NodeManager.FindByName(name)
}

// FindByAddr returns the entry dialling this address, or nil.
func (cluster *Cluster) FindByAddr(host string, port int32) *Node {
	return cluster.NodeManager.FindByAddr(host, port)
}

// ResolveRegistration files a peer's self-description and reports what changed.
func (cluster *Cluster) ResolveRegistration(claim *proto.Node, freshToken string, now int64) ResolveResult {
	return cluster.NodeManager.ResolveRegistration(claim, freshToken, now)
}

// AdoptIdentity upgrades the provisional entry at an address to the identity
// that address reported, preserving its session and without faking heartbeats.
func (cluster *Cluster) AdoptIdentity(host string, port int32, nodeID, name string) (*Node, bool) {
	return cluster.NodeManager.AdoptIdentity(host, port, nodeID, name)
}

// RefreshName records a new name for an already-identified peer, keeping its
// session and its directory key.
func (cluster *Cluster) RefreshName(nodeID, name string) (*Node, bool) {
	return cluster.NodeManager.RefreshName(nodeID, name)
}

// MarkDirty tombstones a superseded node_id and drops its entry, so gossip
// cannot reintroduce it before the cluster has converged.
func (cluster *Cluster) MarkDirty(nodeID string, now int64) {
	cluster.NodeManager.MarkDirty(nodeID, now)
}

// IsDirty reports whether a node_id is currently tombstoned.
func (cluster *Cluster) IsDirty(nodeID string, now int64) bool {
	return cluster.NodeManager.IsDirty(nodeID, now)
}

func (cluster *Cluster) LoadStaticNode(nodes []StaticNode) error {
	return cluster.NodeManager.LoadStaticNode(nodes)
}

// Add add node
func (cluster *Cluster) Add(node *Node) {
	cluster.NodeManager.Add(node)
}

// AddIfAbsent files a node only when no existing entry already matches it.
func (cluster *Cluster) AddIfAbsent(node *Node) bool {
	return cluster.NodeManager.AddIfAbsent(node)
}

// DeleteNode removes an entry only if the directory still holds this exact node.
func (cluster *Cluster) DeleteNode(node *Node) {
	cluster.NodeManager.DeleteNode(node)
}

// GetNodeFromWrongNodeList ...
func (cluster *Cluster) GetNodeFromWrongNodeList(nodeName string) *Node {
	return cluster.WrongTokenNode.Get(nodeName)
}

// AddToWrongNodeList 将节点添加异常节点列表中
//
// Keyed by name, not by directory key: this is a short-lived blacklist for peers
// that presented the wrong cluster token, and such a peer has not proved any
// identity worth filing it under.
func (cluster *Cluster) AddToWrongNodeList(node *Node) {
	cluster.WrongTokenNode.AddWithKey(node.Name, node)
}

// IsSameCluster verifies that a peer belongs to this cluster.
//
// The shared token is the whole test. It is also the HKDF input for every
// end<->end RPC key, so a peer that presents the right token can already read
// and write the user plane — there is nothing a second, cosmetic cluster-name
// factor could add, and it could only ever reject legitimate peers over a typo.
func (cluster *Cluster) IsSameCluster(token string) error {
	if cluster.Token != token {
		return fmt.Errorf("wrong token")
	}
	return nil
}

// GetProtoNodesWithFilter 获取proto Node列表, 返回满足过滤条件的node集合
//
// Keyed by NAME, deliberately, even though the directory itself is keyed by
// node_id. This map goes on the wire as HeartBeatRsp.nodesMap, and a pre-2.8
// peer merging it looks the key up in its own name-keyed directory: sending
// ids would miss every time, and its add-only merge would then overwrite each
// entry with a token-less copy, tearing down every session once per round.
// Post-2.8 merging ignores the key entirely and reads identity from the message
// body, so the key only has to keep old peers correct.
//
// A duplicate name therefore costs one node its slot in the advertised set. That
// is a misconfiguration either way; it is logged, and the digest stays
// consistent because the list is derived from this same map.
func (cluster *Cluster) GetProtoNodesWithFilter(f NodeFilter) map[string]*proto.Node {
	nodeMap := map[string]*proto.Node{}
	for _, node := range cluster.NodeManager.GetAllNode() {
		if !f(node) {
			continue
		}
		name := node.GetName()
		prev, dup := nodeMap[name]
		if dup {
			// The winner MUST be chosen by a total order, not by whichever the map
			// yielded first: Go randomises map iteration, so picking arbitrarily
			// would make this node's own advertised set — and therefore its digest —
			// differ between two successive calls. Every heartbeat would then report
			// a mismatch and every round would pay a full directory reconcile,
			// forever. Ordering by node_id, then address, keeps it stable.
			if advertisePriority(prev) <= advertisePriority(node.Node) {
				continue
			}
			log.Warn("two nodes advertise the same name; one is omitted from the advertised set",
				"node_name", name,
				"kept_node_id", node.GetNodeId(), "kept_host", node.GetHost(), "kept_port", node.GetPort(),
				"dropped_node_id", prev.GetNodeId(), "dropped_host", prev.GetHost(), "dropped_port", prev.GetPort(),
			)
		}
		nodeMap[name] = node.Node
	}
	return nodeMap
}

// advertisePriority gives same-named nodes a deterministic, cluster-wide order.
// It must depend only on fields every node sees identically.
func advertisePriority(n *proto.Node) string {
	return n.GetNodeId() + "\x00" + n.GetHost() + "\x00" + strconv.FormatInt(int64(n.GetPort()), 10)
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
	nodeNameList := []string{}
	for _, node := range cluster.NodeManager.GetAllNode() {
		nodeNameList = append(nodeNameList, node.GetName())
	}
	return nodeNameList
}

// AuthRemoteNode validates a caller's token against its directory entry and, on
// success, replaces the caller-supplied stub with the local entry.
//
// Identity resolution goes through LookupPeer, so a peer that was renamed or
// moved is still recognised. The address is only asserted when neither side
// knows an id: between two identified nodes the token — which we minted for
// that specific id — is a strictly stronger proof than a host/port comparison,
// and demanding the address match as well would reject a peer purely for having
// been reconfigured.
func (cluster *Cluster) AuthRemoteNode(node **Node) error {
	// n本地记录的Node, node: 根据远端访问参数构建的Node
	n := cluster.NodeManager.LookupPeer((*node).Node)
	if n == nil {
		return fmt.Errorf("node not exist")
	}
	claimID, knownID := (*node).GetNodeId(), n.GetNodeId()
	switch {
	case claimID != "" && knownID != "" && claimID != knownID:
		return fmt.Errorf("node %q identity mismatch: directory holds node_id %s, caller claims %s",
			n.GetName(), knownID, claimID)
	case claimID == "" || knownID == "":
		// Legacy pairing: no identity on at least one side, so fall back to the
		// pre-2.8 host+port+name assertion.
		if !(*node).Compare(n) {
			return fmt.Errorf("node %q address mismatch: directory holds %s:%d, caller claims %s:%d",
				n.GetName(), n.GetHost(), n.GetPort(), (*node).GetHost(), (*node).GetPort())
		}
	}
	// 验证 token 与心跳未超时, 通过则原子刷新收到心跳时间 (check-then-act 在 Node 内加锁完成)
	if err := n.AuthAndTouch((*node).GetInToken()); err != nil {
		return err
	}
	*node = n
	return nil
}

// Delete removes the node filed under a directory key.
func (cluster *Cluster) Delete(key string) {
	cluster.NodeManager.Delete(key)
}

// DeleteFromWrongTokenNodeList ...
func (cluster *Cluster) DeleteFromWrongTokenNodeList(nodeName string) {
	cluster.WrongTokenNode.Delete(nodeName)
}

func (cluster *Cluster) Clear() {
	cluster.NodeManager.Clear()
}

// IsValid 判断该节点是否有效; 未知 key 返回 false 而非 panic
func (cluster *Cluster) IsValid(key string) bool {
	n := cluster.NodeManager.Get(key)
	return n != nil && n.IsValid()
}

// RegisteredLocal 判断节点node是否在本地注册过; 未知 key 返回 false
func (cluster *Cluster) RegisteredLocal(key string) bool {
	n := cluster.NodeManager.Get(key)
	return n != nil && n.RegisteredLocal()
}

// Filter 过滤超时未上报的节点
func (cluster *Cluster) FilterTimeoutNode() {
	currentTime := time.Now().Unix()
	cluster.NodeManager.Filter(getNodeFilterByCurrentTime(currentTime))
}

// RegisteredRemote 判断本地是否已经在远端节点注册; 未知 key 返回 false
func (cluster *Cluster) RegisteredRemote(key string) bool {
	n := cluster.NodeManager.Get(key)
	return n != nil && n.RegisteredRemote()
}

// HaveNode 判断是否存在该目录键
func (cluster *Cluster) HaveNode(key string) bool {
	return cluster.NodeManager.HaveNode(key)
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
