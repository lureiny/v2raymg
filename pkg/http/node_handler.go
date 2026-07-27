package http

import (
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type NodeHandler struct{ HttpHandlerImp }

func (handler *NodeHandler) handlerFunc(c *gin.Context) {
	nodeList := handler.getHttpServer().clusterNodes.GetNodesWithFilter(func(n *cluster.Node) bool {
		return true
	})

	s := handler.getHttpServer()
	result := make([]gin.H, 0, len(nodeList))
	groupsMap := make(map[string][]string)

	// Keyed by directory key, not by name: two nodes may now carry the same name
	// (the identity-keyed directory lets them coexist rather than refusing the
	// second), and keying this by name would silently merge their groups.
	nameCount := map[string]int{}
	for _, n := range nodeList {
		nameCount[n.GetName()]++
	}

	// Fetch groups for all nodes in parallel
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range nodeList {
		wg.Add(1)
		go func(node *cluster.Node) {
			defer wg.Done()
			if !node.IsValid() && !node.IsSelf() {
				return
			}
			// Addressed directly rather than looked up by its own name: the name
			// round-trip was ambiguous once duplicates became possible.
			rpcClient := client.NewEndNodeClient([]*cluster.Node{node}, s.GetLocalNode())
			succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetNodeGroupsReqType, &proto.GetNodeGroupsReq{}, s.GetClusterToken())
			for _, v := range succList {
				if gs, ok := v.([]string); ok {
					mu.Lock()
					groupsMap[cluster.DirectoryKey(node)] = gs
					mu.Unlock()
					return
				}
			}
		}(n)
	}
	wg.Wait()

	for _, n := range nodeList {
		groups := groupsMap[cluster.DirectoryKey(n)]
		if groups == nil {
			groups = []string{}
		}
		result = append(result, gin.H{
			"name": n.GetName(),
			"host": n.GetHost(),
			"port": n.GetPort(),
			// The node's real identity. Stable across renames, host changes and
			// port changes; empty only for a peer we have not exchanged with yet
			// (a static seed) or one older than 2.8.
			"node_id": n.GetNodeId(),
			// True when another entry carries this same name, so a caller can see
			// that ?target=<name> is ambiguous and address it by node_id instead.
			"duplicate_name": nameCount[n.GetName()] > 1,
			"groups":         groups,
		})
	}
	c.JSON(200, result)
}

func (handler *NodeHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeHandler) getRelativePath() string { return "/node" }

func (handler *NodeHandler) help() string {
	return `/api/node 获取当前集群内的全部节点（含 groups）`
}
