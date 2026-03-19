package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type CertHandler struct{ HttpHandlerImp }

func (handler *CertHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		Domain string `json:"domain"`
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
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.ObtainNewCertType, &proto.ObtainNewCertReq{Domain: req.Domain}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s", errMsg)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *CertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *CertHandler) getRelativePath() string { return "/cert" }

func (handler *CertHandler) help() string {
	return `POST /cert
	申请证书
	body: {"target": "", "domain": ""}`
}
