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

const copyUserBetweenNodesHelp = `POST /api/copyUserBetweenNodes
	节点间复制用户, 将源节点上的用户添加到目标节点的默认inbound上
	body: {"src_node": "", "dst_node": ""}
	src_node: 源节点名称
	dst_node: 目标节点名称`

// CopyUserBetweenNodesReq is the request payload for POST /copyUserBetweenNodes (MCP tool: copy_user_between_nodes).
type CopyUserBetweenNodesReq struct {
	SrcNode string `json:"src_node,omitempty" jsonschema:"Source cluster node name or node_id (defaults to current node when empty)."`
	DstNode string `json:"dst_node,omitempty" jsonschema:"Destination cluster node name or node_id (defaults to current node when empty; must differ from src_node)."`
}

func (s *HttpServer) copyUserBetweenNodesEndpoint() Endpoint[CopyUserBetweenNodesReq, map[string]any] {
	return Endpoint[CopyUserBetweenNodesReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/copyUserBetweenNodes",
		Scope:          ScopeAdmin,
		Help:           copyUserBetweenNodesHelp,
		MCPName:        "copy_user_between_nodes",
		MCPDescription: "Copy all users from a source cluster node to a destination cluster node (resetting copied traffic counters to zero).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req CopyUserBetweenNodesReq) (map[string]any, error) {
			srcNode := req.SrcNode
			if srcNode == "" {
				srcNode = s.Name
			}
			dstNode := req.DstNode
			if dstNode == "" {
				dstNode = s.Name
			}

			if srcNode == dstNode {
				return nil, NewEndpointErr(http.StatusBadRequest, fmt.Sprintf("src target(%s) is same with dst target(%s)", srcNode, dstNode))
			}

			srcNodes := s.GetTargetNodes(srcNode)
			if len(srcNodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available src node")
			}

			dstNodes := s.GetTargetNodes(dstNode)
			if len(dstNodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available dst node")
			}

			srcNodeRpcClient := client.NewEndNodeClient(srcNodes, s.GetLocalNode())
			dstNodeRpcClient := client.NewEndNodeClient(dstNodes, s.GetLocalNode())
			if srcNodeRpcClient == nil || dstNodeRpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}

			succList, failedList, _ := srcNodeRpcClient.ReqToMultiEndNodeServer(
				ctx, client.GetUsersReqType, &proto.GetUsersReq{}, s.GetClusterToken())
			if len(failedList) > 0 {
				errDetail := failedList[srcNode]
				if errDetail == "" {
					errDetail = joinFailedList(failedList)
				}
				errMsg := fmt.Sprintf("get src node user list err > %v", errDetail)
				log.Errorf("Err=%s|SrcNode=%s", errMsg, srcNode)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			rawUsers, exists := succList[srcNode]
			if !exists {
				// Support addressing src_node by node_id when succList is keyed by node name.
				for _, v := range succList {
					rawUsers = v
					exists = true
					break
				}
			}
			if !exists || rawUsers == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, fmt.Sprintf("get src node(%s) user list returned empty response", srcNode))
			}
			users, ok := rawUsers.([]*proto.User)
			if !ok {
				return nil, NewEndpointErr(http.StatusInternalServerError, fmt.Sprintf("unexpected user list response type from src node(%s): %T", srcNode, rawUsers))
			}

			for _, u := range users {
				if u == nil {
					continue
				}
				u.Downlink = 0
				u.Uplink = 0
			}
			_, failedList, _ = dstNodeRpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.AddUsersReqType,
				&proto.UserOpReq{Users: users},
				s.GetClusterToken(),
			)

			if len(failedList) > 0 {
				return nil, NewEndpointErr(http.StatusInternalServerError, joinFailedList(failedList))
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

type CopyUserBetweenNodesHandler struct{ HttpHandlerImp }

func (handler *CopyUserBetweenNodesHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().copyUserBetweenNodesEndpoint())(c)
}

func (handler *CopyUserBetweenNodesHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *CopyUserBetweenNodesHandler) getRelativePath() string {
	return "/copyUserBetweenNodes"
}

func (handler *CopyUserBetweenNodesHandler) help() string {
	return copyUserBetweenNodesHelp
}
