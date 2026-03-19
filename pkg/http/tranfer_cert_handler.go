package http

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type TransferCertHandler struct{ HttpHandlerImp }

func (handler *TransferCertHandler) handlerFunc(c *gin.Context) {
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
	domain := req.Domain

	cr := handler.getHttpServer().certReader
	if cr == nil {
		c.String(200, "cert reader not configured")
		return
	}
	certFile, keyFile, ok := cr.GetCertFiles(domain)
	if !ok {
		c.String(200, fmt.Sprintf("can't find domain's[%s] cert", domain))
		return
	}

	certData, err := os.ReadFile(certFile)
	if err != nil {
		c.String(200, fmt.Sprintf("read cert file err > %v", err))
		return
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		c.String(200, fmt.Sprintf("read key file err > %v", err))
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	filtered := nodes[:0]
	for _, node := range nodes {
		if node.Name != handler.getHttpServer().Name {
			filtered = append(filtered, node)
		}
	}
	if len(filtered) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(filtered, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.TransferCertType,
		&proto.TransferCertReq{
			Domain:   domain,
			CertData: certData,
			KeyDatas: keyData,
		},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		log.Error("transfer cert failed", "err", joinFailedList(failedList), "target", req.Target)
	}
	c.JSON(200, succList)
}

func (handler *TransferCertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *TransferCertHandler) getRelativePath() string { return "/cert/transfer" }

func (handler *TransferCertHandler) help() string {
	return `POST /cert/transfer
	将本机证书文件传输到指定节点上
	body: {"target": "", "domain": ""}`
}
