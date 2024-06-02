package http

import (
	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
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

	rpcClient := client.NewEndNodeClient(nodes, nil)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetCertsType,
		&proto.GetCertsReq{},
		globalCluster.GetClusterToken(),
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

func (handler *GetCertsHandler) getRelativePath() string {
	return "/getCerts"
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
