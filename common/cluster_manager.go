package common

import (
	"sync"
)

func getNodeFilterByCurrentTime(currentTime int64) nodeFilter {
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

type CenterClusterManager struct {
	clusters map[string]*Cluster
	lock     sync.RWMutex
}

func NewCenterClusterManager() *CenterClusterManager {
	return &CenterClusterManager{
		clusters: map[string]*Cluster{},
		lock:     sync.RWMutex{},
	}
}

func (ccm *CenterClusterManager) Init() {
	ccm.clusters = map[string]*Cluster{}
}

func (ccm *CenterClusterManager) GetCluster(clusterName string) *Cluster {
	ccm.lock.RLock()
	defer ccm.lock.RUnlock()
	if cluster, ok := ccm.clusters[clusterName]; ok {
		return cluster
	}
	return nil
}

func (ccm *CenterClusterManager) Add(clusterName string, node *Node) {
	ccm.lock.Lock()
	defer ccm.lock.Unlock()
	if cluster, ok := ccm.clusters[clusterName]; ok {
		cluster.NodeManager.Add(node.Name, node)
	} else {
		newCluster := &Cluster{
			Name:       clusterName,
			NodeManager: NewNodeManager(),
		}
		newCluster.NodeManager.Add(node.Name, node)
		ccm.clusters[clusterName] = newCluster
	}
}

func (ccm *CenterClusterManager) DeleteNode(clusterName, nodeName string) {
	ccm.lock.RLock()
	defer ccm.lock.RUnlock()
	if cluster, ok := ccm.clusters[clusterName]; ok {
		cluster.NodeManager.Delete(nodeName)
	}
}

func (ccm *CenterClusterManager) DeleteCluster(clusterName string) {
	ccm.lock.Lock()
	defer ccm.lock.Unlock()
	delete(ccm.clusters, clusterName)
}

func (ccm *CenterClusterManager) Clear() {
	ccm.lock.Lock()
	defer ccm.lock.Unlock()
	for k := range ccm.clusters {
		delete(ccm.clusters, k)
	}
}

func (ccm *CenterClusterManager) Filter() {
	ccm.lock.RLock()
	defer ccm.lock.RUnlock()
	for _, cluster := range ccm.clusters {
		cluster.NodeManager.Filter(func(n *Node) bool {
			return n.IsValid()
		})
	}
}
