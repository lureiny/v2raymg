package ping

import (
	"fmt"
	"net"
	"sync"

	"github.com/lureiny/v2raymg/common/util"
)

// PingNodeInfo ...
type PingNodeInfo struct {
	Host string
	Port int32

	ISP string
	Geo string
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
		mutex: sync.RWMutex{},
	}
}

// AddNode ...
func (pnm *PingNodeManager) AddNode(node *PingNodeInfo) error {
	if net.ParseIP(node.Host) == nil {
		return fmt.Errorf("unsupport ip[%s]", node.Host)
	}
	op := func() error {
		if _, ok := pnm.nodes[node.Host]; ok {
			return fmt.Errorf("node[%s] has exist", node.Host)
		}
		pnm.nodes[node.Host] = node
		return nil
	}
	return util.OpWithWlock(&pnm.mutex, op)
}

// ListNode ...
func (pnm *PingNodeManager) ListNode() (nodes []*PingNodeInfo) {
	op := func() error {
		for _, node := range pnm.nodes {
			nodes = append(nodes, node)
		}
		return nil
	}
	util.OpWithRlock(&pnm.mutex, op)
	return
}
