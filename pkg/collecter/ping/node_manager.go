package ping

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
)

// NodeManager manages ping nodes from multiple sources.
type NodeManager interface {
	// LoadFromSources loads nodes from all configured sources.
	LoadFromSources(sources []appconfig.PingNodeSource) error
	// ListByUsage returns nodes that support the given usage type.
	ListByUsage(usage string) []*PingNodeInfo
	// ListAll returns all loaded nodes.
	ListAll() []*PingNodeInfo
	// OnChange registers a callback for node changes (reload).
	OnChange(callback func())
	// Stop stops all reload goroutines.
	Stop()
}

// nodeManagerImpl implements NodeManager.
type nodeManagerImpl struct {
	mu        sync.RWMutex
	nodes     map[string]*PingNodeInfo // key: host
	stopFuncs []func()
	callbacks []func()
}

// NewNodeManager creates a new NodeManager.
func NewNodeManager() NodeManager {
	return &nodeManagerImpl{
		nodes: make(map[string]*PingNodeInfo),
	}
}

// LoadFromSources loads nodes from all configured sources.
func (nm *nodeManagerImpl) LoadFromSources(sources []appconfig.PingNodeSource) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Clear existing nodes and stop functions
	nm.nodes = make(map[string]*PingNodeInfo)
	for _, stop := range nm.stopFuncs {
		stop()
	}
	nm.stopFuncs = nil

	for _, src := range sources {
		var loader NodeLoader

		switch src.Type {
		case "file":
			if src.Source == "" {
				log.Warn("file loader has empty source, skipping")
				continue
			}
			loader = NewFileLoader(src.Source)
		case "remote":
			if src.Source == "" {
				log.Warn("remote loader has empty source, skipping")
				continue
			}
			loader = NewRemoteLoader(src.Source)
			// Setup reload if interval specified
			if src.UpdateInterval > 0 {
				if rl, ok := loader.(ReloadableLoader); ok {
					stop := rl.StartReload(time.Duration(src.UpdateInterval)*time.Second, func(nodes []*PingNodeInfo) {
						nm.updateNodes(nodes)
						nm.notifyChange()
					})
					nm.stopFuncs = append(nm.stopFuncs, stop)
				}
			}
		default:
			log.Warn("unknown node source type", "type", src.Type)
			continue
		}

		nodes, err := loader.Load()
		if err != nil {
			log.Error("load ping nodes failed", "loader", loader.Name(), "err", err)
			continue
		}

		log.Info("loaded ping nodes", "loader", loader.Name(), "count", len(nodes))

		// Merge nodes (later sources override by host)
		for _, node := range nodes {
			nm.nodes[node.Host] = node
		}
	}

	// Log statistics by usage type
	nm.logStatsLocked()

	return nil
}

// updateNodes updates the node map (called from reload callback).
func (nm *nodeManagerImpl) updateNodes(newNodes []*PingNodeInfo) {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// Build set of current hosts
	newHosts := make(map[string]struct{})
	for _, n := range newNodes {
		newHosts[n.Host] = struct{}{}
	}

	// Remove nodes that no longer exist
	for host := range nm.nodes {
		if _, exists := newHosts[host]; !exists {
			delete(nm.nodes, host)
			log.Debug("removed ping node", "host", host)
		}
	}

	// Add/update nodes
	for _, node := range newNodes {
		nm.nodes[node.Host] = node
	}

	// Log updated statistics
	nm.logStatsLocked()
}

// logStatsLocked logs statistics about loaded nodes by usage type (caller must hold lock).
// For nodes with no explicit usage, they are counted under every known type.
func (nm *nodeManagerImpl) logStatsLocked() {
	// Collect all distinct usage values that appear across nodes.
	knownUsages := make(map[string]struct{})
	for _, node := range nm.nodes {
		if len(node.Usage) == 0 {
			// Default: all — we'll fill these in after discovering all types.
			continue
		}
		for _, u := range node.Usage {
			knownUsages[u] = struct{}{}
		}
	}
	// Ensure the built-in types always appear in the summary even if no node
	// explicitly declares them.
	for _, builtin := range []string{"icmp", "tcp"} {
		knownUsages[builtin] = struct{}{}
	}

	// Count per usage type.
	countByUsage := make(map[string]int, len(knownUsages))
	for u := range knownUsages {
		countByUsage[u] = 0
	}
	for _, node := range nm.nodes {
		for u := range knownUsages {
			if hasUsage(node, u) {
				countByUsage[u]++
			}
		}
	}

	// Build a stable key=value list for the structured log.
	args := []any{"total", len(nm.nodes)}
	for _, u := range []string{"icmp", "tcp"} {
		args = append(args, u, countByUsage[u])
	}
	// Append any extra usage types beyond the two built-ins.
	for u, cnt := range countByUsage {
		if u == "icmp" || u == "tcp" {
			continue
		}
		args = append(args, u, cnt)
	}

	log.Info("ping nodes summary", args...)
}

// hasUsage checks if a node supports the given usage.
func hasUsage(node *PingNodeInfo, usage string) bool {
	if len(node.Usage) == 0 {
		return true // Default: all usages
	}
	for _, u := range node.Usage {
		if u == usage {
			return true
		}
	}
	return false
}

// notifyChange calls all registered callbacks.
func (nm *nodeManagerImpl) notifyChange() {
	nm.mu.RLock()
	callbacks := nm.callbacks
	nm.mu.RUnlock()

	for _, cb := range callbacks {
		cb()
	}
}

// ListByUsage returns nodes that support the given usage type.
func (nm *nodeManagerImpl) ListByUsage(usage string) []*PingNodeInfo {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	var result []*PingNodeInfo
	for _, node := range nm.nodes {
		if hasUsage(node, usage) {
			result = append(result, node)
		}
	}
	return result
}

// ListAll returns all loaded nodes.
func (nm *nodeManagerImpl) ListAll() []*PingNodeInfo {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	result := make([]*PingNodeInfo, 0, len(nm.nodes))
	for _, node := range nm.nodes {
		result = append(result, node)
	}
	return result
}

// OnChange registers a callback for node changes.
func (nm *nodeManagerImpl) OnChange(callback func()) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.callbacks = append(nm.callbacks, callback)
}

// Stop stops all reload goroutines.
func (nm *nodeManagerImpl) Stop() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	for _, stop := range nm.stopFuncs {
		stop()
	}
	nm.stopFuncs = nil
}
