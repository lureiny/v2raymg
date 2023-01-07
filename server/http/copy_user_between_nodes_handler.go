package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
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

	srcNodes := handler.getHttpServer().getTargetNodes(parasMap["srcNode"])
	if len(*srcNodes) == 0 {
		c.String(200, "no avaliable src node")
		return
	}

	dstNodes := handler.getHttpServer().getTargetNodes(parasMap["dstNode"])
	if len(*dstNodes) == 0 {
		c.String(200, "no avaliable dst node")
		return
	}

	srcNodeRpcClient := client.NewEndNodeClient(srcNodes, localNode)
	dstNodeRpcClient := client.NewEndNodeClient(dstNodes, localNode)

	succList, failedList, _ := srcNodeRpcClient.ReqToMultiEndNodeServer(client.GetUsersReqType, &proto.GetUsersReq{})
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
	}
	_, failedList, _ = dstNodeRpcClient.ReqToMultiEndNodeServer(
		client.AddUsersReqType,
		&proto.UserOpReq{
			Users: users.([]*proto.User),
		},
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

func (handler *CopyUserBetweenNodesHandler) help() string {
	usage := `/copyUserBetweenNodes
	节点间复制用户, 将源节点上的用户添加到目标节点的默认inbound上
	请求示例: /copyUserBetweenNodes?src_target={src_target}&dst_target={dst_target}&token={token}
	参数列表: 
	token: 用于验证操作权限
	src_node: 源节点名称
	dst_target: 目标节点名称
	`
	return usage
}
