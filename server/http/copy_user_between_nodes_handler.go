package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type CopyUserBetweenNodesHandler struct{ HttpHandlerImp }

func (handler *CopyUserBetweenNodesHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["srcNode"] = c.DefaultQuery("src_node", handler.getHttpServer().Name)
	parasMap["dstNode"] = c.DefaultQuery("dst_node", handler.getHttpServer().Name)
	return parasMap
}

func (handler *CopyUserBetweenNodesHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	if parasMap["srcNode"] == parasMap["dstNode"] {
		c.String(200, fmt.Sprintf("src target(%s) is same with dst target(%s)", parasMap["srcNode"], parasMap["dstNode"]))
		return
	}

	srcNodes := handler.getHttpServer().GetTargetNodes(parasMap["srcNode"])
	if len(srcNodes) == 0 {
		c.String(200, "no avaliable src node")
		return
	}

	dstNodes := handler.getHttpServer().GetTargetNodes(parasMap["dstNode"])
	if len(dstNodes) == 0 {
		c.String(200, "no avaliable dst node")
		return
	}

	srcNodeRpcClient := client.NewEndNodeClient(srcNodes, nil)
	dstNodeRpcClient := client.NewEndNodeClient(dstNodes, nil)

	succList, failedList, _ := srcNodeRpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(), client.GetUsersReqType, &proto.GetUsersReq{}, globalCluster.GetClusterToken())
	if len(failedList) > 0 {
		errMsg := fmt.Sprintf("get src node user list err > %v", failedList[parasMap["srcNode"]])
		logger.Error(
			"Err=%s|SrcNode=%s",
			errMsg,
			parasMap["srcNode"],
		)
		c.String(200, errMsg)
		return
	}

	users := succList[parasMap["srcNode"]]
	for _, u := range users.([]*proto.User) {
		u.Tags = []string{}
		u.Downlink = 0
		u.Uplink = 0
	}
	_, failedList, _ = dstNodeRpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.AddUsersReqType,
		&proto.UserOpReq{
			Users: users.([]*proto.User),
		},
		globalCluster.GetClusterToken(),
	)

	if len(failedList) > 0 {
		c.String(200, joinFailedList(failedList))
		return
	}
	c.String(200, "Succ")
}

func (handler *CopyUserBetweenNodesHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *CopyUserBetweenNodesHandler) getRelativePath() string {
	return "/copyUserBetweenNodes"
}

func (handler *CopyUserBetweenNodesHandler) help() string {
	usage := `/copyUserBetweenNodes
	节点间复制用户, 将源节点上的用户添加到目标节点的默认inbound上
	请求示例: /copyUserBetweenNodes?src_node={src_node}&dst_node={dst_node}&token={token}
	参数列表: 
	token: 用于验证操作权限
	src_node: 源节点名称
	dst_node: 目标节点名称
	`
	return usage
}
