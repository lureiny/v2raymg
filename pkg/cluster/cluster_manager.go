package cluster

func getNodeFilterByCurrentTime(currentTime int64) NodeFilter {
	return func(n *Node) bool {
		return n.IsValid()
	}
}

type ClusterManager interface {
	Clear()
	Filter()
}

type EndNodeClusterManager struct {
	Cluster
}

func NewEndNodeClusterManager() *EndNodeClusterManager {
	encm := &EndNodeClusterManager{}
	encm.Init()
	return encm
}

// CenterClusterManager was removed in 2026-07 together with the center node.
// Discovery is now static_nodes + end<->end directory propagation; nothing in
// the tree needs a multi-cluster view any more.
