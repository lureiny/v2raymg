package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
)

type StatHandler struct{ HttpHandlerImp }

func (handler *StatHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	// stat
	parasMap["reset"] = c.DefaultQuery("reset", "0")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["pattern"] = c.DefaultQuery("pattern", "") // 查询字符
	return parasMap
}

func (handler *StatHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().getTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	statsMap, err := rpcClient.GetBandWidthStats(parasMap["pattern"], parasMap["reset"] == "1")

	if err != nil {
		logger.Info(
			"Err=%s",
			err.Error(),
		)
		c.String(200, err.Error())
		return
	}
	var jsonDatas = gin.H{}
	for key, s := range *statsMap {
		jsonDatas[key] = s
	}
	c.JSON(200, jsonDatas)
}

func (handler *StatHandler) help() string {
	usage := `/stat
	获取指定节点的统计信息, 需要proxy配置中开启统计
	/stat?target={target}&reset={reset}&pattern={pattern}&token={token}
	参数列表:
	target: 目标node名称
	token: 用于验证操作权限
	reset: 是否重置统计数据
	pattern: 查询参数, 默认情况下查询全部统计信息, 包含inbound与用户信息
	`
	return usage
}
