package ping

import (
	"fmt"
	"net"
	"sync"
)

// PingNodeInfo represents a ping target node.
type PingNodeInfo struct {
	Host string
	Port int32

	ISP  string
	Geo  string
	// Usage indicates which ping checkers can use this node.
	// Values: "icmp", "tcp", "pingpe". Default: all.
	Usage []string `yaml:"usage" json:"usage"`
}

// PingNodeManager ...
type PingNodeManager struct {
	mutex sync.RWMutex
	nodes map[string]*PingNodeInfo
}

// NewPingNodeManager ...
func NewPingNodeManager() *PingNodeManager {
	return &PingNodeManager{
		nodes: make(map[string]*PingNodeInfo),
	}
}

// AddNode ...
func (pnm *PingNodeManager) AddNode(node *PingNodeInfo) error {
	if net.ParseIP(node.Host) == nil {
		return fmt.Errorf("unsupport ip[%s]", node.Host)
	}
	pnm.mutex.Lock()
	defer pnm.mutex.Unlock()
	if _, ok := pnm.nodes[node.Host]; ok {
		return fmt.Errorf("node[%s] has exist", node.Host)
	}
	pnm.nodes[node.Host] = node
	return nil
}

// ListNode ...
func (pnm *PingNodeManager) ListNode() []*PingNodeInfo {
	pnm.mutex.RLock()
	defer pnm.mutex.RUnlock()
	nodes := make([]*PingNodeInfo, 0, len(pnm.nodes))
	for _, node := range pnm.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}
