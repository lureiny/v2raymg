package http

import (
	"context"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const nodeListHelp = `/api/node 获取当前集群内的全部节点（含 groups）`

// NodeListReq is the request payload for GET /node (MCP tool: list_nodes).
type NodeListReq struct{}

// NodeInfoResp describes a single cluster node returned by GET /node.
type NodeInfoResp struct {
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Port          int32    `json:"port"`
	NodeID        string   `json:"node_id"`
	DuplicateName bool     `json:"duplicate_name"`
	Groups        []string `json:"groups"`
}

func (s *HttpServer) nodeListEndpoint() Endpoint[NodeListReq, []NodeInfoResp] {
	return Endpoint[NodeListReq, []NodeInfoResp]{
		Method:         http.MethodGet,
		Path:           "/node",
		Scope:          ScopeAdmin,
		Help:           nodeListHelp,
		MCPName:        "list_nodes",
		MCPDescription: "List all nodes in the cluster with their name, host, port, stable node_id, duplicate_name flag, and assigned groups.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req NodeListReq) ([]NodeInfoResp, error) {
			nodeList := s.clusterNodes.GetNodesWithFilter(func(n *cluster.Node) bool {
				return true
			})

			result := make([]NodeInfoResp, 0, len(nodeList))
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
					if rpcClient == nil {
						return
					}
					succList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.GetNodeGroupsReqType, &proto.GetNodeGroupsReq{}, s.GetClusterToken())
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
				result = append(result, NodeInfoResp{
					Name: n.GetName(),
					Host: n.GetHost(),
					Port: n.GetPort(),
					// The node's real identity. Stable across renames, host changes and
					// port changes; empty only for a peer we have not exchanged with yet
					// (a static seed) or one older than 2.8.
					NodeID: n.GetNodeId(),
					// True when another entry carries this same name, so a caller can see
					// that ?target=<name> is ambiguous and address it by node_id instead.
					DuplicateName: nameCount[n.GetName()] > 1,
					Groups:        groups,
				})
			}
			return result, nil
		},
	}
}

type NodeHandler struct{ HttpHandlerImp }

func (handler *NodeHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().nodeListEndpoint())(c)
}

func (handler *NodeHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeHandler) getRelativePath() string { return "/node" }

func (handler *NodeHandler) help() string {
	return nodeListHelp
}
