package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type TagHandler struct{ HttpHandlerImp }

func (handler *TagHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *TagHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetTagReqType,
		&proto.GetTagReq{},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Error(
			"Err=%s|Target=%s",
			errMsg,
			parasMap["target"],
		)
	}
	c.JSON(200, succList)
}

func (handler *TagHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *TagHandler) getRelativePath() string {
	return "/tag"
}

func (handler *TagHandler) help() string {
	usage := `/tag
	获取目标节点的所有inbound tag
	/tag?target={target}&token={token}
	参数列表:
	target: 目标node
	token: 用于验证操作权限
	`
	return usage
}
