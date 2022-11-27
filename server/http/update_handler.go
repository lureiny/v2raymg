package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
)

type UpdateHandler struct{ HttpHandlerImp }

func (handler *UpdateHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	// update proxy server
	parasMap["versionTag"] = c.DefaultQuery("version_tag", "latest")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *UpdateHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().getTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)

	if err := rpcClient.UpdateProxy(parasMap["versionTag"]); err != nil {
		logger.Error(
			"Err=%s|Target=%s|Tag=%s",
			err.Error(),
			parasMap["target"],
			parasMap["versionTag"],
		)
		c.String(200, err.Error())
		return
	}
	c.String(200, "Succ")
}

func (handler *UpdateHandler) help() string {
	usage := `/update
	更新目标节点的proxy版本
	/update?target={target}&version_tag={version_tag}&token={token}
	参数列表:
	target: 目标node
	token: 用于验证操作权限
	version_tag: github上目标tag, 默认为最新版。v2ray: https://github.com/v2fly/v2ray-core/releases, xray: https://github.com/XTLS/Xray-core/releases
	`
	return usage
}
