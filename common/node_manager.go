package common

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type NodeManager struct {
	nodes *map[string]*Node
	lock  sync.RWMutex
}

type nodeFilter func(*Node) bool

func NewNodeManager() NodeManager {
	return NodeManager{
		nodes: &map[string]*Node{},
		lock:  sync.RWMutex{},
	}
}

// 添加新的节点
func (nm *NodeManager) Add(key string, node *Node) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	(*nm.nodes)[key] = node
}

// 判断是否存在该node
func (nm *NodeManager) HaveNode(key string) (*Node, bool) {
	node, ok := (*nm.nodes)[key]
	return node, ok
}

// 删除指定key
func (nm *NodeManager) Delete(key string) {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	delete((*nm.nodes), key)
}

// 加载本地配置文件中的node
func (nm *NodeManager) LoadStaticNode() error {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nodeList := []staticNode{}
	err := globalConfigManager.UnmarshalKey("cluster.nodes", &nodeList)
	if err != nil {
		return err
	}

	localNode := &Node{
		Node: &GlobalLocalNode.Node,
	}

	for _, node := range nodeList {
		if node.IsValide(localNode) {
			logger.Info(
				"Msg=Load Static Node|Node=%s:%d|NodeName=%s",
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

// 清空nodes
func (nm *NodeManager) Clear() {
	nm.lock.Lock()
	defer nm.lock.Unlock()
	nm.nodes = &map[string]*Node{}
}

func (nm *NodeManager) Get(nodeName string) *Node {
	if n, ok := (*nm.nodes)[nodeName]; ok {
		return n
	}
	return nil
}

func (nm *NodeManager) GetNodes() map[string]*Node {
	return *nm.nodes
}

func (nm *NodeManager) GetNodesWithFilter(filter nodeFilter) *[]*Node {
	nodes := []*Node{}
	nm.lock.RLock()
	defer nm.lock.RUnlock()
	for _, n := range *nm.nodes {
		if filter(n) {
			nodes = append(nodes, n)
		}
	}
	return &nodes
}

// 过滤掉不符合条件的Node
func (nm *NodeManager) Filter(filter nodeFilter) {
	tmpNM := &map[string]*Node{}
	nm.lock.RLock()
	for key, node := range *nm.nodes {
		if filter(node) {
			(*tmpNM)[key] = node
		} else {
			logger.Info(
				"Msg=drop node|Node=%s|Node=%s:%d",
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
