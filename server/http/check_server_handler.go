package http

import (
	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type PingCheckHandler struct{ HttpHandlerImp }

func (handler *PingCheckHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["enablePingCheck"] = c.DefaultQuery("enable_ping_check", "false")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *PingCheckHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, nil)
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.SetPingCheckType, &proto.SetPingCheckReq{
		EnablePingCheck: parasMap["enablePingCheck"] == "true",
	}, globalCluster.GetClusterToken())
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

func (handler *PingCheckHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *PingCheckHandler) getRelativePath() string {
	return "/pingCheck"
}

func (handler *PingCheckHandler) help() string {
	usage := `/pingCheck
	/pingCheck?token={token}&target={target}&enable_ping_check={enable_ping_check}
	获取当前集群内的全部节点
	参数列表:
	token: 用于验证操作权限
	target: 目标节点名称
	enable_ping_check: 是否开启ping检测, true: 开启ping检测, 0: 关闭ping检测, 默认为关闭
	`
	return usage
}
