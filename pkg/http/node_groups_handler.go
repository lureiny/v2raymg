package http

import (
	"context"
	"fmt"
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

// NodeGroupsSetHandler PUT /node/:name/groups
type NodeGroupsSetHandler struct{ HttpHandlerImp }

func (handler *NodeGroupsSetHandler) handlerFunc(c *gin.Context) {
	name := c.Param("name")
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	nodes := handler.getHttpServer().GetTargetNodes(name)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.SetNodeGroupsReqType, &proto.SetNodeGroupsReq{Groups: body.Groups}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Node=%s", errMsg, name)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *NodeGroupsSetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeGroupsSetHandler) getRelativePath() string { return "/node/:name/groups" }

func (handler *NodeGroupsSetHandler) help() string {
	return `PUT /api/node/:name/groups
	设置节点所属 groups
	path: name — 目标节点名
	body: {"groups": ["default", "hk"]}`
}
