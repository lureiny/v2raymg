package cluster

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type NodeManager struct {
	nodes *map[string]*Node
	name  string
	lock  sync.RWMutex
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
func (nm *NodeManager) LoadStaticNode() error {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nodeList := []staticNode{}
	err := config.UnmarshalKey(common.ConfigClusterNodes, &nodeList)
	if err != nil {
		return err
	}

	localNode := &Node{
		Node: &globalLocalNode.Node,
	}

	for _, node := range nodeList {
		// 过滤掉与本地节点相同的节点
		if node.IsValide(localNode) {
			logger.Info(
				"Msg=Load Static Node|ManagerName=%s|Node=%s:%d|NodeName=%s",
				nm.name,
				node.Host,
				node.Port,
				node.Name,
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
	if n, ok := (*nm.nodes)[nodeName]; ok {
		return n
	}
	return nil
}

// GetAllNode ...
func (nm *NodeManager) GetAllNode() map[string]*Node {
	return *nm.nodes
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
			logger.Info(
				"Msg=drop node|ManagerName=%s|Node=%s|Node=%s:%d",
				nm.name,
				node.Name,
				node.Host,
				node.Port,
			)
		}
	}
	nm.lock.RUnlock()

	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.nodes = tmpNM
}
