package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const nodeGroupsGetHelp = `GET /api/node/:name/groups
	获取节点所属 groups
	path: name — 目标节点名`

// NodeGroupsGetReq is the request payload for GET /node/:name/groups (MCP tool: get_node_groups).
type NodeGroupsGetReq struct {
	Name string `json:"name" path:"name" jsonschema:"Target cluster node name or node_id whose assigned groups will be retrieved."`
}

// NodeGroupsResp is the response payload for GET /node/:name/groups.
type NodeGroupsResp struct {
	Groups []string `json:"groups"`
}

func (s *HttpServer) nodeGroupsGetEndpoint() Endpoint[NodeGroupsGetReq, NodeGroupsResp] {
	return Endpoint[NodeGroupsGetReq, NodeGroupsResp]{
		Method:         http.MethodGet,
		Path:           "/node/:name/groups",
		Scope:          ScopeAdmin,
		Help:           nodeGroupsGetHelp,
		MCPName:        "get_node_groups",
		MCPDescription: "Get the list of groups assigned to a specific cluster node identified by node name or node_id.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req NodeGroupsGetReq) (NodeGroupsResp, error) {
			if req.Name == "" {
				return NodeGroupsResp{}, NewEndpointErr(http.StatusBadRequest, "name is required")
			}
			nodes := s.GetTargetNodes(req.Name)
			if len(nodes) == 0 {
				return NodeGroupsResp{}, NewEndpointErr(http.StatusBadGateway, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return NodeGroupsResp{}, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.GetNodeGroupsReqType, &proto.GetNodeGroupsReq{}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Node=%s", errMsg, req.Name)
				return NodeGroupsResp{}, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			// Collect groups from the first (and typically only) node
			var groups []string
			for _, v := range succList {
				if gs, ok := v.([]string); ok {
					groups = gs
					break
				}
			}
			if groups == nil {
				groups = []string{}
			}
			return NodeGroupsResp{Groups: groups}, nil
		},
	}
}

// NodeGroupsGetHandler GET /node/:name/groups
type NodeGroupsGetHandler struct{ HttpHandlerImp }

func (handler *NodeGroupsGetHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().nodeGroupsGetEndpoint())(c)
}

func (handler *NodeGroupsGetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeGroupsGetHandler) getRelativePath() string { return "/node/:name/groups" }

func (handler *NodeGroupsGetHandler) help() string {
	return nodeGroupsGetHelp
}

const nodeGroupsSetHelp = `PUT /api/node/:name/groups
	设置节点所属 groups
	path: name — 目标节点名
	body: {"groups": ["default", "hk"]}`

// NodeGroupsSetReq is the request payload for PUT /node/:name/groups (MCP tool: set_node_groups).
type NodeGroupsSetReq struct {
	Name   string   `json:"name" path:"name" jsonschema:"Target cluster node name or node_id whose assigned groups will be replaced."`
	Groups []string `json:"groups" jsonschema:"List of group names to assign to the target cluster node."`
}

func (s *HttpServer) nodeGroupsSetEndpoint() Endpoint[NodeGroupsSetReq, map[string]any] {
	return Endpoint[NodeGroupsSetReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/node/:name/groups",
		Scope:          ScopeAdmin,
		Help:           nodeGroupsSetHelp,
		MCPName:        "set_node_groups",
		MCPDescription: "Set the list of groups assigned to a specific cluster node identified by node name or node_id.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req NodeGroupsSetReq) (map[string]any, error) {
			if req.Name == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "name is required")
			}
			nodes := s.GetTargetNodes(req.Name)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.SetNodeGroupsReqType, &proto.SetNodeGroupsReq{Groups: req.Groups}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Node=%s", errMsg, req.Name)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// NodeGroupsSetHandler PUT /node/:name/groups
type NodeGroupsSetHandler struct{ HttpHandlerImp }

func (handler *NodeGroupsSetHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().nodeGroupsSetEndpoint())(c)
}

func (handler *NodeGroupsSetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeGroupsSetHandler) getRelativePath() string { return "/node/:name/groups" }

func (handler *NodeGroupsSetHandler) help() string {
	return nodeGroupsSetHelp
}
