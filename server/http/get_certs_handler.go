package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type GetCertsHandler struct{ HttpHandlerImp }

func (handler *GetCertsHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *GetCertsHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		client.GetCertsType,
		&proto.GetCertsReq{},
	)

	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Target=%s",
			errMsg,
			parasMap["target"],
		)
	}

	c.JSON(200, succList)
}

func (handler *GetCertsHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		handler.handlerFunc,
	}
}

func (handler *GetCertsHandler) help() string {
	usage := `/getCerts
	获取订阅
	/getCerts?target={target}&token={token}
	target: 目标节点
	token: 用于验证操作权限
	`
	return usage
}
