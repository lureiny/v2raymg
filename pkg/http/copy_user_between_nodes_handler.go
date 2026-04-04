package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type CopyUserBetweenNodesHandler struct{ HttpHandlerImp }

func (handler *CopyUserBetweenNodesHandler) handlerFunc(c *gin.Context) {
	var req struct {
		SrcNode string `json:"src_node"`
		DstNode string `json:"dst_node"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if req.SrcNode == "" {
		req.SrcNode = handler.getHttpServer().Name
	}
	if req.DstNode == "" {
		req.DstNode = handler.getHttpServer().Name
	}

	if req.SrcNode == req.DstNode {
		jsonErr(c, 400, fmt.Sprintf("src target(%s) is same with dst target(%s)", req.SrcNode, req.DstNode))
		return
	}

	srcNodes := handler.getHttpServer().GetTargetNodes(req.SrcNode)
	if len(srcNodes) == 0 {
		jsonErr(c, 502, "no available src node")
		return
	}

	dstNodes := handler.getHttpServer().GetTargetNodes(req.DstNode)
	if len(dstNodes) == 0 {
		jsonErr(c, 502, "no available dst node")
		return
	}

	srcNodeRpcClient := client.NewEndNodeClient(srcNodes, handler.getHttpServer().GetLocalNode())
	dstNodeRpcClient := client.NewEndNodeClient(dstNodes, handler.getHttpServer().GetLocalNode())

	succList, failedList, _ := srcNodeRpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(), client.GetUsersReqType, &proto.GetUsersReq{}, handler.getHttpServer().GetClusterToken())
	if len(failedList) > 0 {
		errMsg := fmt.Sprintf("get src node user list err > %v", failedList[req.SrcNode])
		log.Errorf("Err=%s|SrcNode=%s", errMsg, req.SrcNode)
		jsonErr(c, 500, errMsg)
		return
	}

	users := succList[req.SrcNode]
	for _, u := range users.([]*proto.User) {
		u.Downlink = 0
		u.Uplink = 0
	}
	_, failedList, _ = dstNodeRpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.AddUsersReqType,
		&proto.UserOpReq{Users: users.([]*proto.User)},
		handler.getHttpServer().GetClusterToken(),
	)

	if len(failedList) > 0 {
		jsonErr(c, 500, joinFailedList(failedList))
		return
	}
	jsonOK(c)
}

func (handler *CopyUserBetweenNodesHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *CopyUserBetweenNodesHandler) getRelativePath() string {
	return "/copyUserBetweenNodes"
}

func (handler *CopyUserBetweenNodesHandler) help() string {
	return `POST /api/copyUserBetweenNodes
	节点间复制用户, 将源节点上的用户添加到目标节点的默认inbound上
	body: {"src_node": "", "dst_node": ""}
	src_node: 源节点名称
	dst_node: 目标节点名称`
}
