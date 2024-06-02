package http

import (
	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
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

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, nil)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetBandWidthStatsReqType,
		&proto.GetBandwidthStatsReq{},
		globalCluster.GetClusterToken(),
	)

	if len(succList) > 0 {
		c.JSON(200, succList)
		return
	}
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Pattern=%s|Reset=%v",
			errMsg,
			parasMap["pattern"],
			parasMap["reset"] == "1",
		)
		c.String(200, errMsg)
		return
	}
}

func (handler *StatHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *StatHandler) getRelativePath() string {
	return "/stat"
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
