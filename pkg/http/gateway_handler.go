package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type GatewayHandler struct{ HttpHandlerImp }

func (handler *GatewayHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target              string `json:"target"`
		EnableGatewayModel  bool   `json:"enable_gateway_model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.SetGatewayModelReqType, &proto.SetGatewayModelReq{
		EnableGatewayModel: req.EnableGatewayModel,
	}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *GatewayHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *GatewayHandler) getRelativePath() string { return "/gateway" }

func (handler *GatewayHandler) help() string {
	return `PUT /gateway
	设置gateway模式
	body: {"target": "", "enable_gateway_model": false}
	enable_gateway_model: 是否开启gateway模式`
}
