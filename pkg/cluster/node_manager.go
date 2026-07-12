package cluster

import (
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type NodeManager struct {
	nodes *map[string]*Node
	name  string
	// lock 保护 nodes 指针及其指向的 map。
	// 注意: 持 lock 期间不得调用本类型的其他方法(RWMutex 不可重入)。
	lock sync.RWMutex
}

const defaultNodeManagerName = "NodeManager"

type NodeFilter func(*Node) bool

func NewNodeManager() NodeManager {
	return NodeManager{
		nodes: &map[string]*Node{},
		lock:  sync.RWMutex{},
		name:  defaultNodeManagerName,
	}
}

// Add 添加新的节点
func (nm *NodeManager) Add(key string, node *Node) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	(*nm.nodes)[key] = node
}

// HaveNode 判断是否存在该node
func (nm *NodeManager) HaveNode(key string) bool {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	_, ok := (*nm.nodes)[key]
	return ok
}

// Delete 删除指定key
func (nm *NodeManager) Delete(key string) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	delete((*nm.nodes), key)
}

// SetName ...
func (nm *NodeManager) SetName(name string) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.name = name
}

// LoadStaticNode 加载本地配置文件中的node
func (nm *NodeManager) LoadStaticNode(nodes []StaticNode) error {
	nm.lock.Lock()
	defer nm.lock.Unlock()

	localNode := &Node{
		Node: &globalLocalNode.Node,
	}

	for _, node := range nodes {
		// 过滤掉与本地节点相同的节点
		if node.IsValide(localNode) {
			log.Info("Load Static Node",
				"manager_name", nm.name,
				"node", fmt.Sprintf("%s:%d", node.Host, node.Port),
				"node_name", node.Name,
			)
			(*nm.nodes)[node.Name] = &Node{
				Node: &proto.Node{
					Name:        node.Name,
					Port:        node.Port,
					Host:        node.Host,
					ClusterName: localNode.ClusterName,
				},
				isLocal:    true,
				CreateTime: time.Now().Unix(),
			}
		}
	}
	return nil
}

// Clear 清空nodes
func (nm *NodeManager) Clear() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.nodes = &map[string]*Node{}
}

// Get ...
func (nm *NodeManager) Get(nodeName string) *Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	if n, ok := (*nm.nodes)[nodeName]; ok {
		return n
	}
	return nil
}

// GetAllNode 返回当前节点表的浅拷贝快照。
// 修改返回的 map 不影响内部状态; value 仍是共享的 *Node 指针。
func (nm *NodeManager) GetAllNode() map[string]*Node {
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	nodes := make(map[string]*Node, len(*nm.nodes))
	for key, node := range *nm.nodes {
		nodes[key] = node
	}
	return nodes
}

func (nm *NodeManager) GetNodesWithFilter(filter NodeFilter) []*Node {
	nodes := []*Node{}
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	for _, n := range *nm.nodes {
		if filter(n) {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// 过滤掉不符合条件的Node
func (nm *NodeManager) Filter(filter NodeFilter) {
	tmpNM := &map[string]*Node{}
	nm.lock.RLock()
	for key, node := range *nm.nodes {
		if filter(node) {
			(*tmpNM)[key] = node
		} else {
			log.Info("drop node",
				"manager_name", nm.name,
				"node_name", node.Name,
				"node", fmt.Sprintf("%s:%d", node.Host, node.Port),
			)
		}
	}
	nm.lock.RUnlock()

	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.nodes = tmpNM
}
