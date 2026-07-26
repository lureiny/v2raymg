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

	// Fetch groups for all nodes in parallel
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range nodeList {
		wg.Add(1)
		go func(node *cluster.Node) {
			defer wg.Done()
			nodes := s.GetTargetNodes(node.GetName())
			if len(nodes) == 0 {
				return
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetNodeGroupsReqType, &proto.GetNodeGroupsReq{}, s.GetClusterToken())
			for _, v := range succList {
				if gs, ok := v.([]string); ok {
					mu.Lock()
					groupsMap[node.GetName()] = gs
					mu.Unlock()
					return
				}
			}
		}(n)
	}
	wg.Wait()

	for _, n := range nodeList {
		groups := groupsMap[n.GetName()]
		if groups == nil {
			groups = []string{}
		}
		result = append(result, gin.H{
			"name":   n.GetName(),
			"host":   n.GetHost(),
			"port":   n.GetPort(),
			"groups": groups,
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
