package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type GatewayHandler struct{ HttpHandlerImp }

func (handler *GatewayHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["enableGatewayModel"] = c.DefaultQuery("enable_gateway_model", "0")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *GatewayHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(client.SetGatewayModelReqType, &proto.SetGatewayModelReq{
		EnableGatewayModel: parasMap["enableGatewayModel"] == "1",
	})
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Target=%s",
			errMsg,
			parasMap["target"],
		)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *GatewayHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *GatewayHandler) getRelativePath() string {
	return "/gateway"
}

func (handler *GatewayHandler) help() string {
	usage := `/gateway
	/gateway?token={token}&target={target}&enable_gateway_model={enable_gateway_model}
	获取当前集群内的全部节点
	参数列表:
	token: 用于验证操作权限
	target: 目标节点名称
	enable_gateway_model: 是否开启gateway模式, 1: 开启gateway模式, 0: 关闭gateway模式, 默认为关闭
	`
	return usage
}
