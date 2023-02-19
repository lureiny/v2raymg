package http

import (
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type TransferCertHandler struct{ HttpHandlerImp }

func (handler *TransferCertHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["domain"] = c.DefaultQuery("domain", "")
	return parasMap
}

func readCertFile(cert *lego.Certificate) ([]byte, []byte, error) {
	certData, err := os.ReadFile(cert.CertificateFile)
	if err != nil {
		return nil, nil, err
	}
	keyData, err := os.ReadFile(cert.KeyFile)
	if err != nil {
		return nil, nil, err
	}
	return certData, keyData, nil
}

func (handler *TransferCertHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	cert := handler.httpServer.certManager.GetCert(parasMap["domain"])
	if cert == nil {
		c.String(200, fmt.Sprintf("can't find domain's[%s] cert", parasMap["domain"]))
		return
	}

	certData, keyData, err := readCertFile(cert)
	if err != nil {
		c.String(200, fmt.Sprintf("read cert file err > %v", err))
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	for index, node := range *nodes {
		if node.Name == handler.getHttpServer().Name {
			*nodes = append((*nodes)[0:index], (*nodes)[index+1:]...)
		}
	}
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		client.TransferCertType,
		&proto.TransferCertReq{
			Domain:   parasMap["domain"],
			CertData: certData,
			KeyDatas: keyData,
		},
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

func (handler *TransferCertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *TransferCertHandler) help() string {
	usage := `/transferCert
	将本机证书文件传输到指定节点上
	/tag?target={target}&token={token}&domain={domain}
	参数列表:
	target: 目标node
	token: 用于验证操作权限
	domain: 证书文件对应的域名
	`
	return usage
}
