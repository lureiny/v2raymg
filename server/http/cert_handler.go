package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/global/logger"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type CertHandler struct{ HttpHandlerImp }

func (handler *CertHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["domain"] = c.DefaultQuery("domain", "")
	return parasMap
}

func (handler *CertHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, nil)
	req := &proto.ObtainNewCertReq{
		Domain: parasMap["domain"],
	}
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(client.ObtainNewCertType, req)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s",
			errMsg,
		)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *CertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *CertHandler) getRelativePath() string {
	return "/cert"
}

func (handler *CertHandler) help() string {
	usage := `/cert
	/cert?target={target}&domain={domain}&token={token}
	申请证书
	参数列表:
	target: 目标节点
	domain: 域名
	token: 用于验证操作权限
	`
	return usage
}
