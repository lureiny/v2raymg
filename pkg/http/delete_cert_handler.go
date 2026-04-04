package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type DeleteCertHandler struct{ HttpHandlerImp }

func (handler *DeleteCertHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		Domain string `json:"domain"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if req.Domain == "" {
		jsonErr(c, 400, "domain is required")
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.DeleteCertReqType, &proto.DeleteCertReq{Domain: req.Domain}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s", errMsg)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *DeleteCertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *DeleteCertHandler) getRelativePath() string { return "/cert" }

func (handler *DeleteCertHandler) help() string {
	return `DELETE /api/cert
	删除证书
	body: {"target": "", "domain": ""}`
}
